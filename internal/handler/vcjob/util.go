package vcjob

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"github.com/gin-gonic/gin"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/util"
	"github.com/raids-lab/crater/pkg/config"
	"github.com/raids-lab/crater/pkg/crclient"
)

type VolumeType uint

const (
	_ VolumeType = iota
	FileType
	DataType
)

type ForwardType uint

const (
	_ ForwardType = iota
	IngressType
	NodePortType
)

type CraterJobType string

const (
	CraterJobTypeTensorflow CraterJobType = "tensorflow"
	CraterJobTypePytorch    CraterJobType = "pytorch"
	CraterJobTypeJupyter    CraterJobType = "jupyter"
	CraterJobTypeCustom     CraterJobType = "custom"
)

func GenerateVolumeMounts(
	c context.Context,
	volumes []VolumeMount,
	token util.JWTMessage, // 传入 token 信息
) (pvc []v1.Volume, volumeMounts []v1.VolumeMount, err error) {
	// 初始化返回的 PVC 和 VolumeMount 列表
	volumeMounts = []v1.VolumeMount{
		{
			Name:      VolumeCache,
			MountPath: "/dev/shm",
		},
	}
	pvc = []v1.Volume{
		{
			Name: VolumeCache,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{
					Medium: v1.StorageMediumMemory,
				},
			},
		},
	}

	// 遍历 volumes，根据权限动态创建 PVC
	pvcMap := make(map[string]bool) // 用于避免重复创建同一 PVC
	for _, vm := range volumes {
		volumeMount, err := resolveVolumeMount(c, token, vm)
		if err != nil {
			return nil, nil, err
		}

		// 如果该 PVC 尚未创建，添加到 PVC 列表
		if !pvcMap[volumeMount.Name] {
			pvc = append(pvc, createVolume(volumeMount.Name))
			pvcMap[volumeMount.Name] = true
		}

		// 添加挂载点
		volumeMounts = append(volumeMounts, volumeMount)
	}

	// 挂载启动脚本/usr/local/bin/start.sh
	pvc = append(pvc, v1.Volume{
		Name: "start-bash-script-volume",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "custom-start-configmap",
				},
				//nolint:mnd // 0755 is the default mode
				DefaultMode: ptr.To(int32(0755)),
			},
		},
	})
	volumeMounts = append(volumeMounts, v1.VolumeMount{
		Name:      "start-bash-script-volume",
		MountPath: "/crater-start.sh",
		ReadOnly:  true,
		SubPath:   "start.sh",
	})

	return pvc, volumeMounts, nil
}

// resolveVolumeMount resolves the subpath and volume type based on the mount configuration
func resolveVolumeMount(c context.Context, token util.JWTMessage, vm VolumeMount) (
	mount v1.VolumeMount, err error,
) {
	// Get PVC names from config
	rwxPVCName := config.GetConfig().Storage.RWXPVCName
	roxPVCName := config.GetConfig().Storage.ROXPVCName

	// Handle dataset type volumes - always read-only
	if vm.Type == DataType {
		// Get dataset path from database
		datasetPath, editable, err := GetSubPathByDatasetVolume(c, token.UserID, vm.DatasetID)
		if err != nil {
			return v1.VolumeMount{}, err
		}
		// If editable is true, use RWX PVC
		if editable {
			return v1.VolumeMount{
				Name:      rwxPVCName,
				SubPath:   datasetPath,
				MountPath: vm.MountPath,
				ReadOnly:  false,
			}, nil
		}
		// If editable is false, use ROX PVC
		return v1.VolumeMount{
			Name:      roxPVCName,
			SubPath:   datasetPath,
			MountPath: vm.MountPath,
			ReadOnly:  true,
		}, nil
	}

	// Handle file type volumes based on path prefix
	switch {
	case strings.HasPrefix(vm.SubPath, "public"):
		// Public space paths - permission based on PublicAccessMode
		subPath := filepath.Clean(config.GetConfig().Storage.Prefix.Public + strings.TrimPrefix(vm.SubPath, "public"))
		if isReadOnly(token.PublicAccessMode) {
			// Read-only access
			return v1.VolumeMount{
				Name:      roxPVCName,
				SubPath:   subPath,
				MountPath: vm.MountPath,
				ReadOnly:  true,
			}, nil
		}
		return v1.VolumeMount{
			Name:      rwxPVCName,
			SubPath:   subPath,
			MountPath: vm.MountPath,
			ReadOnly:  false,
		}, nil
	case strings.HasPrefix(vm.SubPath, "account"):
		// Account space paths - permission based on AccountAccessMode
		a := query.Account
		account, err := a.WithContext(c).Where(a.ID.Eq(token.AccountID)).First()
		if err != nil {
			return v1.VolumeMount{}, err
		}
		subPath := filepath.Clean(config.GetConfig().Storage.Prefix.Account + "/" + account.Space + strings.TrimPrefix(vm.SubPath, "account"))
		if isReadOnly(token.AccountAccessMode) {
			// Read-only access
			return v1.VolumeMount{
				Name:      roxPVCName,
				SubPath:   subPath,
				MountPath: vm.MountPath,
				ReadOnly:  true,
			}, nil
		}
		// Read-write access
		return v1.VolumeMount{
			Name:      rwxPVCName,
			SubPath:   subPath,
			MountPath: vm.MountPath,
			ReadOnly:  false,
		}, nil
	case strings.HasPrefix(vm.SubPath, "user"):
		// User space paths - always read-write
		u := query.User
		user, err := u.WithContext(c).Where(u.ID.Eq(token.UserID)).First()
		if err != nil {
			return v1.VolumeMount{}, err
		}
		subPath := filepath.Clean(config.GetConfig().Storage.Prefix.User + "/" + user.Space + strings.TrimPrefix(vm.SubPath, "user"))
		// User's own space always gets read-write access
		return v1.VolumeMount{
			Name:      rwxPVCName,
			SubPath:   subPath,
			MountPath: vm.MountPath,
			ReadOnly:  false,
		}, nil
	default:
		return v1.VolumeMount{}, fmt.Errorf("invalid mount path format: %s", vm.SubPath)
	}
}

// isReadOnly determines the appropriate PVC and read-only flag based on the access mode
func isReadOnly(accessMode model.AccessMode) bool {
	switch accessMode {
	case model.AccessModeRO, model.AccessModeAO:
		// Read-only access
		return true
	case model.AccessModeRW:
		// Read-write access
		return false
	default:
		// Invalid access mode
		return true
	}
}

// 创建 PVC Volume
func createVolume(volumeName string) v1.Volume {
	return v1.Volume{
		Name: volumeName,
		VolumeSource: v1.VolumeSource{
			PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: volumeName,
			},
		},
	}
}

func GetSubPathByDatasetVolume(c context.Context,
	userID, datasetID uint) (subPath string, editable bool, err error) {
	ud := query.UserDataset
	d := query.Dataset
	ad := query.AccountDataset
	ua := query.UserAccount
	dataset, err := d.WithContext(c).Where(d.ID.Eq(datasetID)).First()
	if err != nil {
		return "", false, err
	}
	editable = dataset.Extra.Data().Editable
	// Find()方法没找到不会报err，而是返回nil
	accountDatasets, err := ad.WithContext(c).Where(ad.DatasetID.Eq(datasetID)).Find()
	if err != nil {
		return "", false, err
	}
	for _, accountDataset := range accountDatasets {
		_, err = ua.WithContext(c).Where(ua.AccountID.Eq(accountDataset.AccountID), ua.UserID.Eq(userID)).First()
		if err == nil {
			return dataset.URL, editable, nil
		}
	}
	_, err = ud.WithContext(c).Where(ud.UserID.Eq(userID), ud.DatasetID.Eq(datasetID)).First()
	if err != nil {
		return "", false, err
	}

	return dataset.URL, editable, nil
}

func GenerateTaintTolerationsForAccount(token util.JWTMessage) (tolerations []v1.Toleration) {
	// If current account is not default account (which account id is 1), add toleration
	if token.AccountID == model.DefaultAccountID {
		return nil
	}
	return []v1.Toleration{
		{
			Key:      "crater.raids.io/account",
			Operator: v1.TolerationOpEqual,
			Value:    token.AccountName,
			Effect:   v1.TaintEffectNoSchedule,
		},
	}
}

// GenerateEnvs generates environment variables for the pod
// NB_USER: username
// NB_GID: group ID
// NB_UID: user ID
func GenerateEnvs(ctx context.Context, token util.JWTMessage, customEnvs []v1.EnvVar) []v1.EnvVar {
	u := query.User
	user, err := u.WithContext(ctx).Where(u.ID.Eq(token.UserID)).First()
	if err != nil {
		return customEnvs
	}
	data := user.Attributes.Data()
	if data.UID == nil {
		data.UID = ptr.To("1001")
	}
	if data.GID == nil {
		data.GID = ptr.To("1001")
	}
	// Add user and group ID to environment variables
	customEnvs = append(customEnvs,
		v1.EnvVar{
			Name:  "NB_USER",
			Value: token.Username,
		},
		v1.EnvVar{
			Name:  "NB_GID",
			Value: *data.GID,
		},
		v1.EnvVar{
			Name:  "NB_UID",
			Value: *data.UID,
		},
	)
	return customEnvs
}

func GenerateNodeAffinity(expressions []v1.NodeSelectorRequirement, totalRequests v1.ResourceList) (affinity *v1.Affinity) {
	// expressions will override prefer
	if len(expressions) > 0 {
		return ptr.To(v1.Affinity{
			NodeAffinity: ptr.To(v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: ptr.To(v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: expressions,
						},
					},
				}),
			}),
		})
	}

	// if no resources, return nil
	if totalRequests == nil {
		return affinity
	}

	// if no expressions, use prefer
	gpuCount := GetGPUCountFromResource(totalRequests)
	switch gpuCount {
	case 0:
		return ptr.To(v1.Affinity{
			NodeAffinity: ptr.To(v1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
					{
						Weight: 40,
						Preference: v1.NodeSelectorTerm{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      "nvidia.com/gpu.present",
									Operator: v1.NodeSelectorOpDoesNotExist,
								},
								{
									// InfiniBand Node Feature Discovery Label
									Key:      "feature.node.kubernetes.io/pci-15b3.present",
									Operator: v1.NodeSelectorOpDoesNotExist,
								},
							},
						},
					},
				},
			}),
		})
	case 1:
		return ptr.To(v1.Affinity{
			NodeAffinity: ptr.To(v1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
					{
						Weight: 50,
						Preference: v1.NodeSelectorTerm{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									// InfiniBand Node Feature Discovery Label
									Key:      "feature.node.kubernetes.io/pci-15b3.present",
									Operator: v1.NodeSelectorOpDoesNotExist,
								},
							},
						},
					},
				},
			}),
		})
	}
	return affinity
}

func GetGPUCountFromResource(resources v1.ResourceList) (gpuCount int64) {
	// with prefix nvidia.com
	for k, v := range resources {
		if strings.HasPrefix(k.String(), "nvidia.com") {
			return v.Value()
		}
	}
	return 0
}

func generatePodSpecForParallelJob(
	task *TaskReq,
	affinity *v1.Affinity,
	tolerations []v1.Toleration,
	volumes []v1.Volume,
	volumeMounts []v1.VolumeMount,
	envs []v1.EnvVar,
	ports []v1.ContainerPort,
) (podSpec v1.PodSpec) {
	imagePullSecrets := []v1.LocalObjectReference{}
	if config.GetConfig().Secrets.ImagePullSecretName != "" {
		imagePullSecrets = append(imagePullSecrets, v1.LocalObjectReference{
			Name: config.GetConfig().Secrets.ImagePullSecretName,
		})
	}

	podSpec = v1.PodSpec{
		Affinity:         affinity,
		Tolerations:      tolerations,
		Volumes:          volumes,
		ImagePullSecrets: imagePullSecrets,
		Containers: []v1.Container{
			{
				Name:  task.Name,
				Image: task.Image,
				Resources: v1.ResourceRequirements{
					Limits:   task.Resource,
					Requests: task.Resource,
				},
				Env:   envs,
				Ports: ports,
				SecurityContext: &v1.SecurityContext{
					RunAsUser:  ptr.To(int64(0)),
					RunAsGroup: ptr.To(int64(0)),
					Capabilities: &v1.Capabilities{
						Add: []v1.Capability{"IPC_LOCK"},
					},
				},
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: v1.TerminationMessageReadFile,
				VolumeMounts:             volumeMounts,
			},
		},
		RestartPolicy:      v1.RestartPolicyNever,
		EnableServiceLinks: ptr.To(false),
	}
	if task.Command != nil && *task.Command != "" {
		if task.Shell == nil {
			task.Shell = ptr.To("sh")
		}
		podSpec.Containers[0].Command = []string{*task.Shell, "-c", *task.Command}
	}
	if task.WorkingDir != nil {
		podSpec.Containers[0].WorkingDir = *task.WorkingDir
	}
	return podSpec
}

func marshalYAMLWithIndent(v any, indent int) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(indent)
	if err := encoder.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func getJob(c context.Context, name string, token *util.JWTMessage) (*model.Job, error) {
	j := query.Job
	q := j.WithContext(c).
		Preload(j.Account).
		Preload(j.User).
		Where(j.JobName.Eq(name))

	if token.RolePlatform == model.RoleAdmin {
		return q.First()
	} else {
		return q.
			Where(j.AccountID.Eq(token.AccountID)).
			Where(j.UserID.Eq(token.UserID)).
			First()
	}
}

func getPodNameAndLabelFromJob(vcjob *batch.Job) (podName string, podLabels map[string]string) {
	for i := range vcjob.Spec.Tasks {
		task := &vcjob.Spec.Tasks[i]
		return fmt.Sprintf("%s-%s-0", vcjob.Name, task.Name), task.Template.Labels
	}
	return "", nil
}

// execCommandInPod 在Pod中执行命令并返回输出结果
func (mgr *VolcanojobMgr) execCommandInPod(
	ctx *gin.Context, namespace, podName, containerName string, command []string,
) (string, error) {
	req := mgr.kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	option := &v1.PodExecOptions{
		Command:   command,
		Container: containerName,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}

	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)

	exec, err := remotecommand.NewSPDYExecutor(mgr.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		return "", fmt.Errorf("failed to execute command: %v, stderr: %s, error: %w", command, stderr.String(), err)
	}

	return stdout.String(), nil
}

func getLabelAndAnnotations(jobType CraterJobType, token util.JWTMessage, baseURL, taskName, template string, alertEnabled bool) (
	labels map[string]string,
	jobAnnotations map[string]string,
	podAnnotations map[string]string,
) {
	labels = map[string]string{
		crclient.LabelKeyTaskType: string(jobType),
		crclient.LabelKeyTaskUser: token.Username,
		crclient.LabelKeyBaseURL:  baseURL,
	}
	jobAnnotations = map[string]string{
		AnnotationKeyTaskName:     taskName,
		AnnotationKeyTaskTemplate: template,
		AnnotationKeyAlertEnabled: strconv.FormatBool(alertEnabled),
	}
	podAnnotations = map[string]string{
		AnnotationKeyTaskName: taskName,
		AnnotationKeyUser:     token.Username,
	}
	return labels, jobAnnotations, podAnnotations
}
