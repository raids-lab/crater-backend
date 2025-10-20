package cronjob

import (
	"bytes"
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
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/raids-lab/crater/internal/resputil"

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
	entries       map[string]*JobEntry
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
	Spec    string
	Type    model.CronJobType
	Suspend bool
}

func NewCronJobManager(c *handler.RegisterConfig) handler.Manager {
	once.Do(func() {
		instance = &CronJobManager{
			name:    "cronjob",
			client:  c.Client,
			cron:    cron.New(cron.WithLocation(time.Local)),
			entries: make(map[string]*JobEntry),
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
	suspend bool,
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
		cm.entries[jobName] = &JobEntry{
			EntryID: entryID,
			Name:    jobName,
			Spec:    jobSpec,
			Suspend: suspend,
			Type:    jobType,
		}
	} else {
		return -1, fmt.Errorf("unsupported cron job type: %s", jobType)
	}
	return entryID, nil
}

func (cm *CronJobManager) UpdateJob(
	ctx *gin.Context,
	name string,
	jobType model.CronJobType,
	spec string,
	suspend bool,
	config *string,
) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	err := query.GetDB().Transaction(func(tx *gorm.DB) error {
		cur, err := cm.getCurrentJobConfigFromDB(tx, name)
		if err != nil {
			return err
		}

		update := cm.prepareUpdateConfig(jobType, spec, suspend, cur.Config, config)

		// Handle suspend state transition
		if cm.shouldSuspendJob(cur.Suspend, suspend) {
			return cm.handleJobSuspension(tx, name, cur, update)
		}

		// Handle active job (not suspended)
		if !suspend {
			return cm.handleActiveJob(ctx, tx, name, cur, update, jobType, spec, config)
		}

		return nil
	})
	return err
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
	jobType model.CronJobType,
	spec string,
	suspend bool,
	currentConfig datatypes.JSON,
	config *string,
) *model.CronJobConfig {
	update := &model.CronJobConfig{
		Type:    jobType,
		Spec:    spec,
		Suspend: suspend,
		Config:  currentConfig,
	}
	if config != nil {
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
	jobEntry, exists := cm.entries[name]
	if !exists {
		return nil
	}

	cm.cron.Remove(jobEntry.EntryID)
	delete(cm.entries, name)
	update.EntryID = -1
	return tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error
}

// handleActiveJob handles job activation or update
func (cm *CronJobManager) handleActiveJob(
	ctx *gin.Context,
	tx *gorm.DB,
	name string,
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
	jobType model.CronJobType,
	spec string,
	config *string,
) error {
	// Job was suspended, now activating
	if cur.Suspend {
		return cm.activateJob(ctx, tx, name, cur, update, jobType, spec)
	}

	// Job is already active, check if needs update
	return cm.updateActiveJobIfNeeded(ctx, tx, name, cur, update, jobType, spec, config)
}

// activateJob activates a previously suspended job
func (cm *CronJobManager) activateJob(
	ctx *gin.Context,
	tx *gorm.DB,
	name string,
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
	jobType model.CronJobType,
	spec string,
) error {
	entryID, err := cm.addCronJob(ctx, name, spec, false, jobType, update.Config)
	if err != nil {
		klog.Error(err)
		return err
	}
	update.EntryID = int(entryID)
	return tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error
}

// updateActiveJobIfNeeded updates an active job if configuration changed
func (cm *CronJobManager) updateActiveJobIfNeeded(
	ctx *gin.Context,
	tx *gorm.DB,
	name string,
	cur *model.CronJobConfig,
	update *model.CronJobConfig,
	jobType model.CronJobType,
	spec string,
	config *string,
) error {
	jobEntry, exists := cm.entries[name]
	if !exists {
		return nil
	}

	if !cm.jobNeedsUpdate(jobEntry, cur, jobType, spec, config) {
		return nil
	}

	// Remove old entry and add new one
	cm.cron.Remove(jobEntry.EntryID)
	entryID, err := cm.addCronJob(ctx, name, spec, false, jobType, update.Config)
	if err != nil {
		klog.Error(err)
		return err
	}
	update.EntryID = int(entryID)
	return tx.Model(cur).Where(query.CronJobConfig.Name.Eq(name)).Updates(update).Error
}

// jobNeedsUpdate checks if job configuration has changed
func (cm *CronJobManager) jobNeedsUpdate(
	jobEntry *JobEntry,
	cur *model.CronJobConfig,
	jobType model.CronJobType,
	spec string,
	config *string,
) bool {
	if jobEntry.EntryID != cron.EntryID(cur.EntryID) {
		return true
	}
	if jobEntry.Type != jobType {
		return true
	}
	if jobEntry.Spec != spec {
		return true
	}
	if config != nil && string(cur.Config) != *config {
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
			entryID, err := cm.addCronJob(nil, conf.Name, conf.Spec, conf.Suspend, conf.Type, conf.Config)
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

	accessToken, err := GetAdminTokenByLogin(ctx, mgr.serverHandler)
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
			Success:     success,
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
