package operations

import (
	"github.com/gin-gonic/gin"
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
}

var instance *OperationsMgr

func NewOperationsMgr(conf *handler.RegisterConfig) handler.Manager {
	instance = &OperationsMgr{
		name:           "operations",
		client:         conf.Client,
		kubeClient:     conf.KubeClient,
		promClient:     conf.PrometheusClient,
		taskService:    aitaskctl.NewDBService(),
		taskController: conf.AITaskCtrl,
	}
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
	g.DELETE("/auto", mgr.HandleLowGPUUsageJobs)
	g.PUT("/keep/:name", mgr.SetKeepWhenLowResourceUsage)
	g.DELETE("/cleanup", mgr.HandleLongTimeRunningJobs)
	g.DELETE("/waiting/jupyter", mgr.HandleWaitingJupyterJobs)
	g.GET("/cronjob", mgr.GetCronjobConfigs)
	g.PUT("/cronjob", mgr.UpdateCronjobConfig)
	g.PUT("/add/locktime", mgr.AddLockTime)
	g.PUT("/clear/locktime", mgr.ClearLockTime)

	// Cronjob 记录查询 API (可选功能，在运行 make curd 后取消注释)
	// g.GET("/cronjob/records", mgr.GetCronjobRecords)
	// g.GET("/cronjob/records/:id", mgr.GetCronjobRecordDetail)
	// g.GET("/cronjob/statistics", mgr.GetCronjobStatistics)
}
