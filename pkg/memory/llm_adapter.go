package memory

import (
	"context"

	"ai-corp/pkg/llm"
)

// LLMClientAdapter 将 llm.Client 适配为 memory.LLMClient 接口
type LLMClientAdapter struct {
	client *llm.Client
}

// NewLLMClientAdapter 创建适配器
func NewLLMClientAdapter(client *llm.Client) *LLMClientAdapter {
	return &LLMClientAdapter{client: client}
}

// Generate 无 system prompt 的生成
func (a *LLMClientAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	return a.client.ChatWithSystem(ctx, "", prompt)
}

// GenerateWithSystem 带 system prompt 的生成
func (a *LLMClientAdapter) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	return a.client.ChatWithSystem(ctx, system, prompt)
}
