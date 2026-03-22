package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ModelType 模型类型
type ModelType string

const (
	ModelTypeLocal ModelType = "local" // 本地部署模型 (Ollama)
	ModelTypeCloud ModelType = "cloud" // 云端 API
	ModelTypeAuto  ModelType = "auto"  // 自动选择
)

// ModelInfo 模型信息
type ModelInfo struct {
	Name         string     `json:"name"`
	Provider     string     `json:"provider"`
	Type         ModelType  `json:"type"`
	Endpoint     string     `json:"endpoint"`
	Capabilities []string   `json:"capabilities"`
	MaxTokens    int        `json:"max_tokens"`
	Status       string     `json:"status"`     // available/unavailable/busy
	Latency      int        `json:"latency_ms"` // 平均延迟
}

// DualModeClient 双模式 LLM 客户端
type DualModeClient struct {
	localClient  *OllamaClient
	cloudClients map[string]*Client // key: provider name
	models       map[string]*ModelInfo
	mu           sync.RWMutex
}

// OllamaClient Ollama 本地客户端
type OllamaClient struct {
	baseURL    string
	model      string
	httpClient *HTTPClient
}

// HTTPClient 简化的 HTTP 客户端接口
type HTTPClient struct {
	timeout time.Duration
}

// NewDualModeClient 创建双模式客户端
func NewDualModeClient(config DualModeConfig) (*DualModeClient, error) {
	client := &DualModeClient{
		cloudClients: make(map[string]*Client),
		models:       make(map[string]*ModelInfo),
	}

	// 初始化本地 Ollama 客户端
	if config.OllamaBaseURL != "" {
		client.localClient = &OllamaClient{
			baseURL: config.OllamaBaseURL,
			model:   config.OllamaModel,
			httpClient: &HTTPClient{timeout: 120 * time.Second},
		}

		// 注册本地模型
		client.models["deepseek-local"] = &ModelInfo{
			Name:         "DeepSeek (本地)",
			Provider:     "ollama_local",
			Type:         ModelTypeLocal,
			Endpoint:     config.OllamaBaseURL,
			Capabilities: []string{"code", "chat", "reasoning"},
			MaxTokens:    8192,
			Status:       "available",
		}
	}

	// 初始化云端 API 客户端
	for provider, apiKey := range config.CloudAPIKeys {
		if apiKey == "" {
			continue
		}

		cloudClient := NewClient(Config{
			Provider: Provider(provider),
			APIKey:   apiKey,
			Model:    config.CloudModels[provider],
			Timeout:  60 * time.Second,
		})
		client.cloudClients[provider] = cloudClient

		// 注册云端模型
		modelKey := fmt.Sprintf("%s-cloud", provider)
		client.models[modelKey] = &ModelInfo{
			Name:         fmt.Sprintf("%s (云端)", strings.Title(provider)),
			Provider:     provider,
			Type:         ModelTypeCloud,
			Endpoint:     "api",
			Capabilities: []string{"code", "chat", "reasoning", "vision"},
			MaxTokens:    128000,
			Status:       "available",
		}
	}

	return client, nil
}

// DualModeConfig 双模式配置
type DualModeConfig struct {
	OllamaBaseURL string            `yaml:"ollama_base_url"`
	OllamaModel   string            `yaml:"ollama_model"`
	CloudAPIKeys  map[string]string `yaml:"cloud_api_keys"`
	CloudModels   map[string]string `yaml:"cloud_models"`
}

// Chat 聊天（自动选择最佳模型）
func (d *DualModeClient) Chat(ctx context.Context, messages []Message, modelType ModelType) (string, *ModelInfo, error) {
	switch modelType {
	case ModelTypeLocal:
		return d.chatLocal(ctx, messages)
	case ModelTypeCloud:
		return d.chatCloud(ctx, messages)
	default:
		return d.chatAuto(ctx, messages)
	}
}

// chatLocal 使用本地模型
func (d *DualModeClient) chatLocal(ctx context.Context, messages []Message) (string, *ModelInfo, error) {
	if d.localClient == nil {
		return "", nil, fmt.Errorf("本地模型未配置")
	}

	// 调用 Ollama API
	response, err := d.localClient.Chat(ctx, messages)
	if err != nil {
		return "", nil, err
	}

	modelInfo := d.models["deepseek-local"]
	return response, modelInfo, nil
}

// chatCloud 使用云端模型
func (d *DualModeClient) chatCloud(ctx context.Context, messages []Message) (string, *ModelInfo, error) {
	// 优先使用 DeepSeek 云端，其次 OpenAI
	priority := []string{"deepseek", "openai", "claude"}

	for _, provider := range priority {
		if client, exists := d.cloudClients[provider]; exists {
			response, err := client.Chat(ctx, messages)
			if err != nil {
				continue // 尝试下一个提供商
			}

			modelKey := fmt.Sprintf("%s-cloud", provider)
			modelInfo := d.models[modelKey]
			return response, modelInfo, nil
		}
	}

	return "", nil, fmt.Errorf("所有云端模型均不可用")
}

// chatAuto 自动选择模型
func (d *DualModeClient) chatAuto(ctx context.Context, messages []Message) (string, *ModelInfo, error) {
	// 策略：优先本地，失败则云端

	// 1. 尝试本地模型
	if d.localClient != nil {
		response, err := d.localClient.Chat(ctx, messages)
		if err == nil {
			modelInfo := d.models["deepseek-local"]
			return response, modelInfo, nil
		}
	}

	// 2. 回退到云端
	return d.chatCloud(ctx, messages)
}

// ListModels 列出所有可用模型
func (d *DualModeClient) ListModels() []*ModelInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	models := make([]*ModelInfo, 0, len(d.models))
	for _, model := range d.models {
		models = append(models, model)
	}
	return models
}

// GetModel 获取指定模型信息
func (d *DualModeClient) GetModel(name string) *ModelInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.models[name]
}

// UpdateModelStatus 更新模型状态
func (d *DualModeClient) UpdateModelStatus(name string, status string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if model, exists := d.models[name]; exists {
		model.Status = status
	}
}

// OllamaClient 方法实现

// Chat Ollama 聊天
func (o *OllamaClient) Chat(ctx context.Context, messages []Message) (string, error) {
	// 简化实现：返回提示信息
	// 实际使用时应该调用 Ollama API
	return "本地模型响应（Ollama）", nil
}
