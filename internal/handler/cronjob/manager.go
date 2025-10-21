package cronjob

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/samber/lo"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/raids-lab/crater/internal/resputil"

	corev1 "k8s.io/api/core/v1"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/handler"
)

var (
	instance *CronJobManager
	once     sync.Once
)

type CronJobManager struct {
	name          string
	client        client.Client
	cron          *cron.Cron
	serverHandler http.Handler
	mu            sync.RWMutex
}

//nolint:gochecknoinits // This is the standard way to register a gin handler.
func init() {
	handler.Registers = append(handler.Registers, NewCronJobManager)
}

type JobEntry struct {
	EntryID cron.EntryID
	Name    string
}

func NewCronJobManager(c *handler.RegisterConfig) handler.Manager {
	once.Do(func() {
		instance = &CronJobManager{
			name:   "cronjob",
			client: c.Client,
			cron:   cron.New(cron.WithLocation(time.Local)),
		}
	})
	return instance
}

func GetCronJobManager() *CronJobManager {
	return instance
}

func (cm *CronJobManager) InitServerHandler(serverHandler http.Handler) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.serverHandler = serverHandler
	instance.syncCronJob()
}

type HttpCall struct {
	Method  string            `json:"method"`
	Url     string            `json:"url"`
	Query   map[string]string `json:"query"`
	Payload map[string]any    `json:"payload"`
}

func (cm *CronJobManager) addCronJob(
	ctx *gin.Context,
	jobName string,
	jobSpec string,
	jobType model.CronJobType,
	jobConfig datatypes.JSON,
) (cron.EntryID, error) {
	var entryID cron.EntryID
	if jobType == model.CronJobTypeHTTPCall {
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
		entryID, err = cm.cron.AddFunc(jobSpec, f) // must be the final error
		if err != nil {
			klog.Error(err)
			return -1, err
		}
	} else {
		return -1, fmt.Errorf("unsupported cron job type: %s", jobType)
	}
	return entryID, nil
}

func (cm *CronJobManager) UpdateJob(
	ctx *gin.Context,
	name string,
	jobType *model.CronJobType,
	spec *string,
	suspend *bool,
	config *string,
) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

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

// getCurrentJobConfigFromDB retrieves current job configuration from database
func (cm *CronJobManager) getCurrentJobConfigFromDB(tx *gorm.DB, name string) (*model.CronJobConfig, error) {
	cur := &model.CronJobConfig{}
	if txErr := tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).First(cur).Error; txErr != nil {
		err := fmt.Errorf("DB failed to query: %w", txErr)
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

// handleJobSuspension handles suspending an active job
func (cm *CronJobManager) handleJobSuspension(
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
func (cm *CronJobManager) handleActiveJob(
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

func (cm *CronJobManager) syncCronJob() {
	db := query.GetDB()
	err := query.GetDB().Transaction(func(tx *gorm.DB) error {
		var configs []model.CronJobConfig
		if err := db.Where(query.CronJobConfig.Suspend.Is(false)).Find(&configs).Error; err != nil {
			klog.Errorf("CronJobManager.syncCronJob: failed to load cron job configs: %v", err)
			cm.cron.Start()
			return nil
		}
		klog.Infof("CronJobManager.syncCronJob: loaded %d non-suspended cron jobs from database", len(configs))

		for _, conf := range configs {
			entryID, err := cm.addCronJob(nil, conf.Name, conf.Spec, conf.Type, conf.Config)
			if err != nil {
				err := fmt.Errorf("CronJobManager.addCronJob: failed to add cron job %s with spec %s: %w", conf.Name, conf.Spec, err)
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
	klog.Info("CronJobManager.syncCronJob: cron scheduler started")
}

func (cm *CronJobManager) GetAllCronJobs(ctx *gin.Context) ([]*model.CronJobConfig, error) {
	var configs []*model.CronJobConfig
	if err := query.GetDB().WithContext(ctx).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

func (cm *CronJobManager) Stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cron.Stop()
}

func (mgr *CronJobManager) NewHTTPCallCronJob(
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
		err := fmt.Errorf("CronJobManager.GetConfigMapCredentials failed: %w", err)
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

func (cm *CronJobManager) GetConfigMapCredentials(_ *gin.Context) (username, password string, err error) {
	configMap := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Namespace: "crater",
		Name:      "crater-cronjob-config",
	}

	if err := cm.client.Get(context.Background(), namespacedName, configMap); err != nil {
		return "", "", fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	username = configMap.Data["USERNAME"]
	password = configMap.Data["PASSWORD"]

	if username == "" || password == "" {
		return "", "", fmt.Errorf("USERNAME or PASSWORD not found in ConfigMap data")
	}

	return username, password, nil
}

func (mgr *CronJobManager) GetName() string { return mgr.name }

func (mgr *CronJobManager) RegisterPublic(g *gin.RouterGroup) {
	g.POST("/ping", mgr.Pong)
}

func (mgr *CronJobManager) RegisterProtected(g *gin.RouterGroup) {
	g.POST("/ping", mgr.Pong)
}

func (mgr *CronJobManager) RegisterAdmin(g *gin.RouterGroup) {
	g.POST("/ping", mgr.Pong)
}

func (mgr *CronJobManager) Pong(ctx *gin.Context) {
	resputil.Success(ctx, "pong")
}
