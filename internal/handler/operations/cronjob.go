package operations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/resputil"
)

type CronjobConfigs struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Schedule string         `json:"schedule"`
	Suspend  bool           `json:"suspend"`
	Configs  map[string]any `json:"configs"`
}

// UpdateCronjobConfig godoc
//
//	@Summary		Update cronjob config
//	@Description	Update one cronjob config
//	@Tags			Operations
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			use	body		CronjobConfigs			true	"CronjobConfigs"
//	@Success		200	{object}	resputil.Response[any]	"Success"
//	@Failure		400	{object}	resputil.Response[any]	"Request parameter error"
//	@Failure		500	{object}	resputil.Response[any]	"Other errors"
//	@Router			/v1/operations/cronjob [put]
func (mgr *OperationsMgr) UpdateCronjobConfig(c *gin.Context) {
	var req CronjobConfigs
	if err := c.ShouldBindJSON(&req); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}

	var (
		jobTypePtr *model.CronJobType
		specPtr    *string
		configPtr  *string
	)
	if req.Type != "" {
		jobTypePtr = ptr.To(model.CronJobType(req.Type))
	}
	if req.Schedule != "" {
		specPtr = ptr.To(req.Schedule)
	}

	if len(req.Configs) > 0 {
		configJson, err := json.Marshal(req.Configs)
		if err != nil {
			resputil.Error(c, err.Error(), resputil.NotSpecified)
		}
		configPtr = ptr.To(string(configJson))
	}
	if err := mgr.updateJobConfig(c, req.Name, jobTypePtr, specPtr, &req.Suspend, configPtr); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	resputil.Success(c, "Successfully update cronjob config")
}

// GetCronjobConfigs godoc
//
//	@Summary		Get all cronjob configs
//	@Description	Get all cronjob configs
//	@Tags			Operations
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Success		200	{object}	resputil.Response[any]	"Success"
//	@Failure		400	{object}	resputil.Response[any]	"Request parameter error"
//	@Failure		500	{object}	resputil.Response[any]	"Other errors"
//	@Router			/v1/operations/cronjob [get]
func (mgr *OperationsMgr) GetCronjobConfigs(c *gin.Context) {
	jobs, err := mgr.GetAllCronJobs(c)
	if err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	configs := lo.Map(jobs, func(job *model.CronJobConfig, _ int) CronjobConfigs {
		config := make(map[string]any)
		if err := json.Unmarshal(job.Config, &config); err != nil {
			config = map[string]any{}
		}
		ret := CronjobConfigs{
			Name:     job.Name,
			Type:     string(job.Type),
			Schedule: job.Spec,
			Suspend:  job.GetSuspend(),
			Configs:  config,
		}
		return ret
	})
	resputil.Success(c, configs)
}

type HttpCall struct {
	Method  string            `json:"method"`
	Url     string            `json:"url"`
	Query   map[string]string `json:"query"`
	Payload map[string]any    `json:"payload"`
}

// addCronJob adds a cron job to the scheduler based on job type
func (cm *OperationsMgr) addCronJob(
	ctx *gin.Context,
	jobName string,
	jobSpec string,
	jobType model.CronJobType,
	jobConfig datatypes.JSON,
) (cron.EntryID, error) {
	var entryID cron.EntryID
	var err error
	switch jobType {
	case model.CronJobTypeHTTPCall:
		entryID, err = cm.addHTTPCallCronJob(ctx, jobName, jobSpec, jobConfig)
		if err != nil {
			klog.Error(err)
			return -1, err
		}
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
func (cm *OperationsMgr) addInternalFuncCronJob(
	_ *gin.Context,
	jobName string,
	jobSpec string,
	jobConfig datatypes.JSON,
) (cron.EntryID, error) {
	// Create job function based on job name
	f, err := cm.NewInternalJobFunc(jobName, jobConfig)
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

// NewInternalJobFunc creates the appropriate cron job function based on job name
func (cm *OperationsMgr) NewInternalJobFunc(jobName string, jobConfig datatypes.JSON) (cron.FuncJob, error) {
	switch jobName {
	case CLEAN_LONG_TIME_RUNNING_JOB:
		req := &CleanLongTimeRequest{}
		if err := json.Unmarshal(jobConfig, req); err != nil {
			return nil, err
		}
		return cm.wrapInternalJobFunc(jobName, func() (any, error) {
			return cm.HandleLongTimeRunningJobs(&gin.Context{}, req)
		}), nil

	case CLEAN_LOW_GPU_USAGE_JOB:
		req := &CleanLowGPUUsageRequest{}
		if err := json.Unmarshal(jobConfig, req); err != nil {
			return nil, err
		}
		return cm.wrapInternalJobFunc(jobName, func() (any, error) {
			return cm.HandleLowGPUUsageJobs(&gin.Context{}, req)
		}), nil

	case CLEAN_WAITING_JUPYTER_JOB:
		req := &CancelWaitingJupyterRequest{}
		if err := json.Unmarshal(jobConfig, req); err != nil {
			return nil, err
		}
		return cm.wrapInternalJobFunc(jobName, func() (any, error) {
			return cm.HandleWaitingJupyterJobs(&gin.Context{}, req)
		}), nil

	default:
		return nil, fmt.Errorf("unsupported internal function cron job name: %s", jobName)
	}
}

// wrapInternalJobFunc wraps a job handler function with common logic for execution and recording
func (cm *OperationsMgr) wrapInternalJobFunc(jobName string, handler func() (any, error)) cron.FuncJob {
	return func() {
		// Execute the job handler
		jobResult, err := handler()
		success := err == nil

		if err != nil {
			klog.Errorf("Internal function cron job %s failed: %v", jobName, err)
		}

		// Create job record
		rec := &model.CronJobRecord{
			Name:        jobName,
			ExecuteTime: time.Now(),
			Message:     "",
			Success:     ptr.To(success),
		}

		// Marshal job result to JSON
		if jobResult != nil {
			if data, err := json.Marshal(jobResult); err != nil {
				klog.Errorf("Internal function cron job %s marshal result failed: %v", jobName, err)
			} else {
				rec.JobData = datatypes.JSON(data)
			}
		}

		// Save record to database
		db := query.GetDB()
		if err := db.Model(rec).Create(rec).Error; err != nil {
			klog.Errorf("DB failed to create record of internal function cron job %s: %v", jobName, err)
		}
	}
}

// addHTTPCallCronJob adds an HTTP call type cron job to the scheduler
func (cm *OperationsMgr) addHTTPCallCronJob(
	ctx *gin.Context,
	jobName string,
	jobSpec string,
	jobConfig datatypes.JSON,
) (cron.EntryID, error) {
	httpCall := &HttpCall{}
	if err := json.Unmarshal(jobConfig, httpCall); err != nil {
		klog.Error(err)
		return -1, err
	}
	f, err := cm.NewHTTPCallCronJob(ctx, jobName, httpCall.Method, httpCall.Url, httpCall.Query, httpCall.Payload)
	if err != nil {
		klog.Error(err)
		return -1, err
	}
	entryID, err := cm.cron.AddFunc(jobSpec, f) // must be the final error
	if err != nil {
		klog.Error(err)
		return -1, err
	}
	return entryID, nil
}

// updateJobConfig updates the configuration of an existing cron job
func (cm *OperationsMgr) updateJobConfig(
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
			return cm.handleJobSuspension(tx, name, cur, update)
		}

		// Handle active job (not suspended)
		if suspend != nil && !(*suspend) {
			return cm.handleActiveJob(ctx, tx, name, cur, update)
		}

		return tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error
	})
	return nil
}

// getCurrentJobConfigFromDB retrieves current job configuration from database with row-level lock
func (cm *OperationsMgr) getCurrentJobConfigFromDB(tx *gorm.DB, name string) (*model.CronJobConfig, error) {
	cur := &model.CronJobConfig{}
	// 使用 FOR UPDATE 悲观锁，防止并发修改冲突
	if txErr := tx.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Model(cur).
		Where(query.CronJobConfig.Name.Eq(name)).
		First(cur).Error; txErr != nil {
		err := fmt.Errorf("DB failed to query: %w", txErr)
		klog.Error(err)
		return nil, err
	}
	return cur, nil
}

// prepareUpdateConfig creates update configuration
func (cm *OperationsMgr) prepareUpdateConfig(
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
func (cm *OperationsMgr) shouldSuspendJob(wasSuspended, shouldSuspend bool) bool {
	return !wasSuspended && shouldSuspend
}

// handleJobSuspension handles suspending an active job
func (cm *OperationsMgr) handleJobSuspension(
	tx *gorm.DB,
	name string,
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
) error {
	update.EntryID = -1
	if err := tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error; err != nil {
		klog.Error(err)
		return err
	}
	cm.cron.Remove(cron.EntryID(cur.EntryID))
	return nil
}

// handleActiveJob handles job need to active (not suspended)
func (cm *OperationsMgr) handleActiveJob(
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
	entryID, err := cm.addCronJob(ctx, name, update.Spec, update.Type, update.Config)
	if err != nil {
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
func (cm *OperationsMgr) jobNeedsUpdate(
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

func (cm *OperationsMgr) syncCronJob() {
	db := query.GetDB()
	err := query.GetDB().Transaction(func(tx *gorm.DB) error {
		var configs []*model.CronJobConfig
		if err := db.Where(query.CronJobConfig.Suspend.Is(false)).Find(&configs).Error; err != nil {
			klog.Errorf("OperationMgr.syncCronJob: failed to load cron job configs: %v", err)
			cm.cron.Start()
			return nil
		}
		klog.Infof("OperationMgr.syncCronJob: loaded %d non-suspended cron jobs from database", len(configs))

		for _, conf := range configs {
			entryID, err := cm.addCronJob(nil, conf.Name, conf.Spec, conf.Type, conf.Config)
			if err != nil {
				err := fmt.Errorf("OperationMgr.addCronJob: failed to add cron job %s with spec %s: %w", conf.Name, conf.Spec, err)
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
					klog.Warningf("DB failed to update entry_id for job %s: %v", conf.Name, err)
				}
			}
		}
		return nil
	})

	if err != nil {
		klog.Error(err)
	}

	cm.cron.Start()
	klog.Info("OperationMgr.syncCronJob: cron scheduler started")
}

func (mgr *OperationsMgr) NewHTTPCallCronJob(
	ctx *gin.Context,
	jobName string,
	method string,
	url string,
	queryParams map[string]string,
	payload map[string]any,
) (cron.FuncJob, error) {
	method = strings.ToUpper(method)
	if !lo.Contains([]string{http.MethodGet, http.MethodDelete, http.MethodPost, http.MethodPut}, method) {
		return nil, fmt.Errorf("invalid http method %s", method)
	}

	url, err := MergeURLWithQuery(url, queryParams)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	payloadJson, err := json.Marshal(payload)
	if err != nil {
		err := fmt.Errorf("MergeURLWithQuery failed: %w", err)
		klog.Error(err)
		return nil, err
	}

	username, password, err := mgr.GetConfigMapCredentials(ctx)
	if err != nil {
		err := fmt.Errorf("OperationsMgr.GetConfigMapCredentials failed: %w", err)
		klog.Error(err)
		return nil, err
	}
	accessToken, err := GetAdminTokenByLogin(ctx, username, password, mgr.serverHandler)
	if err != nil {
		err := fmt.Errorf("GetAdminTokenByLogin failed: %w", err)
		klog.Error(err)
		return nil, err
	}
	jobReq := httptest.NewRequest(method, url, bytes.NewReader(payloadJson))
	jobReq.Header.Set("Content-Type", "application/json")
	jobReq.Header.Set("Authorization", "Bearer "+accessToken)

	funcJob := func() {
		w := httptest.NewRecorder()
		executeTime := time.Now()
		mgr.serverHandler.ServeHTTP(w, jobReq)

		body := w.Body.Bytes()

		klog.Infof("CronJob HTTP call completed: %s %s, status: %d, res: %s", method, url, w.Code, body)
		success := true
		if w.Code >= http.StatusBadRequest {
			klog.Warningf("CronJob HTTP call failed with status %d: %s", w.Code, w.Body.String())
			success = false
		}

		rec := &model.CronJobRecord{
			Name:        jobName,
			ExecuteTime: executeTime,
			Success:     ptr.To(success),
			Message:     "",
			JobData:     datatypes.JSON(body),
		}
		db := query.GetDB()
		if err := db.Model(rec).Create(rec).Error; err != nil {
			klog.Errorf("DB failed to create record of http-call cron job %s: %v", jobName, err)
		}
	}

	return funcJob, nil
}

func (cm *OperationsMgr) GetConfigMapCredentials(c *gin.Context) (username, password string, err error) {
	configMap := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Namespace: "crater",
		Name:      "crater-cronjob-config",
	}

	if err := cm.client.Get(c, namespacedName, configMap); err != nil {
		return "", "", fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	username = configMap.Data["USERNAME"]
	password = configMap.Data["PASSWORD"]

	if username == "" || password == "" {
		return "", "", fmt.Errorf("USERNAME or PASSWORD not found in ConfigMap data")
	}

	return username, password, nil
}

func (cm *OperationsMgr) GetAllCronJobs(ctx *gin.Context) ([]*model.CronJobConfig, error) {
	var configs []*model.CronJobConfig
	if err := query.GetDB().WithContext(ctx).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

func (mgr *OperationsMgr) GetCronjobNames(c *gin.Context) {
	names := make([]string, 0)
	if err := query.GetDB().WithContext(c).Model(&model.CronJobConfig{}).Select("name").Find(&names).Error; err != nil {
		klog.Error(err)
		resputil.Error(c, err.Error(), resputil.ServiceError)
		return
	}
	resputil.Success(c, names)
}

func (mgr *OperationsMgr) GetCronjobRecordTimeRange(c *gin.Context) {
	var result struct {
		StartTime time.Time
		EndTime   time.Time
	}
	err := query.
		GetDB().
		WithContext(c).
		Model(&model.CronJobRecord{}).
		Select("min(execute_time) as start_time", "max(execute_time) as end_time").
		Scan(&result).
		Error
	if err != nil {
		klog.Error(err)
		resputil.Error(c, err.Error(), resputil.ServiceError)
		return
	}
	// 最小时间向下取整到当天的 00:00:00
	startTime := result.StartTime.AddDate(0, 0, -1)
	endTime := result.EndTime.AddDate(0, 0, 1)

	resputil.Success(c, map[string]any{
		"startTime": startTime,
		"endTime":   endTime,
	})
}

type GetCronJobRecordsReq struct {
	Name      []string   `json:"name" form:"name"`
	StartTime *time.Time `json:"startTime" form:"startTime"`
	EndTime   *time.Time `json:"endTime" form:"endTime"`
	Success   *bool      `json:"success" form:"success"`

	PageNum  int `json:"pageNum" form:"pageNum"`
	PageSize int `json:"pageSize" form:"pageSize"`
}

func (cm *OperationsMgr) GetCronjobRecords(c *gin.Context) {
	req := &GetCronJobRecordsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		klog.Error(err)
		c.JSON(http.StatusBadRequest, err.Error())
		return
	}

	pageNum := 1
	pageSize := 10
	if req.PageNum > 0 {
		pageNum = req.PageNum
	}
	if req.PageSize > 0 {
		pageSize = req.PageSize
	}

	var (
		records []*model.CronJobRecord
		total   int64
	)
	g, groupCtx := errgroup.WithContext(c)
	g.SetLimit(MAX_GO_ROUTINE_NUM)
	g.Go(func() error {
		tx := query.GetDB().WithContext(groupCtx)
		if len(req.Name) > 0 {
			tx = tx.Where(query.CronJobRecord.Name.In(req.Name...))
		}
		if req.StartTime != nil {
			tx = tx.Where(query.CronJobRecord.ExecuteTime.Gte(*req.StartTime))
		}
		if req.EndTime != nil {
			tx = tx.Where(query.CronJobRecord.ExecuteTime.Lte(*req.EndTime))
		}
		if req.Success != nil {
			tx = tx.Where(query.CronJobRecord.Success.Is(*req.Success))
		}
		err := tx.
			Offset((pageNum - 1) * pageSize).
			Limit(pageSize).
			Find(&records).Error
		if err != nil {
			klog.Error(err)
			return err
		}
		return nil
	})

	g.Go(func() error {
		tx := query.GetDB().WithContext(groupCtx)
		if len(req.Name) > 0 {
			tx = tx.Where(query.CronJobRecord.Name.In(req.Name...))
		}
		if req.StartTime != nil {
			tx = tx.Where(query.CronJobRecord.ExecuteTime.Gte(*req.StartTime))
		}
		if req.EndTime != nil {
			tx = tx.Where(query.CronJobRecord.ExecuteTime.Lte(*req.EndTime))
		}
		if req.Success != nil {
			tx = tx.Where(query.CronJobRecord.Success.Is(*req.Success))
		}

		err := tx.
			Model(&model.CronJobRecord{}).
			Count(&total).
			Error
		if err != nil {
			klog.Error(err)
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		klog.Error(err)
		resputil.Error(c, err.Error(), resputil.ServiceError)
		return
	}

	resputil.Success(c, map[string]any{
		"records": records,
		"total":   total,
	})
}

type DeleteCronJobRecordsReq struct {
	ID        []uint     `json:"id"`
	StartTime *time.Time `json:"startTime"`
	EndTime   *time.Time `json:"endTime"`
}

func (cm *OperationsMgr) DeleteCronjobRecords(c *gin.Context) {
	req := &DeleteCronJobRecordsReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		klog.Error(err)
		resputil.Error(c, err.Error(), resputil.InvalidRequest)
		return
	}
	if len(req.ID) == 0 && req.StartTime == nil && req.EndTime == nil {
		resputil.Error(c, "id or startTime or endTime is required", resputil.InvalidRequest)
		return
	}

	tx := query.GetDB().WithContext(c)

	if len(req.ID) > 0 {
		tx = tx.Where(query.CronJobRecord.ID.In(req.ID...))
	}

	if req.StartTime != nil {
		tx = tx.Where(query.CronJobRecord.ExecuteTime.Gte(*req.StartTime))
	}
	if req.EndTime != nil {
		tx = tx.Where(query.CronJobRecord.ExecuteTime.Lte(*req.EndTime))
	}

	res := tx.Delete(&model.CronJobRecord{})
	if err := res.Error; err != nil {
		klog.Error(err)
		resputil.Error(c, err.Error(), resputil.ServiceError)
		return
	}

	resputil.Success(c, map[string]string{
		"deleted": fmt.Sprintf("%d", res.RowsAffected),
	})
}
