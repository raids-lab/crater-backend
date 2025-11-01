package cronjob

import (
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/raids-lab/crater/pkg/monitor"
)

const (
	VCJOBAPIVERSION = "batch.volcano.sh/v1alpha1"
	VCJOBKIND       = "Job"
	AIJOBAPIVERSION = "aisystem.github.com/v1alpha1"
	AIJOBKIND       = "AIJob"
)

const (
	CLEAN_LONG_TIME_RUNNING_JOB = "clean-long-time-job"
	CLEAN_LOW_GPU_USAGE_JOB     = "clean-low-gpu-util-job"
	CLEAN_WAITING_JUPYTER_JOB   = "clean-waiting-jupyter-job"
)

const (
	CONFIG_MAP_NAME         = "crater-cronjob-config"
	CONFIG_MAP_NAMESPACE    = "crater"
	CONFIG_MAP_USERNAME_KEY = "USERNAME"
	CONFIG_MAP_PASSWORD_KEY = "PASSWORD"
)

type CronJobManager struct {
	Client     client.Client
	KubeClient kubernetes.Interface
	PromClient monitor.PrometheusInterface
	cron       *cron.Cron
	cronMutex  sync.RWMutex
}

func NewCronJobManager(cli client.Client, kubeClient kubernetes.Interface, promClient monitor.PrometheusInterface) *CronJobManager {
	return &CronJobManager{
		Client:     cli,
		KubeClient: kubeClient,
		PromClient: promClient,
		cron:       cron.New(cron.WithLocation(time.Local)),
	}
}
