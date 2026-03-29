// Package security - PII 脱敏
// 检测并遮盖敏感数据：手机号、身份证、银行卡、邮箱等
package security

import (
	"regexp"
	"strings"
)

// PIIType 敏感数据类型
type PIIType string

const (
	PIIPhone    PIIType = "phone"
	PIIIDCard   PIIType = "id_card"
	PIIBankCard PIIType = "bank_card"
	PIIEmail    PIIType = "email"
)

// PIIDetection 检测结果
type PIIDetection struct {
	Type     PIIType `json:"type"`
	Value    string  `json:"value"`
	Masked   string  `json:"masked"`
	Position int     `json:"position"`
}

var (
	// 中国手机号：1开头，第二位3-9，共11位
	phoneRegex = regexp.MustCompile(`1[3-9]\d{9}`)
	// 中国身份证：18位，最后一位可能是X
	idCardRegex = regexp.MustCompile(`[1-9]\d{5}(19|20)\d{2}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\d{3}[\dXx]`)
	// 银行卡号：16-19位数字
	bankCardRegex = regexp.MustCompile(`\d{16,19}`)
	// 邮箱
	emailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

// PIISanitizer PII 脱敏器
type PIISanitizer struct {
	enabledTypes map[PIIType]bool
}

// NewPIISanitizer 创建脱敏器，默认启用所有类型
func NewPIISanitizer() *PIISanitizer {
	return &PIISanitizer{
		enabledTypes: map[PIIType]bool{
			PIIPhone:    true,
			PIIIDCard:   true,
			PIIBankCard: true,
			PIIEmail:    true,
		},
	}
}

// SetEnabled 启用或禁用某种 PII 类型的检测
func (ps *PIISanitizer) SetEnabled(piiType PIIType, enabled bool) {
	ps.enabledTypes[piiType] = enabled
}

// Detect 检测文本中的所有 PII
func (ps *PIISanitizer) Detect(text string) []PIIDetection {
	var detections []PIIDetection

	if ps.enabledTypes[PIIPhone] {
		for _, match := range phoneRegex.FindAllStringIndex(text, -1) {
			value := text[match[0]:match[1]]
			detections = append(detections, PIIDetection{
				Type:     PIIPhone,
				Value:    value,
				Masked:   maskPhone(value),
				Position: match[0],
			})
		}
	}

	if ps.enabledTypes[PIIIDCard] {
		for _, match := range idCardRegex.FindAllStringIndex(text, -1) {
			value := text[match[0]:match[1]]
			detections = append(detections, PIIDetection{
				Type:     PIIIDCard,
				Value:    value,
				Masked:   maskIDCard(value),
				Position: match[0],
			})
		}
	}

	if ps.enabledTypes[PIIEmail] {
		for _, match := range emailRegex.FindAllStringIndex(text, -1) {
			value := text[match[0]:match[1]]
			detections = append(detections, PIIDetection{
				Type:     PIIEmail,
				Value:    value,
				Masked:   maskEmail(value),
				Position: match[0],
			})
		}
	}

	if ps.enabledTypes[PIIBankCard] {
		for _, match := range bankCardRegex.FindAllStringIndex(text, -1) {
			value := text[match[0]:match[1]]
			// 排除已被识别为身份证号的
			isIDCard := false
			for _, d := range detections {
				if d.Type == PIIIDCard && match[0] >= d.Position && match[0] < d.Position+len(d.Value) {
					isIDCard = true
					break
				}
			}
			if !isIDCard {
				detections = append(detections, PIIDetection{
					Type:     PIIBankCard,
					Value:    value,
					Masked:   maskBankCard(value),
					Position: match[0],
				})
			}
		}
	}

	return detections
}

// Sanitize 对文本中的所有 PII 进行脱敏替换
func (ps *PIISanitizer) Sanitize(text string) string {
	result := text

	// 按优先级顺序处理：先身份证（最长），再手机号，再银行卡，最后邮箱
	if ps.enabledTypes[PIIIDCard] {
		result = idCardRegex.ReplaceAllStringFunc(result, maskIDCard)
	}
	if ps.enabledTypes[PIIPhone] {
		result = phoneRegex.ReplaceAllStringFunc(result, maskPhone)
	}
	if ps.enabledTypes[PIIEmail] {
		result = emailRegex.ReplaceAllStringFunc(result, maskEmail)
	}
	// 银行卡号最后处理，避免误匹配已脱敏的内容
	if ps.enabledTypes[PIIBankCard] {
		result = bankCardRegex.ReplaceAllStringFunc(result, func(s string) string {
			// 如果已经包含 * 说明已被脱敏，跳过
			if strings.Contains(s, "*") {
				return s
			}
			return maskBankCard(s)
		})
	}

	return result
}

// HasPII 快速判断文本中是否包含 PII
func (ps *PIISanitizer) HasPII(text string) bool {
	if ps.enabledTypes[PIIPhone] && phoneRegex.MatchString(text) {
		return true
	}
	if ps.enabledTypes[PIIIDCard] && idCardRegex.MatchString(text) {
		return true
	}
	if ps.enabledTypes[PIIEmail] && emailRegex.MatchString(text) {
		return true
	}
	if ps.enabledTypes[PIIBankCard] && bankCardRegex.MatchString(text) {
		return true
	}
	return false
}

// maskPhone 手机号脱敏: 138****1234
func maskPhone(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

// maskIDCard 身份证脱敏: 110***********1234
func maskIDCard(id string) string {
	if len(id) < 7 {
		return id
	}
	return id[:3] + strings.Repeat("*", len(id)-7) + id[len(id)-4:]
}

// maskBankCard 银行卡脱敏: 6222****1234
func maskBankCard(card string) string {
	if len(card) < 8 {
		return card
	}
	return card[:4] + strings.Repeat("*", len(card)-8) + card[len(card)-4:]
}

// maskEmail 邮箱脱敏: u***@example.com
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	user := parts[0]
	if len(user) <= 1 {
		return user + "***@" + parts[1]
	}
	return string(user[0]) + "***@" + parts[1]
}
