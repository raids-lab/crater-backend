package operations

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cj "github.com/raids-lab/crater/internal/handler/cronjob"

	"github.com/raids-lab/crater/internal/resputil"
)

type CronjobConfigs struct {
	Name     string            `json:"name"`
	Schedule string            `json:"schedule"`
	Suspend  bool              `json:"suspend"`
	Configs  map[string]string `json:"configs"`
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
	var err error
	if err = c.ShouldBindJSON(&req); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	fmt.Println(req)
	var funcJob func()
	switch req.Name {
	case CLEAN_LONG_TIME_CRON_JOB_NAME:
		funcJob, err = mgr.CreateHandleLongTimeRunningJobsCronJob(c)
	case CLEAN_LOW_GPU_UTIL_CRON_JOB_NAME:
		funcJob, err = mgr.CreateHandleLowGPUUsageJobsCronJob(c)
	case CLEAN_WAITING_JUPYTER:
		funcJob, err = mgr.CreateHandleWaitingJupyterJobsCronJob(c)
	default:
		err = fmt.Errorf("invalid cronjob name: %s", req.Name)
		klog.Error(err.Error())
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	if err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	if err := cj.GetCronJobManager().UpdateJob(req.Name, req.Schedule, req.Suspend, (*cron.FuncJob)(&funcJob)); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	resputil.Success(c, "Successfully update cronjob config")
}

//nolint:unused // depreciated
func (mgr *OperationsMgr) updateCronjobConfig(c *gin.Context, cronjobConfigs CronjobConfigs) error {
	namespace := CRONJOBNAMESPACE
	cronjob, err := mgr.kubeClient.BatchV1().CronJobs(namespace).Get(c, cronjobConfigs.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get cronjob %s: %w", cronjobConfigs.Name, err)
	}

	// 更新 schedule
	cronjob.Spec.Schedule = cronjobConfigs.Schedule

	// 更新 suspend 字段
	*cronjob.Spec.Suspend = cronjobConfigs.Suspend

	// CronJob 确保只有一个 container
	if len(cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("cronjob %s has no container", cronjobConfigs.Name)
	}
	container := &cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]

	// 检查并更新 env 变量，确保传入的 env 必须已经存在
	for key, newVal := range cronjobConfigs.Configs {
		found := false
		for idx, env := range container.Env {
			if env.Name == key {
				container.Env[idx].Value = newVal
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("container %s missing env key: %s", container.Name, key)
		}
	}

	// 更新 CronJob 对象
	_, err = mgr.kubeClient.BatchV1().CronJobs(namespace).Update(c, cronjob, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update cronjob %s: %w", cronjobConfigs.Name, err)
	}
	return nil
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
	configs, err := mgr.getCronjobConfigs(c)
	if err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	resputil.Success(c, configs)
}

func (mgr *OperationsMgr) getCronjobConfigs(c *gin.Context) ([]CronjobConfigs, error) {
	// get all cronjobs in namespace crater, label crater.raids-lab.io/component=cronjob
	// apiVersion: batch/v1, kind: CronJob
	cronjobConfigList := make([]CronjobConfigs, 0)
	namespace := CRONJOBNAMESPACE
	labelSelector := fmt.Sprintf("%s=%s", CRAONJOBLABELKEY, CRONJOBLABELVALUE)
	cronjobs, err := mgr.kubeClient.BatchV1().CronJobs(namespace).List(c, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		klog.Errorf("Failed to get cronjobs: %v", err)
		return nil, err
	}
	for i := range cronjobs.Items {
		cronjob := &cronjobs.Items[i]
		configs := make(map[string]string)

		// cronjob 确保只有一个 container
		containers := cronjob.Spec.JobTemplate.Spec.Template.Spec.Containers
		if len(containers) != 1 {
			continue
		}
		container := containers[0]
		for _, env := range container.Env {
			configs[env.Name] = env.Value
		}
		cronjobConfigList = append(cronjobConfigList, CronjobConfigs{
			Name:     cronjob.Name,
			Configs:  configs,
			Suspend:  *cronjob.Spec.Suspend,
			Schedule: cronjob.Spec.Schedule,
		})
	}
	return cronjobConfigList, nil
}

func (mgr *OperationsMgr) createCronjobHandler(
	c *gin.Context,
	jobName string,
	configStruct any,
	handler func(any) (any, error),
) (func(), error) {
	return func() {
		namespace := CRONJOBNAMESPACE
		conf := &v1.ConfigMap{}
		err := mgr.client.Get(c, client.ObjectKey{
			Namespace: namespace,
			Name:      jobName,
		}, conf)
		if err != nil {
			klog.Errorf("failed to get configmap %s: %v", jobName, err)
			resputil.Error(c, fmt.Sprintf("failed to get configmap: %v", err), resputil.NotSpecified)
			return
		}

		if err := parseConfigToStruct(conf.Data, configStruct); err != nil {
			klog.Errorf("failed to parse config for %s: %v", jobName, err)
			resputil.Error(c, fmt.Sprintf("failed to parse config: %v", err), resputil.NotSpecified)
			return
		}

		result, err := handler(configStruct)
		if err != nil {
			klog.Errorf("failed to execute handler for %s: %v", jobName, err)
			resputil.Error(c, err.Error(), resputil.NotSpecified)
			return
		}

		resputil.Success(c, result)
	}, nil
}

func (mgr *OperationsMgr) CreateHandleLongTimeRunningJobsCronJob(c *gin.Context) (func(), error) {
	config := &LongTimeJobConfig{}
	return mgr.createCronjobHandler(c, CLEAN_LONG_TIME_CRON_JOB_NAME, config, func(cfg any) (any, error) {
		conf := cfg.(*LongTimeJobConfig)
		if conf.BatchDays < 0 || conf.InteractiveDays < 0 {
			return nil, fmt.Errorf("invalid parameter (batchDays: %d, interactiveDays: %d)", conf.BatchDays, conf.InteractiveDays)
		}

		batchJobTimeout := time.Duration(conf.BatchDays) * 24 * time.Hour
		interactiveJobTimeout := time.Duration(conf.InteractiveDays) * 24 * time.Hour
		defaultRemindTime := 24 * time.Hour

		remindJobList, deletionJobList := mgr.handleLongTimeRunningJobs(c, batchJobTimeout, interactiveJobTimeout, defaultRemindTime)
		return map[string][]string{
			"reminded": remindJobList,
			"deleted":  deletionJobList,
		}, nil
	})
}

func (mgr *OperationsMgr) CreateHandleLowGPUUsageJobsCronJob(c *gin.Context) (func(), error) {
	config := &LowGPUUtilJobConfig{}
	return mgr.createCronjobHandler(c, CLEAN_LOW_GPU_UTIL_CRON_JOB_NAME, config, func(cfg any) (any, error) {
		conf := cfg.(*LowGPUUtilJobConfig)
		if conf.TimeRange <= 0 || conf.WaitTime <= 0 {
			return nil, fmt.Errorf("invalid parameter (timeRange: %d, waitTime: %d)", conf.TimeRange, conf.WaitTime)
		}

		remindJobList, deletionJobList := mgr.handleLowGPUUsageJobs(c, conf.TimeRange, conf.WaitTime, conf.Util)
		return map[string][]string{
			"reminded": remindJobList,
			"deleted":  deletionJobList,
		}, nil
	})
}

func (mgr *OperationsMgr) CreateHandleWaitingJupyterJobsCronJob(c *gin.Context) (func(), error) {
	config := &WaitingJupyterConfig{}
	return mgr.createCronjobHandler(c, CLEAN_WAITING_JUPYTER, config, func(cfg any) (any, error) {
		conf := cfg.(*WaitingJupyterConfig)
		if conf.WaitMinutes < 0 {
			return nil, fmt.Errorf("waitMinutes must be greater than or equal to 0")
		}

		deletedJobs := mgr.deleteUnscheduledJupyterJobs(c, conf.WaitMinutes)
		return deletedJobs, nil
	})
}
