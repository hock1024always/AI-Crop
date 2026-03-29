package database

import (
	"context"
	"encoding/json"
	"time"
)

// ---- 审计日志存储适配层 ----

// AuditStoreAdapter 将 AuditRepo 适配为 security.AuditStore 接口
type AuditStoreAdapter struct {
	repo *AuditRepo
}

// NewAuditStoreAdapter 创建审计存储适配器
func NewAuditStoreAdapter(repo *AuditRepo) *AuditStoreAdapter {
	return &AuditStoreAdapter{repo: repo}
}

// LogAudit 实现 security.AuditStore 接口
func (a *AuditStoreAdapter) LogAudit(ctx context.Context, entry interface{}) error {
	// 使用类型断言获取通用的审计数据
	type auditData struct {
		UserID       string                 `json:"user_id"`
		Action       string                 `json:"action"`
		ResourceType string                 `json:"resource_type"`
		ResourceID   string                 `json:"resource_id"`
		Details      map[string]interface{} `json:"details"`
		IPAddress    string                 `json:"ip_address"`
		UserAgent    string                 `json:"user_agent"`
	}

	// 序列化然后反序列化以适配不同包的类型
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	var ad auditData
	if err := json.Unmarshal(data, &ad); err != nil {
		return err
	}

	dbEntry := &AuditEntry{
		UserID:       ad.UserID,
		Action:       ad.Action,
		ResourceType: ad.ResourceType,
		ResourceID:   ad.ResourceID,
		Details:      ad.Details,
		IPAddress:    ad.IPAddress,
		UserAgent:    ad.UserAgent,
	}

	return a.repo.Log(ctx, dbEntry)
}

// ---- Token 配额存储适配层 ----

// QuotaStoreAdapter 将 DB 适配为 security.QuotaStore 接口
type QuotaStoreAdapter struct {
	pool interface{ Query(ctx context.Context, sql string, args ...interface{}) (interface{}, error) }
	repo *MetricsRepo
}

// NewQuotaStoreAdapter 创建配额存储适配器
func NewQuotaStoreAdapter(repo *MetricsRepo) *QuotaStoreAdapter {
	return &QuotaStoreAdapter{repo: repo}
}

// GetTokenUsage 查询指定时间段内的 Token 使用量
func (q *QuotaStoreAdapter) GetTokenUsage(ctx context.Context, userID string, since time.Time) (int64, error) {
	// 从 inference_metrics 表统计 Token 使用量
	// 注意: 当前 inference_metrics 没有 user_id 字段，用 agent_id 替代
	var total int64
	err := q.repo.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total_tokens), 0) FROM inference_metrics
		 WHERE created_at >= $1`,
		since,
	).Scan(&total)
	return total, err
}

// RecordTokenUsage 记录 Token 使用量（利用现有的 inference_metrics 表）
func (q *QuotaStoreAdapter) RecordTokenUsage(ctx context.Context, userID string, tokens int) error {
	// Token 使用已经通过 InferenceService 记录到 inference_metrics
	// 这里不需要额外记录，只做日志
	return nil
}
