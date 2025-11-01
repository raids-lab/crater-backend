package cronjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/lo"
	"gorm.io/gorm"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/pkg/alert"
	"github.com/raids-lab/crater/pkg/config"
	"github.com/raids-lab/crater/pkg/utils"
)

type CleanLowGPUUsageRequest struct {
	TimeRange int `form:"timeRange" binding:"required"`
	WaitTime  int `form:"waitTime"`
	Util      int `form:"util"`
}

func (mgr *CronJobManager) CleanLowGPUUsageJobs(c context.Context, req *CleanLowGPUUsageRequest) (map[string][]string, error) {
	if req == nil {
		err := errors.New("invalid request")
		return nil, err
	}
	if req.TimeRange <= 0 || req.WaitTime <= 0 {
		err := errors.New("timeRange and waitTime must be greater than 0")
		return nil, err
	}

	remindJobList, deletionJobList := mgr.cleanLowGPUUsageJobs(c, req.TimeRange, req.WaitTime, req.Util)

	ret := map[string][]string{
		"reminded": remindJobList,
		"deleted":  deletionJobList,
	}
	return ret, nil
}

func (mgr *CronJobManager) cleanLowGPUUsageJobs(
	c context.Context, timeRange, waitTime, gpuUtil int) (remindJobList, deletionJobList []string) {
	remindJobList = []string{}
	deletionJobList = []string{}

	deletionJobs, reamindJobs, normalJobs := mgr.classifyLowGPUUsageJobs(c, timeRange, waitTime, gpuUtil)

	// 删除作业
	for _, job := range deletionJobs {
		err := mgr.freeLowGPUUsageVCjob(c, job)
		if err != nil {
			klog.Errorf("Failed to delete job %s: %v", job.JobName, err)
			continue
		}
		deletionJobList = append(deletionJobList, job.JobName)
	}

	// 提醒作业
	deleteTime := utils.GetLocalTime().Add(time.Duration(waitTime) * time.Minute)
	for _, job := range reamindJobs {
		err := mgr.remindLowGPUUsageVCjob(c, job, deleteTime)
		if err != nil {
			klog.Errorf("Failed to remind job %s: %v", job.JobName, err)
			continue
		}
		remindJobList = append(remindJobList, job.JobName)
	}

	// 对于GPU利用率恢复正常的作业，允许再次被提醒
	for _, job := range normalJobs {
		err := mgr.allowRepeatAlert(c, job, model.LowGPUJobRemindedAlert)
		if err != nil {
			klog.Errorf("Failed to allow repeat alert for job %s: %v", job.JobName, err)
			continue
		}
	}

	return remindJobList, deletionJobList
}

func (mgr *CronJobManager) allowRepeatAlert(c context.Context, job *model.Job, alertType model.AlertType) error {
	alertDB := query.Alert
	record, err := alertDB.WithContext(c).Where(
		alertDB.JobName.Eq(job.JobName),
		alertDB.AlertType.Eq(alertType.String()),
	).First()

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	}

	// 允许再次被提醒
	record.AllowRepeat = true
	if err := alertDB.WithContext(c).Save(record); err != nil {
		return err
	}

	return nil
}

func (mgr *CronJobManager) freeLowGPUUsageVCjob(c context.Context, job *model.Job) error {
	err := mgr.deleteVCjobInCluster(c, job)
	if err != nil {
		return err
	}

	if !job.AlertEnabled {
		// 不需要发送邮件
		return nil
	}

	// 发送邮件
	alertMgr := alert.GetAlertMgr()
	if err := alertMgr.DeleteJob(c, job.JobName, nil); err != nil {
		klog.Errorf("Send Alarm Email failed for job %s", job.JobName)
	}

	return nil
}

func (mgr *CronJobManager) remindLowGPUUsageVCjob(c context.Context, job *model.Job, deleteTime time.Time) error {
	if !job.AlertEnabled {
		// 不需要发送邮件
		klog.Infof("Job %s is not alert enabled", job.JobName)
		return nil
	}

	// 发送邮件
	alertMgr := alert.GetAlertMgr()
	if err := alertMgr.RemindLowUsageJob(c, job.JobName, deleteTime, nil); err != nil {
		klog.Errorf("Send Alarm Email failed for job %s", job.JobName)
		return err
	}

	return nil
}

func (mgr *CronJobManager) deleteVCjobInCluster(c context.Context, job *model.Job) error {
	vcjob := &batch.Job{}
	namespace := config.GetConfig().Namespaces.Job
	if err := mgr.Client.Get(c, client.ObjectKey{Name: job.JobName, Namespace: namespace}, vcjob); err != nil {
		return err
	}

	if err := mgr.Client.Delete(c, vcjob); err != nil {
		return err
	}
	return nil
}

func (mgr *CronJobManager) classifyLowGPUUsageJobs(
	c context.Context, timeRange, waitTime, gpuUtil int) (deletionJobs, reamindJobs, normalJobs []*model.Job) {
	// 返回待删除作业、待提醒作业、正常作业
	// 只考虑vcjob
	jobDB := query.Job
	deletionJobs = mgr.getLowGPUUsageVCjobs(c, timeRange+waitTime, gpuUtil)
	toRemindJobs := mgr.getLowGPUUsageVCjobs(c, timeRange, gpuUtil)
	runningJobs, _ := jobDB.WithContext(c).Where(jobDB.Status.Eq(string(batch.Running))).Find()

	deletionMap := make(map[string]bool)
	for _, job := range deletionJobs {
		deletionMap[job.JobName] = true
	}

	reamindJobs = []*model.Job{}
	reamindMap := make(map[string]bool)
	for _, job := range toRemindJobs {
		if _, ok := deletionMap[job.JobName]; ok {
			continue
		}
		reamindMap[job.JobName] = true
		reamindJobs = append(reamindJobs, job)
	}

	normalJobs = []*model.Job{}
	for _, job := range runningJobs {
		if _, ok := deletionMap[job.JobName]; ok {
			continue
		}
		if _, ok := reamindMap[job.JobName]; ok {
			continue
		}
		normalJobs = append(normalJobs, job)
	}
	return deletionJobs, reamindJobs, normalJobs
}

func (mgr *CronJobManager) getLowGPUUsageVCjobs(c context.Context, duration, gpuUtil int) []*model.Job {
	jobDB := query.Job
	podList := mgr.getLowGPUUsagePods(c, duration, gpuUtil)

	whiteList, err := mgr.getJobWhiteList(c)
	if err != nil {
		// 拿不到白名单，就不进行清理，以免“误伤”
		klog.Errorf("Failed to get job white list: %v", err)
		return nil
	}

	jobList := []*model.Job{}
	for _, pod := range podList {
		if len(pod.OwnerReferences) == 0 {
			continue
		}

		owner := pod.OwnerReferences[0]

		if owner.Kind != VCJOBKIND || owner.APIVersion != VCJOBAPIVERSION {
			continue
		}

		// 过滤掉白名单中的作业
		if lo.Contains(whiteList, owner.Name) {
			continue
		}

		job, err := jobDB.WithContext(c).Where(jobDB.JobName.Eq(owner.Name)).First()
		if err != nil {
			klog.Infof("Fail to get vcjob %s\n", owner.Name)
			continue
		}

		jobList = append(jobList, job)
	}
	return jobList
}

func (mgr *CronJobManager) getJobWhiteList(c context.Context) ([]string, error) {
	var cleanList []string
	jobDB := query.Job
	curTime := utils.GetLocalTime()

	data, err := jobDB.WithContext(c).Where(jobDB.LockedTimestamp.Gt(curTime)).Find()

	if err != nil {
		return nil, err
	}
	for _, item := range data {
		cleanList = append(cleanList, item.JobName)
	}
	return cleanList, nil
}

func (mgr *CronJobManager) getLowGPUUsagePods(c context.Context, duration, gpuUtil int) []*v1.Pod {
	namespace := config.GetConfig().Namespaces.Job
	querys := mgr.PromClient.QueryNodeGPUUtilInNS(namespace)
	podList := []*v1.Pod{}

	for i := range querys {
		q := &querys[i]
		pod, err := mgr.KubeClient.CoreV1().
			Pods(namespace).
			Get(c, q.Pod, metav1.GetOptions{})
		if err != nil {
			continue
		}

		if mgr.PromClient.
			GetLeastUsedGPUJobList(q.Pod, fmt.Sprintf("%d", duration), fmt.Sprintf("%d", gpuUtil)) <= 0 {
			// 该Pod GPU利用率低于util
			// 或：该Pod生命周期小于duration
			continue
		}

		podList = append(podList, pod)
	}

	return podList
}

type CleanLongTimeRunningJobsRequest struct {
	BatchDays       *int `form:"batchDays"`
	InteractiveDays *int `form:"interactiveDays"`
}

func (mgr *CronJobManager) CleanLongTimeRunningJobs(c context.Context, req *CleanLongTimeRunningJobsRequest) (map[string][]string, error) {
	if req == nil {
		err := errors.New("invalid request")
		return nil, err
	}
	batchJobTimeout := 4 * 24 * time.Hour
	interactiveJobTimeout := 24 * time.Hour
	if req.BatchDays != nil {
		batchJobTimeout = time.Duration(*req.BatchDays) * 24 * time.Hour
	}
	if req.InteractiveDays != nil {
		interactiveJobTimeout = time.Duration(*req.InteractiveDays) * 24 * time.Hour
	}

	defaultRemindTime := 24 * time.Hour

	remindJobList, deletionJobList := mgr.cleanLongTimeRunningJobs(c, batchJobTimeout, interactiveJobTimeout, defaultRemindTime)
	ret := map[string][]string{
		"reminded": remindJobList,
		"deleted":  deletionJobList,
	}
	return ret, nil
}

func (mgr *CronJobManager) cleanLongTimeRunningJobs(
	c context.Context, batchJobTimeout, interactiveJobTimeout, defaultRemindTime time.Duration) (remindJobList, deletionJobList []string) {
	// 返回待删除作业、待提醒作业
	// 只考虑vcjob
	deletionJobs, reamindJobs := mgr.classifyLongTimeJobs(c, batchJobTimeout, interactiveJobTimeout, defaultRemindTime)
	deletionJobList = []string{}
	remindJobList = []string{}

	// 删除作业
	for _, job := range deletionJobs {
		err := mgr.freeLongTimeVCjob(c, job)
		if err != nil {
			klog.Errorf("Failed to delete job %s: %v", job.JobName, err)
			continue
		}
		deletionJobList = append(deletionJobList, job.JobName)
	}

	// 提醒作业
	deleteTime := utils.GetLocalTime().Add(defaultRemindTime)
	for _, job := range reamindJobs {
		err := mgr.remindLongTimeVCjob(c, job, deleteTime)
		if err != nil {
			klog.Errorf("Failed to remind job %s: %v", job.JobName, err)
			continue
		}
		remindJobList = append(remindJobList, job.JobName)
	}

	return
}

func (mgr *CronJobManager) freeLongTimeVCjob(c context.Context, job *model.Job) error {
	err := mgr.deleteVCjobInCluster(c, job)
	if err != nil {
		return err
	}

	if !job.AlertEnabled {
		// 不需要发送邮件
		return nil
	}

	// 发送邮件
	alertMgr := alert.GetAlertMgr()
	if err := alertMgr.CleanJob(c, job.JobName, nil); err != nil {
		klog.Errorf("Send Alarm Email failed for job %s", job.JobName)
		return err
	}

	return nil
}

func (mgr *CronJobManager) remindLongTimeVCjob(c context.Context, job *model.Job, deleteTime time.Time) error {
	if !job.AlertEnabled {
		// 不需要发送邮件
		klog.Infof("Job %s is not alert enabled", job.JobName)
		return nil
	}

	// 发送邮件
	alertMgr := alert.GetAlertMgr()
	if err := alertMgr.RemindLongTimeRunningJob(c, job.JobName, deleteTime, nil); err != nil {
		klog.Errorf("Send Alarm Email failed for job %s", job.JobName)
		return err
	}

	return nil
}

func (mgr *CronJobManager) classifyLongTimeJobs(
	c context.Context, batchJobTimeout, interactiveJobTimeout, defaultRemindTime time.Duration) (deletionJobs, reamindJobs []*model.Job) {
	deletionJobs = mgr.getLongTimeVCjobs(c, batchJobTimeout, interactiveJobTimeout)
	toRemindjobs := mgr.getLongTimeVCjobs(c, batchJobTimeout-defaultRemindTime, interactiveJobTimeout-defaultRemindTime)

	deletionMap := make(map[string]bool)
	for _, job := range deletionJobs {
		deletionMap[job.JobName] = true
	}
	reamindJobs = []*model.Job{}
	for _, job := range toRemindjobs {
		if _, ok := deletionMap[job.JobName]; ok {
			continue
		}
		reamindJobs = append(reamindJobs, job)
	}

	return
}

func (mgr *CronJobManager) getLongTimeVCjobs(c context.Context, batchTimeout, interactiveTimeout time.Duration) []*model.Job {
	jobDB := query.Job
	runningJobs, err := jobDB.WithContext(c).Where(jobDB.Status.Eq(string(batch.Running))).Find()

	if err != nil {
		klog.Errorf("Failed to get running jobs: %v", err)
		return nil
	}

	whiteList, err := mgr.getJobWhiteList(c)
	if err != nil {
		// 拿不到白名单，就不进行清理，以免“误伤”
		klog.Errorf("Failed to get job white list: %v", err)
		return nil
	}

	jobList := []*model.Job{}

	now := time.Now()
	for _, job := range runningJobs {
		// 过滤掉白名单中的作业
		if lo.Contains(whiteList, job.JobName) {
			continue
		}

		jobAge := now.Sub(job.RunningTimestamp)
		shouldCheck := false
		if job.JobType == model.JobTypeJupyter || job.JobType == model.JobTypeWebIDE {
			shouldCheck = jobAge > interactiveTimeout
		} else {
			shouldCheck = jobAge > batchTimeout
		}
		if !shouldCheck {
			continue
		}

		jobList = append(jobList, job)
	}
	return jobList
}

type CancelWaitingJupyterJobsRequest struct {
	WaitMinitues int `form:"waitMinitues" binding:"required"`
}

func (mgr *CronJobManager) CleanWaitingJupyterJobs(c context.Context, req *CancelWaitingJupyterJobsRequest) (map[string][]string, error) {
	if req == nil {
		err := errors.New("invalid request")
		return nil, err
	}
	deletedJobs := mgr.deleteUnscheduledJupyterJobs(c, req.WaitMinitues)
	ret := map[string][]string{
		"deleted": deletedJobs,
	}
	return ret, nil
}

func (mgr *CronJobManager) deleteUnscheduledJupyterJobs(c context.Context, waitMinitues int) []string {
	jobDB := query.Job
	jobs, err := jobDB.WithContext(c).Where(
		jobDB.Status.Eq(string(batch.Pending)),
		jobDB.JobType.Eq(string(model.JobTypeJupyter)),
		jobDB.CreationTimestamp.Lt(time.Now().Add(-time.Duration(waitMinitues)*time.Minute)),
	).Find()

	if err != nil {
		klog.Errorf("Failed to get unscheduled jupyter jobs: %v", err)
		return nil
	}

	deletedJobs := []string{}
	for _, job := range jobs {
		if mgr.isJobscheduled(c, job.JobName) {
			continue
		}

		// delete job
		vcjob := &batch.Job{}
		namespace := config.GetConfig().Namespaces.Job
		if err := mgr.Client.Get(c, client.ObjectKey{Name: job.JobName, Namespace: namespace}, vcjob); err != nil {
			klog.Errorf("Failed to get job %s: %v", job.JobName, err)
			continue
		}

		if err := mgr.Client.Delete(c, vcjob); err != nil {
			klog.Errorf("Failed to delete job %s: %v", job.JobName, err)
			continue
		}

		deletedJobs = append(deletedJobs, job.JobName)
	}

	return deletedJobs
}

// 如果VCJob还没有创建Pod，返回false
// 所有Pod被schedule，返回true；否则返回false
func (mgr *CronJobManager) isJobscheduled(c context.Context, jobName string) bool {
	namespace := config.GetConfig().Namespaces.Job
	pods, err := mgr.KubeClient.CoreV1().Pods(namespace).List(c, metav1.ListOptions{
		// 目前仅考虑vcjob
		LabelSelector: fmt.Sprintf("volcano.sh/job-name=%s", jobName),
	})
	if err != nil {
		klog.Errorf("Failed to get pods: %v", err)
		return false
	}

	if len(pods.Items) == 0 {
		// 没有Pod，说明VCJob还没有创建Pod
		return false
	}

	result := true
	for i := range pods.Items {
		pod := &pods.Items[i]
		// check pod's conditions: type==PodScheduled && status==True
		thisPodScheduled := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type != "PodScheduled" {
				continue
			}
			if condition.Status == "True" {
				thisPodScheduled = true
				break
			}
			if condition.Status == "False" {
				return false
			}
		}
		result = result && thisPodScheduled
	}
	return result
}
