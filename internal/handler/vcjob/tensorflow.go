package vcjob

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	bus "volcano.sh/apis/pkg/apis/bus/v1alpha1"

	"github.com/raids-lab/crater/internal/resputil"
	"github.com/raids-lab/crater/internal/util"
	"github.com/raids-lab/crater/pkg/aitaskctl"
	"github.com/raids-lab/crater/pkg/config"
	"github.com/raids-lab/crater/pkg/utils"
)

type (
	PortReq struct {
		Name string `json:"name"`
		Port int32  `json:"port"`
	}

	TaskReq struct {
		Name       string          `json:"name"`
		Replicas   int32           `json:"replicas"`
		Resource   v1.ResourceList `json:"resource"`
		Image      ImageBaseInfo   `json:"image"`
		Shell      *string         `json:"shell"`
		Command    *string         `json:"command"`
		WorkingDir *string         `json:"workingDir"`
		Ports      []PortReq       `json:"ports"`
	}

	CreateTensorflowReq struct {
		CreateJobCommon `json:",inline"`
		Tasks           []TaskReq `json:"tasks"`
	}
)

// CreateTrainingJob godoc
//
//	@Summary		Create a training job
//	@Description	Create a training job
//	@Tags			VolcanoJob
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			CreateTrainingReq	body		any						true	"CreateTrainingReq"
//	@Success		200					{object}	resputil.Response[any]	"Success"
//	@Failure		400					{object}	resputil.Response[any]	"Request parameter error"
//	@Failure		500					{object}	resputil.Response[any]	"Other errors"
//	@Router			/v1/vcjobs/tensorflow [post]
func (mgr *VolcanojobMgr) CreateTensorflowJob(c *gin.Context) {
	token := util.GetToken(c)

	var req CreateTensorflowReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resputil.BadRequestError(c, err.Error())
		return
	}

	jobResources := v1.ResourceList{}
	for i := range len(req.Tasks) {
		jobResources = aitaskctl.AddResourceList(jobResources, req.Tasks[i].Resource)
	}
	exceededResources := aitaskctl.CheckResourcesBeforeCreateJob(c, token.UserID, token.AccountID, jobResources)
	if len(exceededResources) > 0 {
		resputil.Error(c, fmt.Sprintf("%v", exceededResources), resputil.NotSpecified)
		return
	}

	// 如果希望接受邮件，则需要确保邮箱已验证
	if req.AlertEnabled && !utils.CheckUserEmail(c, token.UserID) {
		resputil.Error(c, "Email not verified", resputil.UserEmailNotVerified)
		return
	}

	// Ingress base URL
	baseURL := fmt.Sprintf("%s-%s", token.Username, uuid.New().String()[:6])
	jobName := fmt.Sprintf("tf-%s", baseURL)

	// 1. Volume Mounts
	volumes, volumeMounts, err := GenerateVolumeMounts(c, req.VolumeMounts, token)
	if err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}

	// 2. Node Affinity and Tolerations
	baseAffinity := GenerateNodeAffinity(req.Selectors, jobResources)
	baseTolerations := GenerateTaintTolerationsForAccount(token)
	envs := GenerateEnvs(c, token, req.Envs)

	// 3. Labels and Annotations
	labels, jobAnnotations, podAnnotations := getLabelAndAnnotations(
		CraterJobTypeTensorflow,
		token,
		baseURL,
		req.Name,
		req.Template,
		req.AlertEnabled,
	)

	// 4. Create the task spec
	tasks := make([]batch.TaskSpec, len(req.Tasks))
	minAvailable := int32(0)
	for i := range req.Tasks {
		task := &req.Tasks[i]

		// 4.1. Generate architecture-specific affinity and tolerations for this task
		taskAffinity := GenerateArchitectureNodeAffinity(task.Image, baseAffinity)
		taskTolerations := GenerateArchitectureTolerations(task.Image, baseTolerations)

		// 4.2. Generate ports
		ports := make([]v1.ContainerPort, len(task.Ports))
		for j, port := range task.Ports {
			ports[j] = v1.ContainerPort{
				ContainerPort: port.Port,
				Name:          port.Name,
				Protocol:      v1.ProtocolTCP,
			}
		}
		// 4.3. Generate pod spec
		podSpec := generatePodSpecForParallelJob(task, taskAffinity, taskTolerations, volumes, volumeMounts, envs, ports)

		// 4.4. Create task spec
		taskSpec := batch.TaskSpec{
			Name:     task.Name,
			Replicas: task.Replicas,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		}

		if task.Name == "worker" {
			taskSpec.Policies = []batch.LifecyclePolicy{
				{
					Action: bus.CompleteJobAction,
					Event:  bus.TaskCompletedEvent,
				},
			}
		}

		minAvailable += task.Replicas
		tasks[i] = taskSpec
	}

	// 5. Create volcano job
	job := batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
			Namespace:   config.GetConfig().Workspace.Namespace,
			Labels:      labels,
			Annotations: jobAnnotations,
		},
		Spec: batch.JobSpec{
			TTLSecondsAfterFinished: ptr.To(ThreeDaySeconds),
			MinAvailable:            minAvailable,
			SchedulerName:           VolcanoSchedulerName,
			Plugins: map[string][]string{
				"env": {},
				"svc": {},
			},
			Policies: []batch.LifecyclePolicy{
				{
					Action: bus.RestartJobAction,
					Event:  bus.PodEvictedEvent,
				},
			},
			Queue: token.AccountName,
			Tasks: tasks,
		},
	}

	if err = mgr.client.Create(c, &job); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}

	resputil.Success(c, job)
}
