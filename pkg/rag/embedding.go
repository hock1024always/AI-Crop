package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EmbeddingClient Embedding 客户端接口
type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// DeepSeekEmbeddingClient DeepSeek Embedding 客户端
type DeepSeekEmbeddingClient struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewDeepSeekEmbeddingClient 创建 DeepSeek Embedding 客户端
func NewDeepSeekEmbeddingClient(apiKey string) *DeepSeekEmbeddingClient {
	return &DeepSeekEmbeddingClient{
		APIKey:  apiKey,
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-embed", // DeepSeek 的 embedding 模型
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Embed 获取单个文本的向量
func (c *DeepSeekEmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("未获取到向量")
	}
	return embeddings[0], nil
}

// EmbedBatch 批量获取向量
func (c *DeepSeekEmbeddingClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Model: c.Model,
		Input: texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result embeddingResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("API 错误: %s", result.Error.Message)
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}

	return embeddings, nil
}

// MockEmbeddingClient 模拟 Embedding 客户端（用于测试）
type MockEmbeddingClient struct {
	Dimension int
}

// NewMockEmbeddingClient 创建模拟客户端
func NewMockEmbeddingClient() *MockEmbeddingClient {
	return &MockEmbeddingClient{Dimension: 384}
}

// Embed 生成模拟向量
func (c *MockEmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	// 简单的哈希模拟向量
	vector := make([]float32, c.Dimension)
	for i := range vector {
		vector[i] = float32(len(text)+i) / float32(c.Dimension)
	}
	return vector, nil
}

// EmbedBatch 批量生成模拟向量
func (c *MockEmbeddingClient) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := c.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = vec
	}
	return result, nil
}
