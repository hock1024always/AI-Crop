package security

import (
	"testing"
	"time"
)

// ==== PII 脱敏测试 ====

func TestPIIDetectPhone(t *testing.T) {
	ps := NewPIISanitizer()
	text := "我的手机号是13812345678，请联系我"
	detections := ps.Detect(text)

	found := false
	for _, d := range detections {
		if d.Type == PIIPhone && d.Value == "13812345678" {
			found = true
			if d.Masked != "138****5678" {
				t.Errorf("phone mask: got %q, want %q", d.Masked, "138****5678")
			}
		}
	}
	if !found {
		t.Error("phone number not detected")
	}
}

func TestPIIDetectIDCard(t *testing.T) {
	ps := NewPIISanitizer()
	text := "身份证号: 110101199001011234"
	detections := ps.Detect(text)

	found := false
	for _, d := range detections {
		if d.Type == PIIIDCard {
			found = true
			if d.Masked != "110***********1234" {
				t.Errorf("id card mask: got %q, want %q", d.Masked, "110***********1234")
			}
		}
	}
	if !found {
		t.Error("id card not detected")
	}
}

func TestPIIDetectEmail(t *testing.T) {
	ps := NewPIISanitizer()
	text := "邮箱是 test@example.com"
	detections := ps.Detect(text)

	found := false
	for _, d := range detections {
		if d.Type == PIIEmail {
			found = true
			if d.Masked != "t***@example.com" {
				t.Errorf("email mask: got %q, want %q", d.Masked, "t***@example.com")
			}
		}
	}
	if !found {
		t.Error("email not detected")
	}
}

func TestPIISanitize(t *testing.T) {
	ps := NewPIISanitizer()
	text := "用户手机13912345678，邮箱admin@corp.com"
	result := ps.Sanitize(text)

	if result == text {
		t.Error("sanitize should have modified text")
	}
	// 不应包含原始手机号
	if containsStr(result, "13912345678") {
		t.Error("sanitized text still contains phone number")
	}
	// 不应包含原始邮箱用户名
	if containsStr(result, "admin@") {
		t.Error("sanitized text still contains email")
	}
}

func TestPIIHasPII(t *testing.T) {
	ps := NewPIISanitizer()

	if !ps.HasPII("call me 13800138000") {
		t.Error("should detect phone")
	}
	if ps.HasPII("hello world, no pii here") {
		t.Error("should not detect PII in clean text")
	}
}

func TestPIIDisableType(t *testing.T) {
	ps := NewPIISanitizer()
	ps.SetEnabled(PIIPhone, false)

	text := "手机13812345678"
	if ps.HasPII(text) {
		// 可能仍然匹配银行卡（数字长度），但不应匹配为手机
		detections := ps.Detect(text)
		for _, d := range detections {
			if d.Type == PIIPhone {
				t.Error("phone detection should be disabled")
			}
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ==== Token 配额测试 ====

func TestQuotaCheckPass(t *testing.T) {
	qm := NewQuotaManager(DefaultQuotaConfig(), nil)
	err := qm.CheckQuota("user1", 100)
	if err != nil {
		t.Errorf("quota check should pass: %v", err)
	}
}

func TestQuotaMinuteLimit(t *testing.T) {
	config := QuotaConfig{
		MaxTokensPerMinute: 500,
		MaxTokensPerHour:   100000,
		MaxTokensPerDay:    1000000,
		MaxRequestsPerMin:  100,
	}
	qm := NewQuotaManager(config, nil)

	// 使用 400 tokens
	qm.RecordUsage("user1", 400)

	// 再请求 200 应该超限
	err := qm.CheckQuota("user1", 200)
	if err == nil {
		t.Error("should exceed minute token limit")
	}
}

func TestQuotaRequestLimit(t *testing.T) {
	config := QuotaConfig{
		MaxTokensPerMinute: 100000,
		MaxTokensPerHour:   1000000,
		MaxTokensPerDay:    10000000,
		MaxRequestsPerMin:  3,
	}
	qm := NewQuotaManager(config, nil)

	// 消耗 3 个请求
	qm.RecordUsage("user1", 10)
	qm.RecordUsage("user1", 10)
	qm.RecordUsage("user1", 10)

	// 第 4 个请求应该被拒绝
	err := qm.CheckQuota("user1", 10)
	if err == nil {
		t.Error("should exceed request limit")
	}
}

func TestQuotaUsageStats(t *testing.T) {
	qm := NewQuotaManager(DefaultQuotaConfig(), nil)
	qm.RecordUsage("user1", 500)

	stats := qm.GetUsageStats("user1")
	if stats["minute_tokens"].(int64) != 500 {
		t.Errorf("minute_tokens: got %v, want 500", stats["minute_tokens"])
	}
}

func TestQuotaMultipleUsers(t *testing.T) {
	config := QuotaConfig{
		MaxTokensPerMinute: 1000,
		MaxTokensPerHour:   100000,
		MaxTokensPerDay:    1000000,
		MaxRequestsPerMin:  100,
	}
	qm := NewQuotaManager(config, nil)

	qm.RecordUsage("user1", 900)
	qm.RecordUsage("user2", 100)

	// user1 应该超限
	if qm.CheckQuota("user1", 200) == nil {
		t.Error("user1 should exceed limit")
	}
	// user2 不应超限
	if qm.CheckQuota("user2", 200) != nil {
		t.Error("user2 should not exceed limit")
	}
}

// ==== JWT 测试 ====

func TestJWTGenerateAndValidate(t *testing.T) {
	secret := "test-secret-key"
	token := GenerateToken("user123", "admin", secret)

	claims, err := ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("user_id: got %q, want %q", claims.UserID, "user123")
	}
	if claims.Role != "admin" {
		t.Errorf("role: got %q, want %q", claims.Role, "admin")
	}
}

func TestJWTInvalidSignature(t *testing.T) {
	token := GenerateToken("user1", "admin", "secret1")
	_, err := ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Error("should reject token with wrong secret")
	}
}

func TestJWTInvalidFormat(t *testing.T) {
	_, err := ValidateToken("not-a-jwt", "secret")
	if err == nil {
		t.Error("should reject invalid token format")
	}
}

// ==== Rate Limiter 测试 ====

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	if !rl.Allow("key1") {
		t.Error("first request should be allowed")
	}
	if !rl.Allow("key1") {
		t.Error("second request should be allowed")
	}
	if !rl.Allow("key1") {
		t.Error("third request should be allowed")
	}
	if rl.Allow("key1") {
		t.Error("fourth request should be rejected")
	}
}

func TestRateLimiterDifferentKeys(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	if !rl.Allow("key1") {
		t.Error("key1 first request should be allowed")
	}
	if !rl.Allow("key2") {
		t.Error("key2 first request should be allowed")
	}
	if rl.Allow("key1") {
		t.Error("key1 second request should be rejected")
	}
}

// ==== Input Sanitizer 测试 ====

func TestSanitizeInputClean(t *testing.T) {
	result, safe := SanitizeInput("hello world")
	if !safe {
		t.Error("clean input should be safe")
	}
	if result != "hello world" {
		t.Errorf("got %q, want %q", result, "hello world")
	}
}

func TestSanitizeInputXSS(t *testing.T) {
	_, safe := SanitizeInput("<script>alert('xss')</script>")
	if safe {
		t.Error("XSS input should be rejected")
	}
}

func TestSanitizeInputSQLInjection(t *testing.T) {
	_, safe := SanitizeInput("'; DROP TABLE users")
	if safe {
		t.Error("SQL injection should be rejected")
	}
}

// ==== Audit Log 测试 ====

func TestAuditLogRecord(t *testing.T) {
	al := NewAuditLog(100)

	al.Record(AuditEvent{
		Time:     time.Now(),
		ClientIP: "127.0.0.1",
		Method:   "GET",
		Path:     "/api/v1/agents",
		Status:   200,
		Duration: 15,
		UserID:   "user1",
	})

	recent := al.Recent(10)
	if len(recent) != 1 {
		t.Fatalf("expected 1 event, got %d", len(recent))
	}
	if recent[0].UserID != "user1" {
		t.Errorf("user_id: got %q, want %q", recent[0].UserID, "user1")
	}
}

func TestAuditLogMaxCapacity(t *testing.T) {
	al := NewAuditLog(3)

	for i := 0; i < 5; i++ {
		al.Record(AuditEvent{
			Time:   time.Now(),
			Path:   "/test",
			Status: 200,
			UserID: "user1",
		})
	}

	recent := al.Recent(10)
	if len(recent) != 3 {
		t.Errorf("expected max 3 events, got %d", len(recent))
	}
}
