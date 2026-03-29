// Package metrics - 容器级监控
// 通过 Docker API 采集每个容器的 CPU、内存、网络、TPS 指标
package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// 容器级 Prometheus 指标
var (
	// CPU 使用率（百分比）
	ContainerCPUUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "cpu_usage_percent",
		Help:      "Container CPU usage percentage",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 内存使用量（字节）
	ContainerMemoryUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "memory_usage_bytes",
		Help:      "Container memory usage in bytes",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 内存限制（字节）
	ContainerMemoryLimit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "memory_limit_bytes",
		Help:      "Container memory limit in bytes",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 内存使用率（百分比）
	ContainerMemoryPercent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "memory_usage_percent",
		Help:      "Container memory usage percentage",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 网络接收字节数
	ContainerNetworkRx = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "network_rx_bytes_total",
		Help:      "Container network bytes received",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 网络发送字节数
	ContainerNetworkTx = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "network_tx_bytes_total",
		Help:      "Container network bytes transmitted",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 容器运行状态（1=running, 0=stopped）
	ContainerStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "status",
		Help:      "Container status (1=running, 0=stopped)",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// 容器运行时间（秒）
	ContainerUptime = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "uptime_seconds",
		Help:      "Container uptime in seconds",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})

	// PIDs（进程数）
	ContainerPIDs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aicorp",
		Subsystem: "container",
		Name:      "pids",
		Help:      "Number of PIDs in container",
	}, []string{"container_id", "container_name", "task_id", "agent_id"})
)

// ContainerMetrics 容器指标数据
type ContainerMetrics struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	TaskID        string    `json:"task_id"`   // 关联的任务ID
	AgentID       string    `json:"agent_id"`  // 关联的AgentID
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryUsage   int64     `json:"memory_usage"`   // bytes
	MemoryLimit   int64     `json:"memory_limit"`   // bytes
	MemoryPercent float64   `json:"memory_percent"`
	NetworkRx     int64     `json:"network_rx"`     // bytes
		NetworkTx     int64     `json:"network_tx"`     // bytes
	PIDs          int       `json:"pids"`
	Status        string    `json:"status"`
	Uptime        int64     `json:"uptime"`         // seconds
	Timestamp     time.Time `json:"timestamp"`
}

// ContainerMonitor 容器监控器
type ContainerMonitor struct {
	metrics     map[string]*ContainerMetrics // container_id -> metrics
	mu          sync.RWMutex
	stopCh      chan struct{}
	interval    time.Duration
}

// NewContainerMonitor 创建容器监控器
func NewContainerMonitor(interval time.Duration) *ContainerMonitor {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &ContainerMonitor{
		metrics:  make(map[string]*ContainerMetrics),
		stopCh:   make(chan struct{}),
		interval: interval,
	}
}

// Start 启动监控循环
func (cm *ContainerMonitor) Start() {
	go cm.loop()
	log.Println("[ContainerMonitor] Started with interval:", cm.interval)
}

// Stop 停止监控
func (cm *ContainerMonitor) Stop() {
	close(cm.stopCh)
}

// loop 监控循环
func (cm *ContainerMonitor) loop() {
	ticker := time.NewTicker(cm.interval)
	defer ticker.Stop()

	// 立即执行一次
	cm.collect()

	for {
		select {
		case <-ticker.C:
			cm.collect()
		case <-cm.stopCh:
			return
		}
	}
}

// collect 采集所有容器指标
func (cm *ContainerMonitor) collect() {
	containers, err := cm.listContainers()
	if err != nil {
		log.Printf("[ContainerMonitor] Failed to list containers: %v", err)
		return
	}

	for _, container := range containers {
		// 只监控 AI Corp 相关的容器（通过名称或标签过滤）
		if !cm.isAICorpContainer(container) {
			continue
		}

		metrics, err := cm.getContainerStats(container)
		if err != nil {
			log.Printf("[ContainerMonitor] Failed to get stats for %s: %v", container, err)
			continue
		}

		// 从容器名称中提取 task_id 和 agent_id
		cm.extractMetadata(metrics)

		// 保存到内存
		cm.mu.Lock()
		cm.metrics[metrics.ContainerID] = metrics
		cm.mu.Unlock()

		// 更新 Prometheus 指标
		cm.updatePrometheus(metrics)
	}
}

// listContainers 列出所有运行中的容器
func (cm *ContainerMonitor) listContainers() ([]string, error) {
	// 使用 docker ps 命令
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}:{{.Names}}:{{.Status}}:{{.CreatedAt}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}

	var containers []string
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			containers = append(containers, line)
		}
	}
	return containers, nil
}

// isAICorpContainer 判断是否为 AI Corp 管理的容器
func (cm *ContainerMonitor) isAICorpContainer(container string) bool {
	// 通过名称前缀或标签识别
	// 沙箱容器通常以 "sandbox-" 开头
	parts := strings.Split(container, ":")
	if len(parts) < 2 {
		return false
	}
	name := parts[1]
	return strings.HasPrefix(name, "sandbox-") ||
		strings.HasPrefix(name, "ai-corp-") ||
		strings.Contains(name, "agent-")
}

// getContainerStats 获取单个容器的 stats
func (cm *ContainerMonitor) getContainerStats(container string) (*ContainerMetrics, error) {
	parts := strings.SplitN(container, ":", 2)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid container format")
	}
	containerID := parts[0]
	containerName := ""
	if len(parts) > 1 {
		nameParts := strings.Split(parts[1], ":")
		containerName = nameParts[0]
	}

	// 使用 docker stats --no-stream 获取指标
	cmd := exec.Command("docker", "stats", containerID, "--no-stream", "--format",
		"{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}|{{.PIDs}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats failed: %w", err)
	}

	// 解析输出
	stats := &ContainerMetrics{
		ContainerID:   containerID,
		ContainerName: containerName,
		Timestamp:     time.Now(),
	}

	line := strings.TrimSpace(string(output))
	fields := strings.Split(line, "|")
	if len(fields) >= 5 {
		// CPU 百分比
		cpuStr := strings.TrimSuffix(fields[0], "%")
		stats.CPUPercent, _ = strconv.ParseFloat(strings.TrimSpace(cpuStr), 64)

		// 内存使用/限制
		memParts := strings.Split(fields[1], "/")
		if len(memParts) == 2 {
			stats.MemoryUsage = parseContainerMemory(memParts[0])
			stats.MemoryLimit = parseContainerMemory(memParts[1])
		}

		// 内存百分比
		memPercStr := strings.TrimSuffix(fields[2], "%")
		stats.MemoryPercent, _ = strconv.ParseFloat(strings.TrimSpace(memPercStr), 64)

		// 网络 I/O
		netParts := strings.Split(fields[3], "/")
		if len(netParts) == 2 {
			stats.NetworkRx = parseContainerBytes(netParts[0])
			stats.NetworkTx = parseContainerBytes(netParts[1])
		}

		// PIDs
		stats.PIDs, _ = strconv.Atoi(strings.TrimSpace(fields[4]))
	}

	// 获取容器详细信息（状态、运行时间）
	cm.getContainerDetails(containerID, stats)

	return stats, nil
}

// getContainerDetails 获取容器详细信息
func (cm *ContainerMonitor) getContainerDetails(containerID string, stats *ContainerMetrics) {
	cmd := exec.Command("docker", "inspect", containerID,
		"--format", "{{.State.Status}}|{{.State.StartedAt}}|{{.Config.Labels}}")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	parts := strings.SplitN(string(output), "|", 3)
	if len(parts) >= 2 {
		stats.Status = strings.TrimSpace(parts[0])

		// 计算运行时间
		startedAt := strings.TrimSpace(parts[1])
		if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
			stats.Uptime = int64(time.Since(t).Seconds())
		}
	}

	// 解析标签获取 task_id 和 agent_id
	if len(parts) >= 3 {
		labels := parts[2]
		if strings.Contains(labels, "task_id=") {
			stats.TaskID = extractLabel(labels, "task_id")
		}
		if strings.Contains(labels, "agent_id=") {
			stats.AgentID = extractLabel(labels, "agent_id")
		}
	}
}

// extractMetadata 从容器名称中提取元数据
func (cm *ContainerMonitor) extractMetadata(stats *ContainerMetrics) {
	// 如果标签中没有，尝试从名称解析
	// 格式: sandbox-<task_id>-<timestamp> 或 agent-<agent_id>-...
	name := stats.ContainerName

	if stats.TaskID == "" && strings.HasPrefix(name, "sandbox-") {
		parts := strings.Split(name, "-")
		if len(parts) >= 2 {
			stats.TaskID = parts[1]
		}
	}

	if stats.AgentID == "" && strings.HasPrefix(name, "agent-") {
		parts := strings.Split(name, "-")
		if len(parts) >= 2 {
			stats.AgentID = parts[1]
		}
	}
}

// updatePrometheus 更新 Prometheus 指标
func (cm *ContainerMonitor) updatePrometheus(m *ContainerMetrics) {
	labels := prometheus.Labels{
		"container_id":   m.ContainerID[:12], // 使用短ID
		"container_name": m.ContainerName,
		"task_id":        m.TaskID,
		"agent_id":       m.AgentID,
	}

	ContainerCPUUsage.With(labels).Set(m.CPUPercent)
	ContainerMemoryUsage.With(labels).Set(float64(m.MemoryUsage))
	ContainerMemoryLimit.With(labels).Set(float64(m.MemoryLimit))
	ContainerMemoryPercent.With(labels).Set(m.MemoryPercent)
	ContainerNetworkRx.With(labels).Set(float64(m.NetworkRx))
	ContainerNetworkTx.With(labels).Set(float64(m.NetworkTx))
	ContainerPIDs.With(labels).Set(float64(m.PIDs))
	ContainerUptime.With(labels).Set(float64(m.Uptime))

	status := 0.0
	if m.Status == "running" {
		status = 1.0
	}
	ContainerStatus.With(labels).Set(status)
}

// GetMetrics 获取指定容器的指标
func (cm *ContainerMonitor) GetMetrics(containerID string) *ContainerMetrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.metrics[containerID]
}

// GetAllMetrics 获取所有容器指标
func (cm *ContainerMonitor) GetAllMetrics() []*ContainerMetrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]*ContainerMetrics, 0, len(cm.metrics))
	for _, m := range cm.metrics {
		result = append(result, m)
	}
	return result
}

// GetMetricsByTask 获取指定任务的所有容器指标
func (cm *ContainerMonitor) GetMetricsByTask(taskID string) []*ContainerMetrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var result []*ContainerMetrics
	for _, m := range cm.metrics {
		if m.TaskID == taskID {
			result = append(result, m)
		}
	}
	return result
}

// GetMetricsByAgent 获取指定 Agent 的所有容器指标
func (cm *ContainerMonitor) GetMetricsByAgent(agentID string) []*ContainerMetrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var result []*ContainerMetrics
	for _, m := range cm.metrics {
		if m.AgentID == agentID {
			result = append(result, m)
		}
	}
	return result
}

// 辅助函数

func parseContainerMemory(s string) int64 {
	s = strings.TrimSpace(s)
	// 支持格式: "10MiB", "1GiB", "100kB"
	multiplier := int64(1)

	if strings.HasSuffix(s, "GiB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GiB")
	} else if strings.HasSuffix(s, "MiB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MiB")
	} else if strings.HasSuffix(s, "KiB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KiB")
	} else if strings.HasSuffix(s, "kB") {
		multiplier = 1000
		s = strings.TrimSuffix(s, "kB")
	} else if strings.HasSuffix(s, "GB") {
		multiplier = 1000 * 1000 * 1000
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1000 * 1000
		s = strings.TrimSuffix(s, "MB")
	}

	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(val * float64(multiplier))
}

func parseContainerBytes(s string) int64 {
	s = strings.TrimSpace(s)
	// 支持格式: "1.5MB", "100kB"
	multiplier := int64(1)

	if strings.HasSuffix(s, "GB") || strings.HasSuffix(s, "GiB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "i")
		s = strings.TrimSuffix(s, "G")
	} else if strings.HasSuffix(s, "MB") || strings.HasSuffix(s, "MiB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "i")
		s = strings.TrimSuffix(s, "M")
	} else if strings.HasSuffix(s, "kB") || strings.HasSuffix(s, "KiB") {
		multiplier = 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "i")
		s = strings.TrimSuffix(s, "k")
	}

	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(val * float64(multiplier))
}

func extractLabel(labels, key string) string {
	// 简单解析 labels 字符串
	prefix := key + "="
	if idx := strings.Index(labels, prefix); idx != -1 {
		rest := labels[idx+len(prefix):]
		if endIdx := strings.IndexAny(rest, " \t\n}"); endIdx != -1 {
			return strings.Trim(rest[:endIdx], "\"'")
		}
		return strings.Trim(rest, "\"'}")
	}
	return ""
}

// ContainerMetricsResponse API 响应结构
type ContainerMetricsResponse struct {
	Containers []*ContainerMetrics `json:"containers"`
	Count      int                 `json:"count"`
	Timestamp  time.Time           `json:"timestamp"`
}

// ToJSON 转换为 JSON
func (cm *ContainerMonitor) ToJSON() ([]byte, error) {
	response := ContainerMetricsResponse{
		Containers: cm.GetAllMetrics(),
		Count:      len(cm.metrics),
		Timestamp:  time.Now(),
	}
	return json.MarshalIndent(response, "", "  ")
}
