package mcp

import "context"

// Tool MCP 工具定义
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
	Handler     ToolHandler            `json:"-"`
}

// ToolHandler 工具处理函数
type ToolHandler func(ctx context.Context, args map[string]interface{}) (string, error)

// ToolRegistry 工具注册表
type ToolRegistry struct {
	tools map[string]*Tool
}

// NewToolRegistry 创建工具注册表
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*Tool),
	}
}

// Register 注册工具
func (r *ToolRegistry) Register(tool *Tool) error {
	if _, exists := r.tools[tool.Name]; exists {
		return nil // 已存在，忽略
	}
	r.tools[tool.Name] = tool
	return nil
}

// RegisterTools 批量注册工具
func (r *ToolRegistry) RegisterTools(tools []Tool) error {
	for _, tool := range tools {
		t := tool // 复制
		if err := r.Register(&t); err != nil {
			return err
		}
	}
	return nil
}

// Get 获取工具
func (r *ToolRegistry) Get(name string) (*Tool, bool) {
	tool, exists := r.tools[name]
	return tool, exists
}

// List 列出所有工具
func (r *ToolRegistry) List() []*Tool {
	list := make([]*Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		list = append(list, tool)
	}
	return list
}

// Execute 执行工具
func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	tool, exists := r.tools[name]
	if !exists {
		return "", nil
	}
	return tool.Handler(ctx, args)
}
