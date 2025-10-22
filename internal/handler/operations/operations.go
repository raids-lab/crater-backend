package operations

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/raids-lab/crater/internal/handler"
	"github.com/raids-lab/crater/pkg/aitaskctl"
	"github.com/raids-lab/crater/pkg/monitor"
)

//nolint:gochecknoinits // This is the standard way to register a gin handler.
func init() {
	handler.Registers = append(handler.Registers, NewOperationsMgr)
}

type OperationsMgr struct {
	name           string
	client         client.Client
	kubeClient     kubernetes.Interface
	promClient     monitor.PrometheusInterface
	taskService    aitaskctl.DBService
	taskController aitaskctl.TaskControllerInterface

	// cron
	cron          *cron.Cron
	serverHandler http.Handler
	cronMutex     sync.RWMutex
}

var (
	instance *OperationsMgr
	once     sync.Once
)

func NewOperationsMgr(conf *handler.RegisterConfig) handler.Manager {
	once.Do(func() {
		instance = &OperationsMgr{
			name:           "operations",
			client:         conf.Client,
			kubeClient:     conf.KubeClient,
			promClient:     conf.PrometheusClient,
			taskService:    aitaskctl.NewDBService(),
			taskController: conf.AITaskCtrl,

			cron: cron.New(cron.WithLocation(time.Local)),
		}
	})
	return instance
}

func GetOperationsMgrInstance() *OperationsMgr {
	return instance
}

func (mgr *OperationsMgr) GetName() string { return mgr.name }

func (mgr *OperationsMgr) RegisterPublic(_ *gin.RouterGroup) {
}

func (mgr *OperationsMgr) RegisterProtected(_ *gin.RouterGroup) {
}

func (mgr *OperationsMgr) RegisterAdmin(g *gin.RouterGroup) {
	g.GET("/whitelist", mgr.GetWhiteList)
	g.PUT("/keep/:name", mgr.SetKeepWhenLowResourceUsage)
	g.GET("/cronjob", mgr.GetCronjobConfigs)
	g.PUT("/cronjob", mgr.UpdateCronjobConfig)
	g.PUT("/add/locktime", mgr.AddLockTime)
	g.PUT("/clear/locktime", mgr.ClearLockTime)
	g.DELETE("/cronjob/record", mgr.DeleteCronjobRecords)
}

func (cm *OperationsMgr) InitServerHandler(serverHandler http.Handler) {
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()
	cm.serverHandler = serverHandler
	cm.syncCronJob()
}

func (cm *OperationsMgr) StopCron() {
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()
	cm.cron.Stop()
}
