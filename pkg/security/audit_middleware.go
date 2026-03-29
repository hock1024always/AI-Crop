// Package security - Gin 审计日志中间件
// 自动记录所有 HTTP 请求到 audit_log 表
package security

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

// AuditStore 审计日志持久化接口
type AuditStore interface {
	LogAudit(ctx context.Context, entry *AuditEntry) error
}

// AuditEntry 审计日志条目（与 DB 对齐）
type AuditEntry struct {
	UserID       string                 `json:"user_id"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id"`
	Details      map[string]interface{} `json:"details"`
	IPAddress    string                 `json:"ip_address"`
	UserAgent    string                 `json:"user_agent"`
	CreatedAt    time.Time              `json:"created_at"`
}

// AuditMiddleware 返回 Gin 中间件，自动将 HTTP 请求写入审计日志
func AuditMiddleware(store AuditStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 执行请求
		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()

		// 解析资源类型和 ID
		resourceType, resourceID := parseResource(c.FullPath(), c.Param("id"))

		// 解析操作类型
		action := methodToAction(c.Request.Method)

		// 从上下文取 user_id（JWT 中间件会注入）
		userID, _ := c.Get("user_id")
		userIDStr, _ := userID.(string)

		entry := &AuditEntry{
			UserID:       userIDStr,
			Action:       action,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			Details: map[string]interface{}{
				"method":      c.Request.Method,
				"path":        c.Request.URL.Path,
				"status":      status,
				"duration_ms": duration.Milliseconds(),
				"query":       c.Request.URL.RawQuery,
			},
			IPAddress: ClientIP(c.Request),
			UserAgent: c.Request.UserAgent(),
			CreatedAt: start,
		}

		// 异步写入，不阻塞请求
		go func() {
			if err := store.LogAudit(context.Background(), entry); err != nil {
				log.Printf("[Audit] Failed to persist audit log: %v", err)
			}
		}()
	}
}

// methodToAction 将 HTTP 方法映射为审计操作
func methodToAction(method string) string {
	switch method {
	case "GET":
		return "read"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "api_call"
	}
}

// parseResource 从路由路径提取资源类型和 ID
func parseResource(fullPath, paramID string) (string, string) {
	switch {
	case contains(fullPath, "/agents"):
		return "agent", paramID
	case contains(fullPath, "/tasks"):
		return "task", paramID
	case contains(fullPath, "/chat"):
		return "chat", ""
	case contains(fullPath, "/workflows"):
		return "workflow", paramID
	case contains(fullPath, "/rag"):
		return "rag", ""
	case contains(fullPath, "/db"):
		return "database", ""
	default:
		return "system", ""
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
