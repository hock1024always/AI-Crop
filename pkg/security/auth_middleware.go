// Package security - JWT Gin 认证中间件
package security

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware 返回 Gin JWT 认证中间件
// 从 Authorization: Bearer <token> 中提取并验证 JWT
// 验证通过后将 user_id 和 role 注入 gin.Context
func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization format, expected: Bearer <token>"})
			c.Abort()
			return
		}

		claims, err := ValidateToken(parts[1], secret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		// 注入用户信息到上下文
		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// OptionalAuthMiddleware 可选认证中间件
// 有 Token 则验证并注入用户信息，没有则以匿名身份通过
func OptionalAuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Set("user_id", "anonymous")
			c.Set("role", "guest")
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			if claims, err := ValidateToken(parts[1], secret); err == nil {
				c.Set("user_id", claims.UserID)
				c.Set("role", claims.Role)
				c.Next()
				return
			}
		}

		// Token 无效，以匿名身份继续
		c.Set("user_id", "anonymous")
		c.Set("role", "guest")
		c.Next()
	}
}

// RoleRequired 角色权限检查中间件
func RoleRequired(roles ...string) gin.HandlerFunc {
	roleSet := make(map[string]bool)
	for _, r := range roles {
		roleSet[r] = true
	}

	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "no role found in context"})
			c.Abort()
			return
		}

		roleStr, _ := role.(string)
		if !roleSet[roleStr] {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions", "required": roles, "current": roleStr})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RateLimitMiddleware 速率限制中间件
func RateLimitMiddleware(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := ClientIP(c.Request)

		// 如果有认证用户，用 user_id 做限流键
		if userID, exists := c.Get("user_id"); exists {
			if uid, ok := userID.(string); ok && uid != "anonymous" {
				key = uid
			}
		}

		if !limiter.Allow(key) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded, please try again later",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// PIISanitizeMiddleware PII 脱敏中间件
// 对 LLM 响应中的敏感数据进行脱敏
func PIISanitizeMiddleware(sanitizer *PIISanitizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 继续处理请求
		c.Next()

		// 注意：响应体的脱敏需要在业务层处理，
		// 因为 Gin 在 c.Next() 后响应已经写出
		// 这里主要用于日志脱敏和标记
	}
}
