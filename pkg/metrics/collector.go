package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Collector 指标采集器
type Collector struct {
	mu sync.RWMutex

	// Token 统计
	tokenUsage map[string]*TokenUsage // model -> usage

	// 执行统计
	execStats *ExecStats

	// 容器指标
	containerStats map[string]*ContainerStats

	// 网络指标
	networkStats *NetworkStats

	// Agent 指标
	agentStats map[string]*AgentStats
}

// TokenUsage Token 使用统计
type TokenUsage struct {
	Model       string  `json:"model"`
	InputTotal  int64   `json:"input_total"`
	OutputTotal int64   `json:"output_total"`
	InputRate   float64 `json:"input_rate"`  // tokens/sec
	OutputRate  float64 `json:"output_rate"` // tokens/sec
	CostUSD     float64 `json:"cost_usd"`
	lastUpdate  time.Time
}

// ExecStats 执行统计
type ExecStats struct {
	TotalExecutions int64         `json:"total_executions"`
	SuccessCount    int64         `json:"success_count"`
	FailCount       int64         `json:"fail_count"`
	AvgLatencyMs    float64       `json:"avg_latency_ms"`
	MaxMemoryMB     float64       `json:"max_memory_mb"`
	AvgMemoryMB     float64       `json:"avg_memory_mb"`
	latencies       []float64
	memories        []float64
}

// ContainerStats 容器指标
type ContainerStats struct {
	Name      string  `json:"name"`
	ID        string  `json:"id"`
	CPUPct    float64 `json:"cpu_pct"`
	MemoryMB  float64 `json:"memory_mb"`
	MemoryPct float64 `json:"memory_pct"`
	NetIn     int64   `json:"net_in"`
	NetOut    int64   `json:"net_out"`
	Status    string  `json:"status"`
}

// NetworkStats 网络指标
type NetworkStats struct {
	BytesIn     int64   `json:"bytes_in"`
	BytesOut    int64   `json:"bytes_out"`
	Connections int     `json:"connections"`
	RateIn      float64 `json:"rate_in"`  // bytes/sec
	RateOut     float64 `json:"rate_out"` // bytes/sec
}

// AgentStats Agent 指标
type AgentStats struct {
	AgentID        string  `json:"agent_id"`
	TasksCompleted int64   `json:"tasks_completed"`
	TasksFailed    int64   `json:"tasks_failed"`
	AvgTaskTimeMs  float64 `json:"avg_task_time_ms"`
	TokensUsed     int64   `json:"tokens_used"`
	Status         string  `json:"status"`
}

// Snapshot 指标快照（用于前端展示）
type Snapshot struct {
	Timestamp      int64                       `json:"timestamp"`
	System         *SystemStats                `json:"system"`
	Tokens         map[string]*TokenUsage      `json:"tokens"`
	Execution      *ExecStats                  `json:"execution"`
	Containers     map[string]*ContainerStats  `json:"containers"`
	Network        *NetworkStats               `json:"network"`
	Agents         map[string]*AgentStats      `json:"agents"`
}

// SystemStats 系统级指标
type SystemStats struct {
	CPUPct     float64 `json:"cpu_pct"`
	MemoryMB   float64 `json:"memory_mb"`
	MemoryPct  float64 `json:"memory_pct"`
	GoRoutines int     `json:"goroutines"`
	Uptime     int64   `json:"uptime_sec"`
}

// NewCollector 创建采集器
func NewCollector() *Collector {
	return &Collector{
		tokenUsage:     make(map[string]*TokenUsage),
		execStats:      &ExecStats{},
		containerStats: make(map[string]*ContainerStats),
		networkStats:   &NetworkStats{},
		agentStats:     make(map[string]*AgentStats),
	}
}

var startTime = time.Now()

// --- 记录方法 ---

// RecordTokenUsage 记录 Token 使用
func (c *Collector) RecordTokenUsage(model string, inputTokens, outputTokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	usage, ok := c.tokenUsage[model]
	if !ok {
		usage = &TokenUsage{Model: model}
		c.tokenUsage[model] = usage
	}

	now := time.Now()
	elapsed := now.Sub(usage.lastUpdate).Seconds()
	if elapsed > 0 && usage.lastUpdate.IsZero() == false {
		usage.InputRate = float64(inputTokens) / elapsed
		usage.OutputRate = float64(outputTokens) / elapsed
	}

	usage.InputTotal += int64(inputTokens)
	usage.OutputTotal += int64(outputTokens)
	usage.lastUpdate = now

	// 简单费用估算（以 DeepSeek 价格为例）
	usage.CostUSD += float64(inputTokens) * 0.0000014
	usage.CostUSD += float64(outputTokens) * 0.0000028
}

// RecordExecution 记录代码执行
func (c *Collector) RecordExecution(durationMs float64, memoryMB float64, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.execStats.TotalExecutions++
	if success {
		c.execStats.SuccessCount++
	} else {
		c.execStats.FailCount++
	}

	c.execStats.latencies = append(c.execStats.latencies, durationMs)
	c.execStats.memories = append(c.execStats.memories, memoryMB)

	// 保留最近 1000 条
	if len(c.execStats.latencies) > 1000 {
		c.execStats.latencies = c.execStats.latencies[len(c.execStats.latencies)-1000:]
	}
	if len(c.execStats.memories) > 1000 {
		c.execStats.memories = c.execStats.memories[len(c.execStats.memories)-1000:]
	}

	// 计算平均值
	c.execStats.AvgLatencyMs = avg(c.execStats.latencies)
	c.execStats.AvgMemoryMB = avg(c.execStats.memories)
	c.execStats.MaxMemoryMB = max(c.execStats.memories)
}

// RecordAgentTask 记录 Agent 任务
func (c *Collector) RecordAgentTask(agentID string, durationMs float64, tokens int, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats, ok := c.agentStats[agentID]
	if !ok {
		stats = &AgentStats{AgentID: agentID}
		c.agentStats[agentID] = stats
	}

	if success {
		stats.TasksCompleted++
	} else {
		stats.TasksFailed++
	}
	stats.TokensUsed += int64(tokens)

	total := stats.TasksCompleted + stats.TasksFailed
	if total > 0 {
		stats.AvgTaskTimeMs = (stats.AvgTaskTimeMs*float64(total-1) + durationMs) / float64(total)
	}
}

// UpdateAgentStatus 更新 Agent 状态
func (c *Collector) UpdateAgentStatus(agentID, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats, ok := c.agentStats[agentID]
	if !ok {
		stats = &AgentStats{AgentID: agentID}
		c.agentStats[agentID] = stats
	}
	stats.Status = status
}

// --- 采集方法 ---

// CollectContainerStats 采集 Docker 容器指标
func (c *Collector) CollectContainerStats() {
	out, err := exec.Command("docker", "stats", "--no-stream", "--format",
		"{{.Name}}\t{{.ID}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}").Output()
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}

		cpuPct := parsePercent(parts[2])
		memPct := parsePercent(parts[4])
		memMB := parseMemory(parts[3])
		netIn, netOut := parseNetIO(parts[5])

		c.containerStats[parts[0]] = &ContainerStats{
			Name:      parts[0],
			ID:        parts[1][:12],
			CPUPct:    cpuPct,
			MemoryMB:  memMB,
			MemoryPct: memPct,
			NetIn:     netIn,
			NetOut:    netOut,
			Status:    "running",
		}
	}
}

// collectSystemStats 采集系统指标
func (c *Collector) collectSystemStats() *SystemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &SystemStats{
		MemoryMB:   float64(m.Alloc) / 1024 / 1024,
		GoRoutines: runtime.NumGoroutine(),
		Uptime:     int64(time.Since(startTime).Seconds()),
	}
}

// --- 快照 ---

// GetSnapshot 获取当前指标快照
func (c *Collector) GetSnapshot() *Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 深拷贝
	tokens := make(map[string]*TokenUsage)
	for k, v := range c.tokenUsage {
		copy := *v
		tokens[k] = &copy
	}

	containers := make(map[string]*ContainerStats)
	for k, v := range c.containerStats {
		copy := *v
		containers[k] = &copy
	}

	agents := make(map[string]*AgentStats)
	for k, v := range c.agentStats {
		copy := *v
		agents[k] = &copy
	}

	execCopy := *c.execStats
	netCopy := *c.networkStats

	return &Snapshot{
		Timestamp:  time.Now().UnixMilli(),
		System:     c.collectSystemStats(),
		Tokens:     tokens,
		Execution:  &execCopy,
		Containers: containers,
		Network:    &netCopy,
		Agents:     agents,
	}
}

// --- 定时采集 ---

// StartCollecting 启动定时采集
func (c *Collector) StartCollecting(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.CollectContainerStats()
		case <-ctx.Done():
			return
		}
	}
}

// --- HTTP Handler ---

// HTTPHandler 返回指标 HTTP 处理器
func (c *Collector) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot := c.GetSnapshot()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshot)
	}
}

// PrometheusHandler 返回 Prometheus 格式指标
func (c *Collector) PrometheusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.mu.RLock()
		defer c.mu.RUnlock()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		// Token 指标
		for model, usage := range c.tokenUsage {
			fmt.Fprintf(w, "llm_tokens_input_total{model=%q} %d\n", model, usage.InputTotal)
			fmt.Fprintf(w, "llm_tokens_output_total{model=%q} %d\n", model, usage.OutputTotal)
			fmt.Fprintf(w, "llm_tokens_input_rate{model=%q} %.2f\n", model, usage.InputRate)
			fmt.Fprintf(w, "llm_tokens_output_rate{model=%q} %.2f\n", model, usage.OutputRate)
			fmt.Fprintf(w, "llm_cost_usd{model=%q} %.6f\n", model, usage.CostUSD)
		}

		// 执行指标
		fmt.Fprintf(w, "execution_total %d\n", c.execStats.TotalExecutions)
		fmt.Fprintf(w, "execution_success_total %d\n", c.execStats.SuccessCount)
		fmt.Fprintf(w, "execution_fail_total %d\n", c.execStats.FailCount)
		fmt.Fprintf(w, "execution_avg_latency_ms %.2f\n", c.execStats.AvgLatencyMs)
		fmt.Fprintf(w, "execution_max_memory_mb %.2f\n", c.execStats.MaxMemoryMB)

		// 容器指标
		for name, cs := range c.containerStats {
			fmt.Fprintf(w, "container_cpu_pct{name=%q} %.2f\n", name, cs.CPUPct)
			fmt.Fprintf(w, "container_memory_mb{name=%q} %.2f\n", name, cs.MemoryMB)
			fmt.Fprintf(w, "container_net_in_bytes{name=%q} %d\n", name, cs.NetIn)
			fmt.Fprintf(w, "container_net_out_bytes{name=%q} %d\n", name, cs.NetOut)
		}

		// Agent 指标
		for id, as := range c.agentStats {
			fmt.Fprintf(w, "agent_tasks_completed{id=%q} %d\n", id, as.TasksCompleted)
			fmt.Fprintf(w, "agent_tasks_failed{id=%q} %d\n", id, as.TasksFailed)
			fmt.Fprintf(w, "agent_tokens_used{id=%q} %d\n", id, as.TokensUsed)
		}

		// 系统指标
		sys := c.collectSystemStats()
		fmt.Fprintf(w, "system_memory_mb %.2f\n", sys.MemoryMB)
		fmt.Fprintf(w, "system_goroutines %d\n", sys.GoRoutines)
		fmt.Fprintf(w, "system_uptime_sec %d\n", sys.Uptime)
	}
}

// --- 辅助函数 ---

func parsePercent(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseMemory(s string) float64 {
	// "123.4MiB / 1GiB"
	parts := strings.Split(s, "/")
	if len(parts) == 0 {
		return 0
	}
	mem := strings.TrimSpace(parts[0])
	mem = strings.Replace(mem, "GiB", "", 1)
	mem = strings.Replace(mem, "MiB", "", 1)
	mem = strings.Replace(mem, "KiB", "", 1)
	v, _ := strconv.ParseFloat(mem, 64)

	if strings.Contains(parts[0], "GiB") {
		v *= 1024
	} else if strings.Contains(parts[0], "KiB") {
		v /= 1024
	}
	return v
}

func parseNetIO(s string) (int64, int64) {
	// "1.2kB / 3.4MB"
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseBytes(parts[0]), parseBytes(parts[1])
}

func parseBytes(s string) int64 {
	s = strings.TrimSpace(s)
	multiplier := int64(1)
	if strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	} else if strings.HasSuffix(s, "kB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "kB")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(v * float64(multiplier))
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func max(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// Global instance
var globalCollector *Collector
var once sync.Once

// Global 获取全局采集器
func Global() *Collector {
	once.Do(func() {
		globalCollector = NewCollector()
		log.Println("[metrics] Global collector initialized")
	})
	return globalCollector
}
