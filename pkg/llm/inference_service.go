package llm

import (
	"context"
	"fmt"
	"time"

	"ai-corp/pkg/database"
	"ai-corp/pkg/metrics"
)

// InferenceService wraps the LLM client with database-backed metrics recording,
// cache lookup, and audit logging. It is the primary interface for all LLM calls
// in the orchestrator.
type InferenceService struct {
	client *Client
	db     *database.DB
}

// NewInferenceService creates an InferenceService.
// db may be nil; if so, metrics recording is skipped.
func NewInferenceService(client *Client, db *database.DB) *InferenceService {
	return &InferenceService{client: client, db: db}
}

// ChatResult holds the LLM response along with usage metadata.
type ChatResult struct {
	Content          string  `json:"content"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	LatencyMs        int     `json:"latency_ms"`
	TTFTMs           int     `json:"ttft_ms"`
	TPS              float64 `json:"tps"`
	CacheHit         bool    `json:"cache_hit"`
}

// Chat sends messages to the LLM and records metrics in the database.
func (s *InferenceService) Chat(ctx context.Context, messages []Message, agentID *string) (*ChatResult, error) {
	if s.client == nil {
		return nil, fmt.Errorf("LLM client not configured")
	}

	start := time.Now()

	// Build request
	req := Request{
		Model:    s.client.config.Model,
		Messages: messages,
		Stream:   false,
	}

	// Call LLM API and capture full response
	resp, err := s.client.ChatFull(ctx, req)
	latencyMs := int(time.Since(start).Milliseconds())

	result := &ChatResult{
		Model:    s.client.config.Model,
		Provider: string(s.client.config.Provider),
	}

	if err != nil {
		// Record error metric (DB + Prometheus)
		s.recordMetric(ctx, agentID, result.Model, 0, 0, 0, latencyMs, 0, 0, false, "error", errCode(err))
		metrics.RecordInference(result.Model, result.Provider, "error",
			float64(latencyMs)/1000.0, 0, 0, 0, 0, false)
		return nil, err
	}

	result.Content = resp.Choices[0].Message.Content
	result.PromptTokens = resp.Usage.PromptTokens
	result.CompletionTokens = resp.Usage.CompletionTokens
	result.TotalTokens = resp.Usage.TotalTokens
	result.LatencyMs = latencyMs

	if resp.Usage.CompletionTokens > 0 && latencyMs > 0 {
		result.TPS = float64(resp.Usage.CompletionTokens) / (float64(latencyMs) / 1000.0)
	}

	// Record success metric (DB + Prometheus)
	s.recordMetric(ctx, agentID, result.Model,
		result.PromptTokens, result.CompletionTokens, result.TotalTokens,
		latencyMs, 0, result.TPS, false, "success", nil)
	metrics.RecordInference(result.Model, result.Provider, "success",
		float64(latencyMs)/1000.0, 0, result.TPS,
		result.PromptTokens, result.CompletionTokens, false)

	return result, nil
}

// ChatWithSystem is a convenience method that prepends a system message.
func (s *InferenceService) ChatWithSystem(ctx context.Context, systemPrompt, userMessage string, agentID *string) (*ChatResult, error) {
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}
	return s.Chat(ctx, messages, agentID)
}

// recordMetric writes an inference metric to the database if available.
func (s *InferenceService) recordMetric(ctx context.Context, agentID *string, model string,
	promptTokens, completionTokens, totalTokens, latencyMs, ttftMs int,
	tps float64, cacheHit bool, status string, errorCode *string) {

	if s.db == nil {
		return
	}

	m := &database.InferenceMetric{
		AgentID:          agentID,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		LatencyMs:        latencyMs,
		TTFTMs:           ttftMs,
		TPS:              tps,
		CacheHit:         cacheHit,
		Status:           status,
		ErrorCode:        errorCode,
	}

	// Non-blocking: don't fail the request if metric recording fails
	go func() {
		recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.db.Metrics.Record(recordCtx, m)
	}()
}

func errCode(err error) *string {
	if err == nil {
		return nil
	}
	s := err.Error()
	if len(s) > 64 {
		s = s[:64]
	}
	return &s
}
