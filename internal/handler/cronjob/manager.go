package cronjob

import (
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/internal/handler"
)

var (
	instance *CronJobManager
)

type CronJobManager struct {
	name    string
	cron    *cron.Cron
	entries map[string]*JobEntry
	mu      sync.RWMutex
}

//nolint:gochecknoinits // This is the standard way to register a gin handler.
func init() {
	handler.Registers = append(handler.Registers, NewCronJobManager)
}

type JobEntry struct {
	EntryID cron.EntryID
	Spec    string
	Suspend bool
	FuncJob cron.FuncJob
	Config  map[string]string
}

func NewCronJobManager(_ *handler.RegisterConfig) handler.Manager {
	instance = &CronJobManager{
		name:    "cronjob",
		cron:    cron.New(cron.WithLocation(time.Local)),
		entries: make(map[string]*JobEntry),
	}
	return instance
}

func GetCronJobManager() *CronJobManager {
	return instance
}

func (cm *CronJobManager) AddJob(name, spec string, cmd func()) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.addJob(name, spec, cmd); err != nil {
		return err
	}
	return nil
}

func (cm *CronJobManager) addJob(name, spec string, cmd func()) error {
	if _, exists := cm.entries[name]; exists {
		return fmt.Errorf("job %s already exists", name)
	}

	entryID, err := cm.cron.AddFunc(spec, cmd)
	if err != nil {
		return err
	}
	cm.entries[name] = &JobEntry{
		EntryID: entryID,
		Spec:    spec,
		Suspend: false,
		FuncJob: cmd,
	}
	return nil
}

func (cm *CronJobManager) UpdateJob(name, spec string, suspend bool, funcJob *cron.FuncJob) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	jobEntry, exists := cm.entries[name]
	if !exists || jobEntry == nil { // need to add the job
		if funcJob == nil {
			err := fmt.Errorf("CronJobManager.UpdateJob failed: funcJob is nil")
			klog.Error(err.Error())
			return err
		}
		if err := cm.addJob(name, spec, *funcJob); err != nil {
			err := fmt.Errorf("CronJobManager.UpdateJob failed: %w", err)
			klog.Error(err.Error())
			return err
		}
		jobEntry = cm.entries[name]
		klog.Warningf("job %s not found", name)
	}

	cm.cron.Remove(jobEntry.EntryID)
	jobEntry.Spec = spec
	jobEntry.Suspend = suspend
	if funcJob != nil {
		jobEntry.FuncJob = *funcJob
	}
	if !suspend {
		entryID, err := cm.cron.AddFunc(spec, jobEntry.FuncJob)
		if err != nil {
			err := fmt.Errorf("CronJobManager.UpdateJob cannot add job %s: %w", name, err)
			return err
		}
		jobEntry.EntryID = entryID
	}

	return nil
}

func (cm *CronJobManager) RemoveJob(name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if jobEntry, exists := cm.entries[name]; exists {
		cm.cron.Remove(jobEntry.EntryID)
		delete(cm.entries, name)
	}
}

func (cm *CronJobManager) Start() {
	cm.cron.Start()
}

func (cm *CronJobManager) Stop() {
	cm.cron.Stop()
}

func (mgr *CronJobManager) GetName() string { return mgr.name }

func (mgr *CronJobManager) RegisterPublic(_ *gin.RouterGroup) {
}

func (mgr *CronJobManager) RegisterProtected(_ *gin.RouterGroup) {
}

func (mgr *CronJobManager) RegisterAdmin(_ *gin.RouterGroup) {
}
