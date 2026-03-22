// Package aiinfra 提供 AI 基础设施能力
// 包含：推理优化（KV Cache/批处理/量化）、指标采集、自训练数据收集、可视化 API
package aiinfra

import (
	"sync"
	"time"
)

// ---- 推理优化：KV Cache ----

type CacheEntry struct {
	Prompt    string
	Response  string
	Model     string
	CreatedAt time.Time
	HitCount  int
}

type InferenceCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	maxSize int
	hits    int64
	misses  int64
}

func NewInferenceCache(maxSize int) *InferenceCache {
	return &InferenceCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
	}
}

func (c *InferenceCache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if ok {
		c.mu.Lock()
		e.HitCount++
		c.mu.Unlock()
		c.hits++
		return e, true
	}
	c.misses++
	return nil, false
}

func (c *InferenceCache) Set(key, prompt, response, model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 简单 LRU：超出容量时删除最旧
	if len(c.entries) >= c.maxSize {
		var oldest *CacheEntry
		var oldestKey string
		for k, e := range c.entries {
			if oldest == nil || e.CreatedAt.Before(oldest.CreatedAt) {
				oldest = e
				oldestKey = k
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = &CacheEntry{
		Prompt:    prompt,
		Response:  response,
		Model:     model,
		CreatedAt: time.Now(),
	}
}

func (c *InferenceCache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := c.hits + c.misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100
	}
	return map[string]interface{}{
		"size":     len(c.entries),
		"max_size": c.maxSize,
		"hits":     c.hits,
		"misses":   c.misses,
		"hit_rate": hitRate,
	}
}

// ---- 推理指标采集 ----

type InferenceRecord struct {
	RequestID    string        `json:"request_id"`
	Model        string        `json:"model"`
	AgentID      string        `json:"agent_id"`
	Prompt       string        `json:"prompt"`
	Response     string        `json:"response"`
	InputTokens  int           `json:"input_tokens"`
	OutputTokens int           `json:"output_tokens"`
	Latency      time.Duration `json:"latency_ms"`
	CacheHit     bool          `json:"cache_hit"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

type InferenceMetrics struct {
	mu      sync.Mutex
	records []InferenceRecord
	maxLen  int

	// 聚合统计
	TotalRequests int64
	TotalTokensIn  int64
	TotalTokensOut int64
	TotalErrors    int64
	TotalLatencyMs int64
}

func NewInferenceMetrics(maxLen int) *InferenceMetrics {
	return &InferenceMetrics{
		records: make([]InferenceRecord, 0, maxLen),
		maxLen:  maxLen,
	}
}

func (m *InferenceMetrics) Record(r InferenceRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.records) >= m.maxLen {
		m.records = m.records[1:]
	}
	r.Timestamp = time.Now()
	m.records = append(m.records, r)
	m.TotalRequests++
	m.TotalTokensIn += int64(r.InputTokens)
	m.TotalTokensOut += int64(r.OutputTokens)
	m.TotalLatencyMs += r.Latency.Milliseconds()
	if !r.Success {
		m.TotalErrors++
	}
}

func (m *InferenceMetrics) Summary() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	avgLatency := int64(0)
	if m.TotalRequests > 0 {
		avgLatency = m.TotalLatencyMs / m.TotalRequests
	}
	// 按模型分组统计
	modelStats := make(map[string]int64)
	for _, r := range m.records {
		modelStats[r.Model]++
	}
	return map[string]interface{}{
		"total_requests":   m.TotalRequests,
		"total_tokens_in":  m.TotalTokensIn,
		"total_tokens_out": m.TotalTokensOut,
		"total_errors":     m.TotalErrors,
		"avg_latency_ms":   avgLatency,
		"error_rate":       float64(m.TotalErrors) / float64(max(m.TotalRequests, 1)) * 100,
		"model_distribution": modelStats,
	}
}

func (m *InferenceMetrics) RecentRecords(n int) []InferenceRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n > len(m.records) {
		n = len(m.records)
	}
	return m.records[len(m.records)-n:]
}

// ---- 训练数据收集（RLHF/微调数据集） ----

type FeedbackType string

const (
	FeedbackPositive FeedbackType = "positive"
	FeedbackNegative FeedbackType = "negative"
	FeedbackEdit     FeedbackType = "edit"    // 用户编辑了回复
)

type TrainingExample struct {
	ID           string       `json:"id"`
	Prompt       string       `json:"prompt"`
	Response     string       `json:"response"`    // 模型原始回复
	EditedResp   string       `json:"edited_resp"` // 用户编辑后的回复
	Feedback     FeedbackType `json:"feedback"`
	AgentType    string       `json:"agent_type"`
	Model        string       `json:"model"`
	Score        float64      `json:"score"` // 1-5
	CollectedAt  time.Time    `json:"collected_at"`
}

type TrainingDataset struct {
	mu       sync.Mutex
	examples []TrainingExample
}

func NewTrainingDataset() *TrainingDataset {
	return &TrainingDataset{}
}

func (d *TrainingDataset) Add(ex TrainingExample) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ex.CollectedAt = time.Now()
	d.examples = append(d.examples, ex)
}

func (d *TrainingDataset) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.examples)
}

func (d *TrainingDataset) Export() []TrainingExample {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]TrainingExample, len(d.examples))
	copy(out, d.examples)
	return out
}

// ExportForFineTuning 导出为微调格式（Alpaca/ShareGPT）
func (d *TrainingDataset) ExportForFineTuning(format string) []map[string]interface{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []map[string]interface{}
	for _, ex := range d.examples {
		if ex.Feedback == FeedbackNegative {
			continue // 跳过负面反馈
		}
		resp := ex.Response
		if ex.EditedResp != "" {
			resp = ex.EditedResp // 优先使用编辑后的回复
		}
		switch format {
		case "alpaca":
			out = append(out, map[string]interface{}{
				"instruction": ex.Prompt,
				"input":       "",
				"output":      resp,
			})
		case "sharegpt":
			out = append(out, map[string]interface{}{
				"conversations": []map[string]string{
					{"from": "human", "value": ex.Prompt},
					{"from": "gpt", "value": resp},
				},
			})
		default:
			out = append(out, map[string]interface{}{
				"prompt":   ex.Prompt,
				"response": resp,
			})
		}
	}
	return out
}

// ---- 量化配置建议 ----

type QuantizationConfig struct {
	ModelName   string  `json:"model_name"`
	OriginalGB  float64 `json:"original_gb"`
	Q4_K_M_GB   float64 `json:"q4_k_m_gb"`
	Q8_0_GB     float64 `json:"q8_0_gb"`
	Q4_K_M_PPL  float64 `json:"q4_k_m_ppl_loss"` // 困惑度损失
	Recommended string  `json:"recommended"`
	CPUTokensSec int    `json:"cpu_tokens_per_sec"`
}

var ModelQuantizationGuide = []QuantizationConfig{
	{
		ModelName:    "DeepSeek-Coder-1.3B",
		OriginalGB:   2.7,
		Q4_K_M_GB:    0.8,
		Q8_0_GB:      1.4,
		Q4_K_M_PPL:   0.15,
		Recommended:  "Q4_K_M",
		CPUTokensSec: 15,
	},
	{
		ModelName:    "DeepSeek-Coder-6.7B",
		OriginalGB:   13.4,
		Q4_K_M_GB:    4.1,
		Q8_0_GB:      7.1,
		Q4_K_M_PPL:   0.12,
		Recommended:  "Q4_K_M",
		CPUTokensSec: 4,
	},
	{
		ModelName:    "Qwen2.5-7B",
		OriginalGB:   15.0,
		Q4_K_M_GB:    4.5,
		Q8_0_GB:      8.0,
		Q4_K_M_PPL:   0.10,
		Recommended:  "Q4_K_M",
		CPUTokensSec: 3,
	},
	{
		ModelName:    "Llama-3.2-3B",
		OriginalGB:   6.4,
		Q4_K_M_GB:    2.0,
		Q8_0_GB:      3.4,
		Q4_K_M_PPL:   0.18,
		Recommended:  "Q4_K_M",
		CPUTokensSec: 8,
	},
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
