package cronjob

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
)

// AddCronJob adds a cron job to the scheduler based on job type
func (cm *CronJobManager) AddCronJob(
	ctx *gin.Context,
	jobName string,
	jobSpec string,
	jobType model.CronJobType,
	jobConfig datatypes.JSON,
) (cron.EntryID, error) {
	var entryID cron.EntryID
	var err error
	switch jobType {
	case model.CronJobTypeInternalFunc:
		entryID, err = cm.addInternalFuncCronJob(ctx, jobName, jobSpec, jobConfig)
		if err != nil {
			klog.Error(err)
			return -1, err
		}
	default:
		return -1, fmt.Errorf("unsupported cron job type: %s", jobType)
	}
	return entryID, nil
}

// addInternalFuncCronJob adds an internal function type cron job to the scheduler
func (cm *CronJobManager) addInternalFuncCronJob(
	_ *gin.Context,
	jobName string,
	jobSpec string,
	jobConfig datatypes.JSON,
) (cron.EntryID, error) {
	// Create job function based on job name
	f, err := cm.newInternalJobFunc(jobName, jobConfig)
	if err != nil {
		klog.Error(err)
		return -1, err
	}

	// Add function to cron scheduler
	entryID, err := cm.cron.AddFunc(jobSpec, f)
	if err != nil {
		klog.Error(err)
		return -1, err
	}
	return entryID, nil
}

// newInternalJobFunc creates the appropriate cron job function based on job name
func (cm *CronJobManager) newInternalJobFunc(jobName string, jobConfig datatypes.JSON) (cron.FuncJob, error) {
	ctx := context.Background()
	switch jobName {
	case CLEAN_LONG_TIME_RUNNING_JOB:
		req := &CleanLongTimeRunningJobsRequest{}
		if err := json.Unmarshal(jobConfig, req); err != nil {
			return nil, err
		}
		return cm.wrapInternalJobFunc(jobName, func() (any, error) {
			return cm.CleanLongTimeRunningJobs(ctx, req)
		}), nil

	case CLEAN_LOW_GPU_USAGE_JOB:
		req := &CleanLowGPUUsageRequest{}
		if err := json.Unmarshal(jobConfig, req); err != nil {
			return nil, err
		}
		return cm.wrapInternalJobFunc(jobName, func() (any, error) {
			return cm.CleanLowGPUUsageJobs(ctx, req)
		}), nil

	case CLEAN_WAITING_JUPYTER_JOB:
		req := &CancelWaitingJupyterJobsRequest{}
		if err := json.Unmarshal(jobConfig, req); err != nil {
			return nil, err
		}
		return cm.wrapInternalJobFunc(jobName, func() (any, error) {
			return cm.CleanWaitingJupyterJobs(ctx, req)
		}), nil

	default:
		return nil, fmt.Errorf("unsupported internal function cron job name: %s", jobName)
	}
}

// wrapInternalJobFunc wraps a job handler function with common logic for execution and recording
func (cm *CronJobManager) wrapInternalJobFunc(jobName string, handler func() (any, error)) cron.FuncJob {
	return func() {
		// Execute the job handler
		jobResult, err := handler()
		status := model.CronJobRecordStatusSuccess
		if err != nil {
			status = model.CronJobRecordStatusFailed
		}

		// Create job record
		rec := &model.CronJobRecord{
			Name:        jobName,
			ExecuteTime: time.Now(),
			Message:     "",
			Status:      status,
		}

		// Marshal job result to JSON
		if jobResult != nil {
			if data, err := json.Marshal(jobResult); err != nil {
				err := fmt.Errorf("CronJobManager.wrapInternalJobFunc failed to marshal job result: %w", err)
				klog.Error(err)
			} else {
				rec.JobData = datatypes.JSON(data)
			}
		}

		// Save record to database
		db := query.GetDB()
		if err := db.Model(rec).Create(rec).Error; err != nil {
			err := fmt.Errorf("CronJobManager.wrapInternalJobFunc failed to create record: %w", err)
			klog.Error(err)
		}
	}
}

// UpdateJobConfig updates the configuration of an existing cron job
func (cm *CronJobManager) UpdateJobConfig(
	ctx *gin.Context,
	name string,
	jobType *model.CronJobType,
	spec *string,
	suspend *bool,
	config *string,
) error {
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()

	var (
		cur    *model.CronJobConfig
		update *model.CronJobConfig
		err    error
	)

	err = query.GetDB().Transaction(func(tx *gorm.DB) error {
		cur, err = cm.getCurrentJobConfigFromDB(tx, name)
		if err != nil {
			return err
		}

		update = cm.prepareUpdateConfig(cur, jobType, spec, suspend, config)

		// Handle suspend state transition
		if suspend != nil && cm.shouldSuspendJob(cur.GetSuspend(), *suspend) {
			return cm.updateSuspendedJobConfig(tx, name, cur, update)
		}

		// Handle active job (not suspended)
		if suspend != nil && !(*suspend) {
			return cm.updateActiveJobConfig(ctx, tx, name, cur, update)
		}

		return tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error
	})
	return err
}

// getCurrentJobConfigFromDB retrieves current job configuration from database with row-level lock
func (cm *CronJobManager) getCurrentJobConfigFromDB(tx *gorm.DB, name string) (*model.CronJobConfig, error) {
	cur := &model.CronJobConfig{}
	if txErr := tx.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Model(cur).
		Where(query.CronJobConfig.Name.Eq(name)).
		First(cur).Error; txErr != nil {
		err := fmt.Errorf("CronJobManager.getCurrentJobConfigFromDB failed: %w", txErr)
		klog.Error(err)
		return nil, err
	}
	return cur, nil
}

// prepareUpdateConfig creates update configuration
func (cm *CronJobManager) prepareUpdateConfig(
	cur *model.CronJobConfig,
	jobType *model.CronJobType,
	spec *string,
	suspend *bool,
	config *string,
) *model.CronJobConfig {
	update := &model.CronJobConfig{
		Name:    cur.Name,
		Type:    cur.Type,
		Spec:    cur.Spec,
		Suspend: cur.Suspend,
		Config:  cur.Config,
	}
	if jobType != nil {
		update.Type = *jobType
	}
	if spec != nil && *spec != "" {
		update.Spec = *spec
	}
	if suspend != nil {
		update.Suspend = suspend
	}
	if config != nil && *config != "" {
		update.Config = datatypes.JSON(*config)
	}
	return update
}

// shouldSuspendJob checks if job should be suspended
func (cm *CronJobManager) shouldSuspendJob(wasSuspended, shouldSuspend bool) bool {
	return !wasSuspended && shouldSuspend
}

// updateSuspendedJobConfig handles suspending an active job
func (cm *CronJobManager) updateSuspendedJobConfig(
	tx *gorm.DB,
	name string,
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
) error {
	curEntryID := cur.EntryID
	update.EntryID = -1
	if err := tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error; err != nil {
		err := fmt.Errorf("CronJobManager.updateSuspendedJobConfig failed to update cron job config for job %s: %w", name, err)
		klog.Error(err)
		return err
	}
	cm.cron.Remove(cron.EntryID(curEntryID))
	return nil
}

// updateActiveJobConfig handles job need to active (not suspended)
func (cm *CronJobManager) updateActiveJobConfig(
	ctx *gin.Context,
	tx *gorm.DB,
	name string,
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
) error {
	if cur.GetSuspend() {
		if cm.jobNeedsUpdate(cur, update) {
			cm.cron.Remove(cron.EntryID(cur.EntryID))
		}
	}
	entryID, err := cm.AddCronJob(ctx, name, update.Spec, update.Type, update.Config)
	if err != nil {
		err := fmt.Errorf("addCronJob failed: %w", err)
		klog.Error(err)
		return err
	}
	update.EntryID = int(entryID)
	if err := tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error; err != nil {
		err := fmt.Errorf("DB failed to update cron job config for job %s: %w", name, err)
		cm.cron.Remove(entryID)
		klog.Error(err)
		return err
	}
	return nil
}

// jobNeedsUpdate checks if job configuration has changed
func (cm *CronJobManager) jobNeedsUpdate(
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
) bool {
	if cur.Type != update.Type {
		return true
	}
	if cur.Spec != update.Spec {
		return true
	}
	if update.Config != nil && !bytes.Equal(cur.Config, update.Config) {
		return true
	}
	return false
}

// SyncCronJob synchronizes cron jobs from database and starts the scheduler
func (cm *CronJobManager) SyncCronJob() {
	db := query.GetDB()
	err := query.GetDB().Transaction(func(tx *gorm.DB) error {
		var configs []*model.CronJobConfig
		if err := db.Where(query.CronJobConfig.Suspend.Is(false)).Find(&configs).Error; err != nil {
			err := fmt.Errorf("CronJobManager.SyncCronJob: failed to load cron job configs: %w", err)
			klog.Error(err)
			cm.cron.Start()
			return nil
		}
		klog.Infof("CronJobManager.SyncCronJob: loaded %d non-suspended cron jobs from database", len(configs))

		for _, conf := range configs {
			entryID, err := cm.AddCronJob(nil, conf.Name, conf.Spec, conf.Type, conf.Config)
			if err != nil {
				err := fmt.Errorf("CronJobManager.AddCronJob: failed to add cron job %s with spec %s: %w", conf.Name, conf.Spec, err)
				klog.Error(err)
				continue
			}
			if int(entryID) != conf.EntryID {
				err := tx.
					Model(&model.CronJobConfig{}).
					Where(query.CronJobConfig.Name.Eq(conf.Name)).
					Update("entry_id", int(entryID)).
					Error
				if err != nil {
					err := fmt.Errorf("DB failed to update entry_id for job %s: %w", conf.Name, err)
					klog.Error(err)
				}
			}
		}
		return nil
	})

	if err != nil {
		klog.Error(err)
	}

	cm.cron.Start()
	klog.Info("CronJobManager.SyncCronJob: cron scheduler started")
}

// GetConfigMapCredentials retrieves credentials from ConfigMap
func (cm *CronJobManager) GetConfigMapCredentials(c *gin.Context) (username, password string, err error) {
	configMap := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Namespace: CONFIG_MAP_NAMESPACE,
		Name:      CONFIG_MAP_NAME,
	}

	if err := cm.Client.Get(c, namespacedName, configMap); err != nil {
		return "", "", fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	username = configMap.Data[CONFIG_MAP_USERNAME_KEY]
	password = configMap.Data[CONFIG_MAP_PASSWORD_KEY]

	if username == "" || password == "" {
		return "", "", fmt.Errorf("%s or %s not found in ConfigMap data", CONFIG_MAP_USERNAME_KEY, CONFIG_MAP_PASSWORD_KEY)
	}

	return username, password, nil
}

// GetAllCronJobs retrieves all cron job configurations from database
func (cm *CronJobManager) GetAllCronJobs(ctx context.Context) ([]*model.CronJobConfig, error) {
	var configs []*model.CronJobConfig
	if err := query.GetDB().WithContext(ctx).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// StopCron stops the cron scheduler
func (cm *CronJobManager) StopCron() {
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()
	cm.cron.Stop()
}
