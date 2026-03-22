package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Provider LLM 提供商
type Provider string

const (
	ProviderDeepSeek Provider = "deepseek"
	ProviderOpenAI   Provider = "openai"
	ProviderClaude   Provider = "claude"
)

// Config LLM 配置
type Config struct {
	Provider Provider
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
}

// Client LLM 客户端
type Client struct {
	config Config
	client *http.Client
}

// Message LLM 消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request LLM 请求
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
}

// Response LLM 响应
type Response struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewClient 创建 LLM 客户端
func NewClient(config Config) *Client {
	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}

	// 设置默认 BaseURL
	if config.BaseURL == "" {
		switch config.Provider {
		case ProviderDeepSeek:
			config.BaseURL = "https://api.deepseek.com/v1"
		case ProviderOpenAI:
			config.BaseURL = "https://api.openai.com/v1"
		case ProviderClaude:
			config.BaseURL = "https://api.anthropic.com/v1"
		}
	}

	// 设置默认模型
	if config.Model == "" {
		switch config.Provider {
		case ProviderDeepSeek:
			config.Model = "deepseek-chat"
		case ProviderOpenAI:
			config.Model = "gpt-4"
		case ProviderClaude:
			config.Model = "claude-3-opus-20240229"
		}
	}

	return &Client{
		config: config,
		client: &http.Client{Timeout: config.Timeout},
	}
}

// Chat 发送聊天请求
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	req := Request{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.config.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var llmResp Response
	if err := json.Unmarshal(body, &llmResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	return llmResp.Choices[0].Message.Content, nil
}

// ChatWithSystem 带系统提示的聊天
func (c *Client) ChatWithSystem(ctx context.Context, systemPrompt string, userMessage string) (string, error) {
	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMessage},
	}
	return c.Chat(ctx, messages)
}

// AgentSystemPrompt 返回 Agent 的系统提示
func AgentSystemPrompt(agentType string) string {
	prompts := map[string]string{
		"developer": `你是一个专业的软件开发工程师。你的职责是：
1. 根据需求编写高质量、可维护的代码
2. 遵循最佳实践和设计模式
3. 编写清晰的注释和文档
4. 考虑代码的性能和安全性

请用简洁、专业的方式回答问题，并提供可运行的代码示例。`,

		"tester": `你是一个专业的测试工程师。你的职责是：
1. 设计全面的测试用例
2. 执行自动化测试
3. 发现和报告 bug
4. 验证修复效果

请关注边界条件、异常情况和性能测试。`,

		"architect": `你是一个系统架构师。你的职责是：
1. 设计系统架构
2. 选择合适的技术栈
3. 考虑可扩展性、可用性、性能
4. 评审技术方案

请提供清晰的架构图和设计文档。`,

		"devops": `你是一个运维工程师。你的职责是：
1. 部署和配置系统
2. 监控系统状态
3. 排查和解决问题
4. 优化系统性能

请提供详细的操作步骤和脚本。`,

		"product_manager": `你是一个产品经理。你的职责是：
1. 分析用户需求
2. 编写产品文档
3. 规划产品路线图
4. 协调开发进度

请关注用户价值和商业目标。`,
	}

	if prompt, ok := prompts[agentType]; ok {
		return prompt
	}
	return "你是一个 AI 助手，请帮助用户解决问题。"
}

// TaskDecomposePrompt 任务分解提示
func TaskDecomposePrompt() string {
	return `你是一个任务分解专家。请将用户的请求分解为具体的子任务。

输出格式（JSON）：
{
  "main_task": "主任务描述",
  "sub_tasks": [
    {
      "step": 1,
      "agent_type": "developer|tester|architect|devops",
      "action": "具体操作",
      "dependencies": []
    }
  ],
  "estimated_time": "预计完成时间"
}

请只输出 JSON，不要有其他内容。`
}
