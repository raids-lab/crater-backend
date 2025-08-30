//nolint:lll // Promtheus Query is too long
package monitor

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/pkg/config"
)

func (p *PrometheusClient) QueryPodProfileMetric(namespace, podname string) (PodUtil, error) {
	podUtil := PodUtil{}
	var err error
	podUtil.CPUMemMax, err = p.queryMetric(fmt.Sprintf("max_over_time(container_memory_usage_bytes{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[60s])/1048576", namespace, podname))
	if err != nil {
		return podUtil, err
	}
	podUtil.GPUUtilAvg, err = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_GPU_UTIL{namespace=%q,pod=%q}[60s])/100", namespace, podname))
	podUtil.GPUUtilMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_DEV_GPU_UTIL{namespace=%q,pod=%q}[60s])/100", namespace, podname))
	podUtil.GPUUtilStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_GPU_UTIL{namespace=%q,pod=%q}[60s])/100", namespace, podname))
	podUtil.SMActiveAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_SM_ACTIVE{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.SMActiveMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_SM_ACTIVE{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.SMActiveStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_SM_ACTIVE{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.SMOccupancyAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_SM_OCCUPANCY{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.SMOccupancyMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_SM_OCCUPANCY{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.SMOccupancyStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_SM_OCCUPANCY{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.DramUtilAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.DramUtilMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.DramUtilStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.MemCopyUtilAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{namespace=%q,pod=%q}[60s])/100", namespace, podname))
	podUtil.MemCopyUtilMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{namespace=%q,pod=%q}[60s])/100", namespace, podname))
	podUtil.MemCopyUtilStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{namespace=%q,pod=%q}[60s])/100", namespace, podname))
	podUtil.PCIETxAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PCIE_TX_BYTES{namespace=%q,pod=%q}[60s])/1048576", namespace, podname))
	podUtil.PCIETxMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PCIE_TX_BYTES{namespace=%q,pod=%q}[60s])/1048576", namespace, podname))
	podUtil.PCIERxAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PCIE_RX_BYTES{namespace=%q,pod=%q}[60s])/1048576", namespace, podname))
	podUtil.PCIERxMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PCIE_RX_BYTES{namespace=%q,pod=%q}[60s])/1048576", namespace, podname))
	podUtil.CPUUsageAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(irate(container_cpu_usage_seconds_total{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[30s])[2m:])", namespace, podname))
	podUtil.GPUMemMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_DEV_FB_USED{namespace=%q,pod=%q}[60s])", namespace, podname))
	podUtil.CPUMemMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(container_memory_usage_bytes{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[60s])/1048576", namespace, podname))

	if err != nil || podUtil.GPUUtilAvg == 0 {
		// TODO(liyilong): 为了中期兼容，先使用默认值
		nodeName := "cn-beijing.10.168.205.227"
		podUtil.GPUUtilAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_GPU_UTIL{node=%q}[60s])/100", nodeName))
		podUtil.GPUUtilMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_DEV_GPU_UTIL{node=%q}[60s])/100", nodeName))
		podUtil.GPUUtilStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_GPU_UTIL{node=%q}[60s])/100", nodeName))
		podUtil.SMActiveAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_SM_ACTIVE{node=%q}[60s])", nodeName))
		podUtil.SMActiveMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_SM_ACTIVE{node=%q}[60s])", nodeName))
		podUtil.SMActiveStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_SM_ACTIVE{node=%q}[60s])", nodeName))
		podUtil.SMOccupancyAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_SM_OCCUPANCY{node=%q}[60s])", nodeName))
		podUtil.SMOccupancyMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_SM_OCCUPANCY{node=%q}[60s])", nodeName))
		podUtil.SMOccupancyStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_SM_OCCUPANCY{node=%q}[60s])", nodeName))
		podUtil.DramUtilAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_DRAM_ACTIVE{node=%q}[60s])", nodeName))
		podUtil.DramUtilMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_DRAM_ACTIVE{node=%q}[60s])", nodeName))
		podUtil.DramUtilStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_DRAM_ACTIVE{node=%q}[60s])", nodeName))
		podUtil.MemCopyUtilAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{node=%q}[60s])/100", nodeName))
		podUtil.MemCopyUtilMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{node=%q}[60s])/100", nodeName))
		podUtil.MemCopyUtilStd, _ = p.queryMetric(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{node=%q}[60s])/100", nodeName))
		podUtil.PCIETxAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PCIE_TX_BYTES{node=%q}[60s])/1048576", nodeName))
		podUtil.PCIETxMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PCIE_TX_BYTES{node=%q}[60s])/1048576", nodeName))
		podUtil.PCIERxAvg, _ = p.queryMetric(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PCIE_RX_BYTES{node=%q}[60s])/1048576", nodeName))
		podUtil.PCIERxMax, _ = p.queryMetric(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PCIE_RX_BYTES{node=%q}[60s])/1048576", nodeName))
	}

	return podUtil, nil
}

func (p *PrometheusClient) QueryProfileData(namespacedName types.NamespacedName, from time.Time) *ProfileData {
	profileData := ProfileData{}
	now := time.Now()
	duration := now.Sub(from)

	// 将 duration 转换为秒数，因为这是最通用的单位，然后计算出合适的单位。
	seconds := int64(duration.Seconds())
	var promRange string

	switch {
	//nolint:mnd // 60 second is 1 minute.
	case seconds < 60:
		promRange = fmt.Sprintf("%ds", seconds)
	default:
		minutes := seconds / 60
		promRange = fmt.Sprintf("%dm", minutes)
	}

	allNull := true
	// TODO(liyilong): 支持多 GPU
	queryMetricWithPtr := func(query string) *float32 {
		value, err := p.queryMetric(query)
		if err != nil {
			return nil
		}
		allNull = false
		return &value
	}

	namespace := namespacedName.Namespace
	podname := namespacedName.Name

	// 查询 Pod 申请的 CPU 和内存（计算每个容器之和，并使用 max_over_time 在 Pod 运行期间的最大值）
	profileData.CPURequest = queryMetricWithPtr(fmt.Sprintf("max_over_time(kube_pod_container_resource_requests{namespace=%q,pod=%q,resource=\"cpu\"}[%s])", namespace, podname, promRange))
	profileData.CPULimit = queryMetricWithPtr(fmt.Sprintf("max_over_time(kube_pod_container_resource_limits{namespace=%q,pod=%q,resource=\"cpu\"}[%s])", namespace, podname, promRange))
	profileData.MemRequest = queryMetricWithPtr(fmt.Sprintf("max_over_time(kube_pod_container_resource_requests{namespace=%q,pod=%q,resource=\"memory\"}[%s])/1048576", namespace, podname, promRange))
	profileData.MemLimit = queryMetricWithPtr(fmt.Sprintf("max_over_time(kube_pod_container_resource_limits{namespace=%q,pod=%q,resource=\"memory\"}[%s])/1048576", namespace, podname, promRange))

	// CPU 相关指标 - 如果运行时间超过1小时，则只使用最近1小时数据
	cpuUsageRange := promRange
	if duration > time.Hour {
		cpuUsageRange = "1h"
	}
	profileData.CPUUsageAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(irate(container_cpu_usage_seconds_total{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[30s])[%s:])", namespace, podname, cpuUsageRange))
	profileData.CPUUsageMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(irate(container_cpu_usage_seconds_total{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[30s])[%s:])", namespace, podname, cpuUsageRange))
	profileData.CPUUsageStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(irate(container_cpu_usage_seconds_total{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[30s])[%s:])", namespace, podname, cpuUsageRange))

	profileData.CPUMemMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(container_memory_usage_bytes{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[%s])/1048576", namespace, podname, promRange))
	profileData.CPUMemAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(container_memory_usage_bytes{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[%s])/1048576", namespace, podname, promRange))
	profileData.CPUMemStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(container_memory_usage_bytes{namespace=%q,pod=%q,container!=\"POD\",container!=\"\"}[%s])/1048576", namespace, podname, promRange))

	// 如果 Pod 没有申请 GPU，则直接返回 CPU 相关指标
	gpuRequested := p.checkIfGPURequested(namespacedName)
	if !gpuRequested {
		if allNull {
			return nil
		}
		return &profileData
	}

	// GPU 相关指标
	profileData.PCIETxAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PCIE_TX_BYTES{namespace=%q,pod=%q}[%s])/1048576", namespace, podname, promRange))
	profileData.PCIETxMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PCIE_TX_BYTES{namespace=%q,pod=%q}[%s])/1048576", namespace, podname, promRange))
	profileData.PCIERxAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PCIE_RX_BYTES{namespace=%q,pod=%q}[%s])/1048576", namespace, podname, promRange))
	profileData.PCIERxMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PCIE_RX_BYTES{namespace=%q,pod=%q}[%s])/1048576", namespace, podname, promRange))

	profileData.GPUUtilAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_GPU_UTIL{namespace=%q,pod=%q}[%s])/100", namespace, podname, promRange))
	profileData.GPUUtilMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_DEV_GPU_UTIL{namespace=%q,pod=%q}[%s])/100", namespace, podname, promRange))
	profileData.GPUUtilStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_GPU_UTIL{namespace=%q,pod=%q}[%s])/100", namespace, podname, promRange))

	profileData.SMActiveAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_SM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.SMActiveMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_SM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.SMActiveStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_SM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.SMOccupancyAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_SM_OCCUPANCY{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.SMOccupancyMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_SM_OCCUPANCY{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.SMOccupancyStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_SM_OCCUPANCY{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.DramUtilAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.DramUtilMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.DramUtilStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.MemCopyUtilAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{namespace=%q,pod=%q}[%s])/100", namespace, podname, promRange))
	profileData.MemCopyUtilMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{namespace=%q,pod=%q}[%s])/100", namespace, podname, promRange))
	profileData.MemCopyUtilStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_MEM_COPY_UTIL{namespace=%q,pod=%q}[%s])/100", namespace, podname, promRange))

	// 查询 GPU 总显存指标
	profileData.GPUMemTotal = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_DEV_FB_USED{namespace=%q,pod=%q}[%s])+DCGM_FI_DEV_FB_FREE", namespace, podname, promRange))

	profileData.GPUMemMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_DEV_FB_USED{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.GPUMemAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_DEV_FB_USED{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.GPUMemStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_DEV_FB_USED{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.TensorActiveAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PIPE_TENSOR_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.TensorActiveMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PIPE_TENSOR_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.TensorActiveStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_PIPE_TENSOR_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.Fp64ActiveAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PIPE_FP64_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.Fp64ActiveMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PIPE_FP64_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.Fp64ActiveStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_PIPE_FP64_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.Fp32ActiveAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PIPE_FP32_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.Fp32ActiveMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PIPE_FP32_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.Fp32ActiveStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_PIPE_FP32_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.DramActiveAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.DramActiveMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.DramActiveStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_DRAM_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	profileData.Fp16ActiveAvg = queryMetricWithPtr(fmt.Sprintf("avg_over_time(DCGM_FI_PROF_PIPE_FP16_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.Fp16ActiveMax = queryMetricWithPtr(fmt.Sprintf("max_over_time(DCGM_FI_PROF_PIPE_FP16_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))
	profileData.Fp16ActiveStd = queryMetricWithPtr(fmt.Sprintf("stddev_over_time(DCGM_FI_PROF_PIPE_FP16_ACTIVE{namespace=%q,pod=%q}[%s])", namespace, podname, promRange))

	if allNull {
		return nil
	}
	return &profileData
}

func (p *PrometheusClient) QueryNodeCPUUsageRatio() map[string]float32 {
	query := `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`
	data, err := p.float32MapQuery(query, "instance")
	if err != nil {
		klog.Errorf("QueryNodeCPUUsageRatio error: %v", err)
		return nil
	}
	return data
}

func (p *PrometheusClient) QueryNodeMemoryUsageRatio() map[string]float32 {
	query := "(1 - (avg by (instance) (node_memory_MemAvailable_bytes) / avg by (instance) (node_memory_MemTotal_bytes))) * 100"
	data, err := p.float32MapQuery(query, "instance")
	if err != nil {
		klog.Errorf("QueryNodeMemoryUsageRatio error: %v", err)
		return nil
	}
	return data
}

// QueryNodeAllocatedMemory returns the allocated memory of each node
func (p *PrometheusClient) QueryNodeAllocatedMemory() map[string]int {
	query := `sum by (node) (
		kube_pod_container_resource_requests{resource="memory"} * 
		on(pod, namespace) group_left() 
		(kube_pod_status_phase{phase="Running"})
	  )`
	data, err := p.intMapQuery(query, "node")
	if err != nil {
		klog.Errorf("QueryNodeAllocatedMemory error: %v", err)
		return nil
	}
	return data
}

// QueryNodeAllocatedCPU returns the allocated CPU of each node
func (p *PrometheusClient) QueryNodeAllocatedCPU() map[string]float32 {
	query := `sum by (node) (
		kube_pod_container_resource_requests{resource="cpu"} * 
		on(pod, namespace) group_left() 
		(kube_pod_status_phase{phase="Running"})
	  )`
	data, err := p.float32MapQuery(query, "node")
	if err != nil {
		klog.Errorf("QueryNodeAllocatedCPU error: %v", err)
		return nil
	}
	return data
}

// QueryNodeAllocatedGPU returns the allocated GPU of each node
func (p *PrometheusClient) QueryNodeAllocatedGPU() map[string]int {
	query := `sum by (node) (
		kube_pod_container_resource_requests{resource=~"nvidia_.*"} * 
		on(pod, namespace) group_left() 
		(kube_pod_status_phase{phase="Running"})
	  )`
	data, err := p.intMapQuery(query, "node")
	if err != nil {
		klog.Errorf("QueryNodeAllocatedGPU error: %v", err)
		return nil
	}
	return data
}

// QueryNodeRunningPodCount returns the running pod of each node
func (p *PrometheusClient) QueryNodeRunningPodCount() map[string]int {
	query := fmt.Sprintf(`count by (node) (
	    kube_pod_status_phase{phase="Running"} * 
		on(pod) group_left(node, created_by_kind) kube_pod_info{namespace=%q, created_by_kind=~".*Job.*"} == 1
	)`, config.GetConfig().Namespaces.Job)
	data, err := p.intMapQuery(query, "node")
	if err != nil {
		klog.Errorf("QueryNodeRunningPodCount error: %v", err)
		return nil
	}
	return data
}

func (p *PrometheusClient) QueryPodCPUUsage(podName string) float32 {
	query := fmt.Sprintf("sum(rate(container_cpu_usage_seconds_total{pod=%q}[5m]))", podName)
	data, err := p.float32MapQuery(query, "")
	if err != nil {
		klog.Errorf("QueryPodCPURatio error: %v", err)
		return 0.0
	}
	var sum float32
	for _, v := range data {
		sum += v
	}
	return sum
}

func (p *PrometheusClient) QueryPodCPUAllocate(podName, namespace string) int {
	query := fmt.Sprintf("kube_pod_container_resource_requests{pod=%q, namespace=%q, resource=\"cpu\"}", podName, namespace)
	data, err := p.intMapQuery(query, "")
	if err != nil {
		klog.Errorf("QueryPodCPUAllocate error: %v", err)
		return -1
	}
	var sum int
	for _, v := range data {
		sum += v
	}
	return sum
}

func (p *PrometheusClient) QueryPodMemoryUsage(podName string) int {
	query := fmt.Sprintf("container_memory_usage_bytes{pod=%q}", podName)
	data, err := p.intMapQuery(query, "")
	if err != nil {
		klog.Errorf("QueryPodMemory error: %v", err)
		return 0
	}
	var sum int
	for _, v := range data {
		sum += v
	}
	return sum
}

func (p *PrometheusClient) QueryPodMemoryAllocate(podName, namespace string) int {
	query := fmt.Sprintf("kube_pod_container_resource_requests{pod=%q, namespace=%q, resource=\"memory\"}", podName, namespace)
	data, err := p.intMapQuery(query, "")
	if err != nil {
		klog.Errorf("QueryPodCPUAllocate error: %v", err)
		return -1
	}
	var sum int
	for _, v := range data {
		sum += v
	}
	return sum
}

func (p *PrometheusClient) QueryPodGPUAllocate(podName, namespace string) map[string]int {
	query := fmt.Sprintf("kube_pod_container_resource_requests{pod=%q, namespace=%q, resource!=\"cpu\", resource!=\"memory\"}", podName, namespace)
	data, err := p.intMapQuery(query, "resource")
	if err != nil {
		klog.Errorf("QueryPodCPUAllocate error: %v", err)
		return nil
	}
	return data
}

func (p *PrometheusClient) QueryNodeGPUUtil() []NodeGPUUtil {
	expression := "DCGM_FI_DEV_GPU_UTIL"
	data, err := p.getNodeGPUUtil(expression)
	if err != nil {
		klog.Errorf("QueryNodeGPUUtil error: %v", err)
		return nil
	}
	return data
}

func (p *PrometheusClient) QueryNodeGPUUtilInNS(namespace string) []NodeGPUUtil {
	expression := fmt.Sprintf("DCGM_FI_DEV_GPU_UTIL{namespace=%q}", namespace)
	data, err := p.getNodeGPUUtil(expression)
	if err != nil {
		klog.Errorf("QueryNodeGPUUtil error: %v", err)
		return nil
	}
	return data
}

func (p *PrometheusClient) GetJobPodsList() map[string][]string {
	query := fmt.Sprintf(`kube_pod_info{namespace=%q,created_by_kind="Job"}`, config.GetConfig().Namespaces.Job)
	data, err := p.getJobPods(query)
	if err != nil {
		klog.Errorf("GetJobPodsList error: %v", err)
		return nil
	}
	return data
}

func (p *PrometheusClient) GetPodOwner(podName string) string {
	query := fmt.Sprintf("kube_pod_owner{pod=%q}", podName)
	data, err := p.intMapQuery(query, "owner_name")
	if err != nil {
		klog.Errorf("GetJobPodsList error: %v", err)
		return ""
	}
	if len(data) == 0 {
		return ""
	}
	for key := range data {
		return key
	}
	return ""
}

func (p *PrometheusClient) GetLeastUsedGPUJobList(podName, _time, util string) int {
	query := fmt.Sprintf("max_over_time(DCGM_FI_DEV_GPU_UTIL{pod=%q}[%vm]) <= %v and DCGM_FI_DEV_GPU_UTIL{pod=%q} offset %vm", podName, _time, util, podName, _time)
	data, err := p.checkGPUUsed(query)
	if err != nil {
		klog.Errorf("GetLeastUsedGPUJobList error: %v", err)
		return 0
	}
	return data
}
