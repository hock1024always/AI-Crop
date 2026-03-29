// Package security - Token 配额管理
// 在 LLM 调用前检查额度，超限拒绝
package security

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// QuotaStore 配额持久化接口
type QuotaStore interface {
	GetTokenUsage(ctx context.Context, userID string, since time.Time) (int64, error)
	RecordTokenUsage(ctx context.Context, userID string, tokens int) error
}

// QuotaConfig 配额配置
type QuotaConfig struct {
	MaxTokensPerMinute int64 `json:"max_tokens_per_minute"`
	MaxTokensPerHour   int64 `json:"max_tokens_per_hour"`
	MaxTokensPerDay    int64 `json:"max_tokens_per_day"`
	MaxRequestsPerMin  int   `json:"max_requests_per_minute"`
}

// DefaultQuotaConfig 默认配额
func DefaultQuotaConfig() QuotaConfig {
	return QuotaConfig{
		MaxTokensPerMinute: 10000,
		MaxTokensPerHour:   100000,
		MaxTokensPerDay:    1000000,
		MaxRequestsPerMin:  60,
	}
}

// QuotaManager Token 配额管理器
type QuotaManager struct {
	config  QuotaConfig
	store   QuotaStore         // 可选的持久化存储
	usage   map[string]*usage  // 内存级快速检查
	mu      sync.RWMutex
}

type usage struct {
	minuteTokens int64
	hourTokens   int64
	dayTokens    int64
	minuteReqs   int
	minuteReset  time.Time
	hourReset    time.Time
	dayReset     time.Time
}

// NewQuotaManager 创建配额管理器
func NewQuotaManager(config QuotaConfig, store QuotaStore) *QuotaManager {
	return &QuotaManager{
		config: config,
		store:  store,
		usage:  make(map[string]*usage),
	}
}

// CheckQuota 检查是否有足够的配额
// 返回 nil 表示允许，否则返回错误说明原因
func (qm *QuotaManager) CheckQuota(userID string, estimatedTokens int) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	u := qm.getOrCreateUsage(userID)
	now := time.Now()

	// 重置过期的窗口
	if now.After(u.minuteReset) {
		u.minuteTokens = 0
		u.minuteReqs = 0
		u.minuteReset = now.Add(time.Minute)
	}
	if now.After(u.hourReset) {
		u.hourTokens = 0
		u.hourReset = now.Add(time.Hour)
	}
	if now.After(u.dayReset) {
		u.dayTokens = 0
		u.dayReset = now.Add(24 * time.Hour)
	}

	// 检查请求次数限制
	if qm.config.MaxRequestsPerMin > 0 && u.minuteReqs >= qm.config.MaxRequestsPerMin {
		return fmt.Errorf("rate limit exceeded: max %d requests per minute", qm.config.MaxRequestsPerMin)
	}

	// 检查 Token 限制
	estimated := int64(estimatedTokens)
	if qm.config.MaxTokensPerMinute > 0 && u.minuteTokens+estimated > qm.config.MaxTokensPerMinute {
		return fmt.Errorf("token quota exceeded: minute limit %d (used %d)", qm.config.MaxTokensPerMinute, u.minuteTokens)
	}
	if qm.config.MaxTokensPerHour > 0 && u.hourTokens+estimated > qm.config.MaxTokensPerHour {
		return fmt.Errorf("token quota exceeded: hour limit %d (used %d)", qm.config.MaxTokensPerHour, u.hourTokens)
	}
	if qm.config.MaxTokensPerDay > 0 && u.dayTokens+estimated > qm.config.MaxTokensPerDay {
		return fmt.Errorf("token quota exceeded: day limit %d (used %d)", qm.config.MaxTokensPerDay, u.dayTokens)
	}

	return nil
}

// RecordUsage 记录实际使用量
func (qm *QuotaManager) RecordUsage(userID string, tokensUsed int) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	u := qm.getOrCreateUsage(userID)
	u.minuteTokens += int64(tokensUsed)
	u.hourTokens += int64(tokensUsed)
	u.dayTokens += int64(tokensUsed)
	u.minuteReqs++

	// 异步持久化
	if qm.store != nil {
		go func() {
			_ = qm.store.RecordTokenUsage(context.Background(), userID, tokensUsed)
		}()
	}
}

// GetUsageStats 获取用户配额使用情况
func (qm *QuotaManager) GetUsageStats(userID string) map[string]interface{} {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	u, exists := qm.usage[userID]
	if !exists {
		return map[string]interface{}{
			"minute_tokens": 0, "hour_tokens": 0, "day_tokens": 0,
			"minute_requests": 0,
			"limits": qm.config,
		}
	}

	return map[string]interface{}{
		"minute_tokens":     u.minuteTokens,
		"hour_tokens":       u.hourTokens,
		"day_tokens":        u.dayTokens,
		"minute_requests":   u.minuteReqs,
		"minute_remaining":  maxZero(qm.config.MaxTokensPerMinute - u.minuteTokens),
		"hour_remaining":    maxZero(qm.config.MaxTokensPerHour - u.hourTokens),
		"day_remaining":     maxZero(qm.config.MaxTokensPerDay - u.dayTokens),
		"limits":            qm.config,
	}
}

func (qm *QuotaManager) getOrCreateUsage(userID string) *usage {
	u, exists := qm.usage[userID]
	if !exists {
		now := time.Now()
		u = &usage{
			minuteReset: now.Add(time.Minute),
			hourReset:   now.Add(time.Hour),
			dayReset:    now.Add(24 * time.Hour),
		}
		qm.usage[userID] = u
	}
	return u
}

func maxZero(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
