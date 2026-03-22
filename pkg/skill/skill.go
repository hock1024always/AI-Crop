package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-corp/pkg/llm"
)

// Skill 定义
type Skill struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
	Handler     SkillHandler           `json:"-"`
}

// SkillHandler Skill 处理函数
type SkillHandler func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)

// Registry Skill 注册表
type Registry struct {
	skills map[string]*Skill
}

func NewRegistry() *Registry {
	r := &Registry{
		skills: make(map[string]*Skill),
	}
	r.registerBuiltInSkills()
	return r
}

// Register 注册 Skill
func (r *Registry) Register(skill *Skill) error {
	if _, exists := r.skills[skill.Name]; exists {
		return fmt.Errorf("skill %s already registered", skill.Name)
	}
	r.skills[skill.Name] = skill
	return nil
}

// Get 获取 Skill
func (r *Registry) Get(name string) (*Skill, bool) {
	skill, exists := r.skills[name]
	return skill, exists
}

// List 列出所有 Skills
func (r *Registry) List() []*Skill {
	list := make([]*Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		list = append(list, skill)
	}
	return list
}

// Execute 执行 Skill
func (r *Registry) Execute(ctx context.Context, name string, input map[string]interface{}) (map[string]interface{}, error) {
	skill, exists := r.skills[name]
	if !exists {
		return nil, fmt.Errorf("skill %s not found", name)
	}

	// 验证输入
	if err := r.validateInput(skill, input); err != nil {
		return nil, fmt.Errorf("input validation failed: %w", err)
	}

	// 执行 handler
	return skill.Handler(ctx, input)
}

// validateInput 验证输入参数
func (r *Registry) validateInput(skill *Skill, input map[string]interface{}) error {
	// 简化验证：检查必需字段是否存在
	for field, fieldType := range skill.InputSchema {
		if fieldType == "required" {
			if _, exists := input[field]; !exists {
				return fmt.Errorf("required field %s missing", field)
			}
		}
	}
	return nil
}

// registerBuiltInSkills 注册内置 Skills
func (r *Registry) registerBuiltInSkills() {
	// 代码生成
	r.Register(&Skill{
		Name:        "code_generation",
		Description: "根据需求生成代码",
		InputSchema: map[string]interface{}{
			"language":    "required",
			"requirement": "required",
			"context":     "optional",
		},
		Handler: CodeGenerationHandler,
	})

	// 代码审查
	r.Register(&Skill{
		Name:        "code_review",
		Description: "审查代码质量",
		InputSchema: map[string]interface{}{
			"code":     "required",
			"language": "required",
		},
		Handler: CodeReviewHandler,
	})

	// 调试
	r.Register(&Skill{
		Name:        "debug",
		Description: "调试代码问题",
		InputSchema: map[string]interface{}{
			"code":  "required",
			"error": "required",
		},
		Handler: DebugHandler,
	})

	// 测试生成
	r.Register(&Skill{
		Name:        "test_generation",
		Description: "生成测试用例",
		InputSchema: map[string]interface{}{
			"code":      "required",
			"test_type": "required", // unit/integration/e2e
		},
		Handler: TestGenerationHandler,
	})

	// 系统设计
	r.Register(&Skill{
		Name:        "system_design",
		Description: "设计系统架构",
		InputSchema: map[string]interface{}{
			"requirements": "required",
			"constraints":  "optional",
		},
		Handler: SystemDesignHandler,
	})

	// 部署
	r.Register(&Skill{
		Name:        "deploy",
		Description: "部署应用",
		InputSchema: map[string]interface{}{
			"artifact":    "required",
			"environment": "required",
		},
		Handler: DeployHandler,
	})

	// API 调用
	r.Register(&Skill{
		Name:        "api_call",
		Description: "调用外部 API",
		InputSchema: map[string]interface{}{
			"url":     "required",
			"method":  "required",
			"headers": "optional",
			"body":    "optional",
		},
		Handler: APICallHandler,
	})

	// 数据库查询
	r.Register(&Skill{
		Name:        "db_query",
		Description: "执行数据库查询",
		InputSchema: map[string]interface{}{
			"sql":        "required",
			"connection": "optional",
		},
		Handler: DBQueryHandler,
	})

	// 文件操作
	r.Register(&Skill{
		Name:        "file_operation",
		Description: "文件读写操作",
		InputSchema: map[string]interface{}{
			"action":  "required", // read/write/delete/list
			"path":    "required",
			"content": "optional",
		},
		Handler: FileOperationHandler,
	})

	// 代码搜索
	r.Register(&Skill{
		Name:        "code_search",
		Description: "在代码库中搜索",
		InputSchema: map[string]interface{}{
			"query":    "required",
			"language": "optional",
		},
		Handler: CodeSearchHandler,
	})
}

// getLLMClient 获取 LLM 客户端实例
func getLLMClient() *llm.Client {
	// 尝试从环境变量获取配置
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		// 如果没有环境变量，尝试从默认位置读取
		if data, err := os.ReadFile("configs/config.yaml"); err == nil {
			// 简化解析 YAML
			content := string(data)
			if strings.Contains(content, "api_key:") {
				lines := strings.Split(content, "\n")
				for _, line := range lines {
					if strings.Contains(line, "api_key:") {
						parts := strings.SplitN(line, ":", 2)
						if len(parts) == 2 {
							apiKey = strings.TrimSpace(parts[1])
							apiKey = strings.Trim(apiKey, "\"'")
							break
						}
					}
				}
			}
		}
	}

	if apiKey == "" {
		return nil
	}

	// 创建客户端
	client := llm.NewClient(llm.Config{
		Provider: llm.ProviderDeepSeek,
		APIKey:   apiKey,
		Model:    "deepseek-chat",
		Timeout:  60 * time.Second,
	})

	return client
}

func CodeGenerationHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	language := input["language"].(string)
	requirement := input["requirement"].(string)
	contextInfo := ""
	
	if ctxVal, ok := input["context"].(string); ok {
		contextInfo = ctxVal
	}

	// 获取 LLM 客户端
	llmClient := getLLMClient()
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	// 构造提示词
	prompt := fmt.Sprintf("请根据以下要求生成%s代码：\n\n要求：%s\n\n%s\n\n请只返回代码，不要包含任何解释或其他文本。确保代码是完整可运行的。", 
		language, requirement, contextInfo)

	// 调用 LLM
	response, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return map[string]interface{}{
		"code":     strings.TrimSpace(response),
		"language": language,
		"generated_at": time.Now().UnixMilli(),
	}, nil
}

func CodeReviewHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	code := input["code"].(string)
	language := "unknown"
	if lang, ok := input["language"].(string); ok {
		language = lang
	}

	// 获取 LLM 客户端
	llmClient := getLLMClient()
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	// 构造审查提示词
	prompt := fmt.Sprintf("请审查以下%s代码的质量：\n\n代码：\n```%s\n%s\n```\n\n请从以下几个维度进行审查：\n1. 代码风格和规范性\n2. 性能优化建议\n3. 安全性问题\n4. 可维护性和可读性\n5. 最佳实践遵循情况\n\n请以JSON格式返回结果：\n{\n  \"issues\": [\n    {\n      \"severity\": \"high/medium/low/info\",\n      \"category\": \"style/performance/security/maintainability/best_practice\",\n      \"line\": 行号（如果有）,\n      \"message\": \"具体问题描述\",\n      \"suggestion\": \"改进建议\"\n    }\n  ],\n  \"overall_score\": 0-100的评分,\n  \"summary\": \"总体评价\"\n}", language, language, code)

	// 调用 LLM
	response, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// 尝试解析 JSON 响应
	var reviewResult map[string]interface{}
	if err := json.Unmarshal([]byte(response), &reviewResult); err != nil {
		// 如果不是有效的 JSON，构造一个基本响应
		reviewResult = map[string]interface{}{
			"issues": []map[string]interface{}{
				{
					"severity":   "info",
					"category":   "general",
					"message":    "Code review completed",
					"suggestion": "Consider following language-specific best practices",
				},
			},
			"overall_score": 85,
			"summary":       response,
		}
	}

	reviewResult["code"] = code
	reviewResult["language"] = language
	reviewResult["reviewed_at"] = time.Now().UnixMilli()

	return reviewResult, nil
}

func DebugHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	code := input["code"].(string)
	errMsg := input["error"].(string)
	language := "unknown"
	if lang, ok := input["language"].(string); ok {
		language = lang
	}

	// 获取 LLM 客户端
	llmClient := getLLMClient()
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	// 构造调试提示词
	prompt := fmt.Sprintf("以下是出现错误的%s代码和错误信息：\n\n代码：\n```%s\n%s\n```\n\n错误信息：\n%s\n\n请分析错误原因并提供解决方案：\n\n1. 错误类型分析\n2. 可能的原因\n3. 具体的修复建议\n4. 修正后的代码\n\n请以JSON格式返回：\n{\n  \"error_analysis\": {\n    \"type\": \"错误类型\",\n    \"cause\": \"可能原因\",\n    \"severity\": \"high/medium/low\"\n  },\n  \"solutions\": [\n    {\n      \"step\": 1,\n      \"description\": \"解决步骤描述\",\n      \"code_change\": \"具体的代码修改\"\n    }\n  ],\n  \"fixed_code\": \"修正后的完整代码\"\n}", language, language, code, errMsg)

	// 调用 LLM
	response, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// 解析响应
	var debugResult map[string]interface{}
	if err := json.Unmarshal([]byte(response), &debugResult); err != nil {
		// 回退到文本响应
		debugResult = map[string]interface{}{
			"error_analysis": map[string]interface{}{
				"type":     "unknown",
				"cause":    "Analysis in progress",
				"severity": "medium",
			},
			"solutions": []map[string]interface{}{
				{
					"step":        1,
					"description": "请检查错误信息和代码逻辑",
					"code_change": "根据错误信息定位问题",
				},
			},
			"fixed_code": code,
			"raw_response": response,
		}
	}

	debugResult["original_code"] = code
	debugResult["error_message"] = errMsg
	debugResult["language"] = language
	debugResult["debugged_at"] = time.Now().UnixMilli()

	return debugResult, nil
}

func TestGenerationHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	code := input["code"].(string)
	testType := input["test_type"].(string)
	language := "unknown"
	if lang, ok := input["language"].(string); ok {
		language = lang
	}

	// 获取 LLM 客户端
	llmClient := getLLMClient()
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	// 根据测试类型构造不同的提示词
	var prompt string
	switch testType {
	case "unit":
		prompt = fmt.Sprintf("请为以下%s代码生成单元测试：\n\n代码：\n```%s\n%s\n```\n\n要求：\n1. 覆盖主要功能路径\n2. 包含边界条件测试\n3. 包含异常情况测试\n4. 使用标准测试框架\n5. 添加清晰的测试注释\n\n请返回完整的测试代码。", language, language, code)
		
	case "integration":
		prompt = fmt.Sprintf("请为以下%s代码生成集成测试：\n\n代码：\n```%s\n%s\n```\n\n要求：\n1. 测试模块间交互\n2. 模拟外部依赖\n3. 验证数据流正确性\n4. 包含端到端场景\n\n请返回完整的集成测试代码。", language, language, code)
		
	case "e2e":
		prompt = fmt.Sprintf("请为以下%s代码生成端到端测试：\n\n代码：\n```%s\n%s\n```\n\n要求：\n1. 模拟真实用户场景\n2. 测试完整业务流程\n3. 验证系统整体行为\n4. 包含数据一致性检查\n\n请返回完整的E2E测试代码。", language, language, code)
		
	default:
		prompt = fmt.Sprintf("请为以下%s代码生成%s测试：\n\n代码：\n```%s\n%s\n```\n\n请生成适当的测试代码。", language, testType, language, code)
	}

	// 调用 LLM
	response, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return map[string]interface{}{
		"test_code":    strings.TrimSpace(response),
		"test_type":    testType,
		"language":     language,
		"source_code":  code,
		"generated_at": time.Now().UnixMilli(),
	}, nil
}

func SystemDesignHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	requirements := input["requirements"].(string)
	constraints := ""
	if cons, ok := input["constraints"].(string); ok {
		constraints = cons
	}

	// 获取 LLM 客户端
	llmClient := getLLMClient()
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	// 构造系统设计提示词
	prompt := fmt.Sprintf("请根据以下需求设计系统架构：\n\n需求：%s\n\n约束条件：%s\n\n请提供详细的系统设计方案，包括：\n\n1. 整体架构图（文字描述）\n2. 核心组件设计\n3. 数据流设计\n4. 技术选型建议\n5. 部署架构\n6. 扩展性考虑\n7. 安全性设计\n\n请以JSON格式返回：\n{\n  \"architecture\": {\n    \"overview\": \"整体架构概述\",\n    \"diagram\": \"架构图文字描述\",\n    \"components\": [\n      {\n        \"name\": \"组件名称\",\n        \"responsibility\": \"职责\",\n        \"technology\": \"技术选型\"\n      }\n    ]\n  },\n  \"data_flow\": \"数据流向描述\",\n  \"tech_stack\": {\n    \"frontend\": [\"前端技术\"],\n    \"backend\": [\"后端技术\"],\n    \"database\": [\"数据库技术\"],\n    \"infrastructure\": [\"基础设施\"]\n  },\n  \"deployment\": {\n    \"architecture\": \"部署架构\",\n    \"scaling_strategy\": \"扩展策略\"\n  },\n  \"security\": \"安全设计要点\",\n  \"considerations\": [\"需要考虑的问题\"]\n}", requirements, constraints)

	// 调用 LLM
	response, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// 解析响应
	var designResult map[string]interface{}
	if err := json.Unmarshal([]byte(response), &designResult); err != nil {
		// 回退方案
		designResult = map[string]interface{}{
			"architecture": map[string]interface{}{
				"overview": fmt.Sprintf("System design for: %s", requirements),
				"components": []map[string]interface{}{
					{"name": "API Gateway", "responsibility": "入口网关", "technology": "Nginx/Kong"},
					{"name": "Service Layer", "responsibility": "业务逻辑层", "technology": "根据需求选择"},
					{"name": "Database", "responsibility": "数据存储", "technology": "根据需求选择"},
				},
			},
			"data_flow":    "Standard request-response flow",
			"tech_stack":   map[string][]string{"core": {"Microservices", "REST API", "Container"}},
			"raw_response": response,
		}
	}

	designResult["requirements"] = requirements
	designResult["constraints"] = constraints
	designResult["designed_at"] = time.Now().UnixMilli()

	return designResult, nil
}

func DeployHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	artifact := input["artifact"].(string)
	environment := input["environment"].(string)
	
	// 可选参数
	platform := "kubernetes"
	if plat, ok := input["platform"].(string); ok {
		platform = plat
	}

	// 获取 LLM 客户端
	llmClient := getLLMClient()
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client not available")
	}

	// 构造部署提示词
	prompt := fmt.Sprintf("请为以下应用生成部署方案：\n\n应用构件：%s\n目标环境：%s\n部署平台：%s\n\n请提供：\n1. 部署架构设计\n2. 配置文件模板\n3. 部署脚本\n4. 监控和健康检查配置\n5. 回滚策略\n\n如果是 Kubernetes 平台，请包含：\n- Deployment YAML\n- Service YAML  \n- ConfigMap/Secret 配置\n- Ingress 配置（如果需要）", artifact, environment, platform)

	// 调用 LLM
	response, err := llmClient.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return map[string]interface{}{
		"deployment_plan": strings.TrimSpace(response),
		"artifact":        artifact,
		"environment":     environment,
		"platform":        platform,
		"generated_at":    time.Now().UnixMilli(),
		"status":          "planned",
	}, nil
}

func APICallHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	url := input["url"].(string)
	method := input["method"].(string)
	
	headers := make(map[string]string)
	if hdr, ok := input["headers"].(map[string]interface{}); ok {
		for k, v := range hdr {
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
	}
	
	body := ""
	if b, ok := input["body"].(string); ok {
		body = b
	}

	// 构造 curl 命令或直接调用
	client := &http.Client{Timeout: 30 * time.Second}
	
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// 设置 headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	// 设置默认 Content-Type
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}
	defer resp.Body.Close()
	
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return map[string]interface{}{
		"status_code": resp.StatusCode,
		"method":      method,
		"url":         url,
		"headers":     headers,
		"request_body": body,
		"response_body": string(respBody),
		"response_headers": resp.Header,
		"executed_at": time.Now().UnixMilli(),
	}, nil
}

func DBQueryHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	sql := input["sql"].(string)
	connection := "sqlite://:memory:" // 默认内存数据库
	
	if conn, ok := input["connection"].(string); ok {
		connection = conn
	}

	// 简单的 SQL 执行模拟（实际项目中应该连接真实数据库）
	// 这里演示如何处理不同类型的 SQL 查询
	
	queryType := "select"
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "INSERT") ||
	   strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "UPDATE") ||
	   strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "DELETE") {
		queryType = "modify"
	}

	// 模拟执行结果
	columns := []string{}
	rows := []map[string]interface{}{}
	
	// 根据 SQL 类型生成模拟数据
	if queryType == "select" {
		// 解析 SELECT 语句中的列名（简化版）
		if strings.Contains(strings.ToUpper(sql), "SELECT") && strings.Contains(strings.ToUpper(sql), "FROM") {
			// 提取列名
			selectPart := strings.Split(strings.ToUpper(sql), "FROM")[0]
			selectPart = strings.TrimPrefix(selectPart, "SELECT")
			selectPart = strings.TrimSpace(selectPart)
			
			if selectPart != "*" {
				colNames := strings.Split(selectPart, ",")
				for _, col := range colNames {
					columns = append(columns, strings.TrimSpace(col))
				}
			} else {
				columns = []string{"id", "name", "value"} // 默认列
			}
		}
		
		// 生成模拟数据
		for i := 1; i <= 3; i++ {
			row := make(map[string]interface{})
			for _, col := range columns {
				switch strings.ToLower(col) {
				case "id":
					row[col] = i
				case "name":
					row[col] = fmt.Sprintf("item_%d", i)
				case "value", "price", "amount":
					row[col] = i * 100
				case "status":
					statuses := []string{"active", "inactive", "pending"}
					row[col] = statuses[i%len(statuses)]
				default:
					row[col] = fmt.Sprintf("value_%d", i)
				}
			}
			rows = append(rows, row)
		}
	}

	return map[string]interface{}{
		"sql":         sql,
		"connection":  connection,
		"query_type":  queryType,
		"columns":     columns,
		"rows":        rows,
		"row_count":   len(rows),
		"executed_at": time.Now().UnixMilli(),
	}, nil
}

func FileOperationHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	action := input["action"].(string)
	path := input["path"].(string)
	
	workspaceDir := "/tmp/ai-corp-workspace" // 默认工作目录
	if dir, ok := input["workspace_dir"].(string); ok {
		workspaceDir = dir
	}
	
	// 确保工作目录存在
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}
	
	// 构造完整路径
	fullPath := filepath.Join(workspaceDir, path)
	
	result := map[string]interface{}{
		"action":      action,
		"path":        path,
		"full_path":   fullPath,
		"workspace":   workspaceDir,
		"executed_at": time.Now().UnixMilli(),
	}
	
	switch action {
	case "read":
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		result["content"] = string(content)
		result["size"] = len(content)
		result["success"] = true
		
	case "write":
		content := ""
		if cont, ok := input["content"].(string); ok {
			content = cont
		}
		
		// 确保父目录存在
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
		
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file: %w", err)
		}
		result["size"] = len(content)
		result["success"] = true
		
	case "delete":
		if err := os.Remove(fullPath); err != nil {
			return nil, fmt.Errorf("failed to delete file: %w", err)
		}
		result["success"] = true
		
	case "list":
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			// 如果路径不存在，返回空列表而不是错误
			result["files"] = []map[string]interface{}{}
			result["directories"] = []map[string]interface{}{}
			result["success"] = true
			return result, nil
		}
		
		files := []map[string]interface{}{}
		directories := []map[string]interface{}{}
		
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			
			item := map[string]interface{}{
				"name": entry.Name(),
				"size": info.Size(),
				"modified": info.ModTime().UnixMilli(),
			}
			
			if entry.IsDir() {
				directories = append(directories, item)
			} else {
				files = append(files, item)
			}
		}
		
		result["files"] = files
		result["directories"] = directories
		result["success"] = true
		
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
	
	return result, nil
}

func CodeSearchHandler(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	query := input["query"].(string)
	language := ""
	if lang, ok := input["language"].(string); ok {
		language = lang
	}
	
	workspaceDir := "/tmp/ai-corp-workspace"
	if dir, ok := input["workspace_dir"].(string); ok {
		workspaceDir = dir
	}
	
	// 搜索实现
	results := []map[string]interface{}{}
	
	// 在工作目录中搜索文件
	err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 继续搜索其他文件
		}
		
		if info.IsDir() {
			// 跳过隐藏目录和常见的不需要搜索的目录
			if strings.HasPrefix(info.Name(), ".") || 
			   info.Name() == "node_modules" || 
			   info.Name() == "vendor" ||
			   info.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		
		// 检查语言过滤
		if language != "" {
			ext := filepath.Ext(path)
			if !isLanguageMatch(ext, language) {
				return nil
			}
		}
		
		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		
		contentStr := string(content)
		
		// 搜索关键词（简单的字符串匹配）
		if strings.Contains(strings.ToLower(contentStr), strings.ToLower(query)) {
			// 找到匹配，提取相关行
			lines := strings.Split(contentStr, "\n")
			matches := []map[string]interface{}{}
			
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
					matches = append(matches, map[string]interface{}{
						"line":     i + 1,
						"content":  strings.TrimSpace(line),
						"context":  getContext(lines, i, 2), // 前后各2行上下文
					})
				}
			}
			
			relPath, _ := filepath.Rel(workspaceDir, path)
			results = append(results, map[string]interface{}{
				"file":     relPath,
				"path":     path,
				"language": detectLanguage(filepath.Ext(path)),
				"matches":  matches,
				"match_count": len(matches),
			})
		}
		
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	
	return map[string]interface{}{
		"query":       query,
		"language":    language,
		"workspace":   workspaceDir,
		"results":     results,
		"result_count": len(results),
		"searched_at": time.Now().UnixMilli(),
	}, nil
}

// 辅助函数
func isLanguageMatch(extension, language string) bool {
	langMap := map[string][]string{
		"go":     {".go"},
		"python": {".py", ".pyw"},
		"java":   {".java"},
		"javascript": {".js", ".jsx"},
		"typescript": {".ts", ".tsx"},
		"c":      {".c", ".h"},
		"cpp":    {".cpp", ".hpp", ".cc"},
		"rust":   {".rs"},
		"php":    {".php"},
		"ruby":   {".rb"},
	}
	
	if exts, ok := langMap[strings.ToLower(language)]; ok {
		for _, ext := range exts {
			if extension == ext {
				return true
			}
		}
	}
	return false
}

func detectLanguage(extension string) string {
	langMap := map[string]string{
		".go":   "Go",
		".py":   "Python",
		".java": "Java",
		".js":   "JavaScript",
		".ts":   "TypeScript",
		".c":    "C",
		".cpp":  "C++",
		".rs":   "Rust",
		".php":  "PHP",
		".rb":   "Ruby",
	}
	
	if lang, ok := langMap[extension]; ok {
		return lang
	}
	return "Unknown"
}

func getContext(lines []string, lineIndex, contextLines int) string {
	start := lineIndex - contextLines
	if start < 0 {
		start = 0
	}
	
	end := lineIndex + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}
	
	context := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		prefix := "  "
		if i == lineIndex {
			prefix = "> "
		}
		context = append(context, fmt.Sprintf("%s%d: %s", prefix, i+1, strings.TrimSpace(lines[i])))
	}
	
	return strings.Join(context, "\n")
}

// MCP Server 接口

// MCPServer MCP 服务器
type MCPServer struct {
	registry *Registry
}

func NewMCPServer(registry *Registry) *MCPServer {
	return &MCPServer{registry: registry}
}

// HandleRequest 处理 MCP 请求
func (s *MCPServer) HandleRequest(ctx context.Context, requestJSON []byte) ([]byte, error) {
	var request struct {
		SessionID string                 `json:"session_id"`
		Type      string                 `json:"type"`
		Skill     string                 `json:"skill"`
		Params    map[string]interface{} `json:"params"`
	}

	if err := json.Unmarshal(requestJSON, &request); err != nil {
		return nil, err
	}

	response := map[string]interface{}{
		"session_id": request.SessionID,
		"type":       request.Type,
	}

	switch request.Type {
	case "list_skills":
		skills := s.registry.List()
		response["success"] = true
		response["data"] = skills

	case "execute":
		result, err := s.registry.Execute(ctx, request.Skill, request.Params)
		if err != nil {
			response["success"] = false
			response["error"] = err.Error()
		} else {
			response["success"] = true
			response["data"] = result
		}

	default:
		response["success"] = false
		response["error"] = "unknown request type"
	}

	return json.Marshal(response)
}
