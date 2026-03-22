// Package security 提供 API 安全增强功能
// 包含：JWT 认证、Rate Limiting、请求审计、输入过滤
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- JWT ----

type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	Exp    int64  `json:"exp"`
}

func GenerateToken(userID, role, secret string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(Claims{
		UserID: userID,
		Role:   role,
		Exp:    time.Now().Add(24 * time.Hour).Unix(),
	})
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func ValidateToken(token, secret string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}
	body := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if expected != parts[2] {
		return nil, fmt.Errorf("invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if time.Now().Unix() > c.Exp {
		return nil, fmt.Errorf("token expired")
	}
	return &c, nil
}

// ---- Rate Limiter (sliding window) ----

type RateLimiter struct {
	mu       sync.Mutex
	windows  map[string][]time.Time
	maxReqs  int
	interval time.Duration
}

func NewRateLimiter(maxReqs int, interval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		windows:  make(map[string][]time.Time),
		maxReqs:  maxReqs,
		interval: interval,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.interval)
	win := rl.windows[key]
	// 过滤过期记录
	valid := win[:0]
	for _, t := range win {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= rl.maxReqs {
		rl.windows[key] = valid
		return false
	}
	rl.windows[key] = append(valid, now)
	return true
}

func (rl *RateLimiter) cleanup() {
	for range time.Tick(time.Minute) {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.interval)
		for key, win := range rl.windows {
			valid := win[:0]
			for _, t := range win {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(rl.windows, key)
			} else {
				rl.windows[key] = valid
			}
		}
		rl.mu.Unlock()
	}
}

// ---- Audit Logger ----

type AuditEvent struct {
	Time     time.Time `json:"time"`
	ClientIP string    `json:"client_ip"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Status   int       `json:"status"`
	Duration int64     `json:"duration_ms"`
	UserID   string    `json:"user_id,omitempty"`
}

type AuditLog struct {
	mu     sync.Mutex
	events []AuditEvent
	max    int
}

func NewAuditLog(max int) *AuditLog {
	return &AuditLog{events: make([]AuditEvent, 0, max), max: max}
}

func (a *AuditLog) Record(e AuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) >= a.max {
		a.events = a.events[1:]
	}
	a.events = append(a.events, e)
}

func (a *AuditLog) Recent(n int) []AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n > len(a.events) {
		n = len(a.events)
	}
	return a.events[len(a.events)-n:]
}

// ---- Input Sanitizer ----

var dangerousPatterns = []string{
	"<script", "javascript:", "onload=", "onerror=",
	"'; DROP", "\" OR \"1\"=\"1", "--", "/*", "*/",
}

func SanitizeInput(s string) (string, bool) {
	lower := strings.ToLower(s)
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return "", false
		}
	}
	return s, true
}

// ---- HTTP Middleware helpers ----

func ClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
