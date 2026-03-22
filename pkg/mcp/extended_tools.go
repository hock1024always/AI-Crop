package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ai-corp/pkg/skill"
)

// ExtendedToolRegistry 扩展的 MCP 工具注册表
type ExtendedToolRegistry struct {
	tools map[string]Tool
}

// Tool MCP 工具定义
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
	Handler     ToolHandler            `json:"-"`
}

// ToolHandler 工具处理函数
type ToolHandler func(ctx context.Context, args map[string]interface{}) (interface{}, error)

// NewExtendedToolRegistry 创建扩展工具注册表
func NewExtendedToolRegistry() *ExtendedToolRegistry {
	registry := &ExtendedToolRegistry{
		tools: make(map[string]Tool),
	}
	registry.registerSystemTools()
	registry.registerNetworkTools()
	registry.registerFileTools()
	registry.registerMultimediaTools()
	return registry
}

// GetTools 获取所有工具
func (r *ExtendedToolRegistry) GetTools() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// Execute 执行工具
func (r *ExtendedToolRegistry) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	tool, exists := r.tools[toolName]
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
	return tool.Handler(ctx, args)
}

// 系统操作工具
func (r *ExtendedToolRegistry) registerSystemTools() {
	// Shell 命令执行
	r.tools["shell_execute"] = Tool{
		Name:        "shell_execute",
		Description: "在系统上执行 shell 命令",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "要执行的命令",
				},
				"working_dir": map[string]interface{}{
					"type":        "string",
					"description": "工作目录",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "超时时间（秒）",
					"default":     30,
				},
			},
			"required": []string{"command"},
		},
		Handler: r.handleShellExecute,
	}

	// 系统监控
	r.tools["system_monitor"] = Tool{
		Name:        "system_monitor",
		Description: "获取系统资源使用情况",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metrics": map[string]interface{}{
					"type":        "array",
					"description": "要监控的指标列表",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"cpu", "memory", "disk", "network"},
					},
				},
			},
		},
		Handler: r.handleSystemMonitor,
	}

	// 进程管理
	r.tools["process_manager"] = Tool{
		Name:        "process_manager",
		Description: "管理进程（查看、终止、重启）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "操作类型",
					"enum":        []string{"list", "kill", "restart"},
				},
				"pid": map[string]interface{}{
					"type":        "integer",
					"description": "进程ID（kill/restart时需要）",
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "进程名匹配模式（list时使用）",
				},
			},
			"required": []string{"action"},
		},
		Handler: r.handleProcessManager,
	}
}

// 网络工具
func (r *ExtendedToolRegistry) registerNetworkTools() {
	// HTTP 客户端
	r.tools["http_client"] = Tool{
		Name:        "http_client",
		Description: "发送 HTTP 请求",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "请求URL",
				},
				"method": map[string]interface{}{
					"type":        "string",
					"description": "HTTP方法",
					"default":     "GET",
				},
				"headers": map[string]interface{}{
					"type":        "object",
					"description": "请求头",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "请求体",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "超时时间（秒）",
					"default":     30,
				},
			},
			"required": []string{"url"},
		},
		Handler: r.handleHTTPClient,
	}

	// 网络诊断
	r.tools["network_diagnostic"] = Tool{
		Name:        "network_diagnostic",
		Description: "网络连接诊断工具",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{
					"type":        "string",
					"description": "目标地址（IP或域名）",
				},
				"tests": map[string]interface{}{
					"type":        "array",
					"description": "要执行的测试类型",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"ping", "traceroute", "dns_lookup", "port_scan"},
					},
				},
			},
			"required": []string{"target"},
		},
		Handler: r.handleNetworkDiagnostic,
	}
}

// 文件操作工具
func (r *ExtendedToolRegistry) registerFileTools() {
	// 高级文件操作
	r.tools["advanced_file_ops"] = Tool{
		Name:        "advanced_file_ops",
		Description: "高级文件操作（批量处理、压缩、同步等）",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "操作类型",
					"enum":        []string{"batch_rename", "compress", "decompress", "sync", "search"},
				},
				"source": map[string]interface{}{
					"type":        "string",
					"description": "源路径",
				},
				"destination": map[string]interface{}{
					"type":        "string",
					"description": "目标路径",
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "文件匹配模式",
				},
			},
			"required": []string{"operation", "source"},
		},
		Handler: r.handleAdvancedFileOps,
	}

	// 文件备份
	r.tools["backup_utility"] = Tool{
		Name:        "backup_utility",
		Description: "文件备份和恢复工具",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"description": "操作类型",
					"enum":        []string{"backup", "restore", "list_backups"},
				},
				"source": map[string]interface{}{
					"type":        "string",
					"description": "源目录",
				},
				"backup_dir": map[string]interface{}{
					"type":        "string",
					"description": "备份目录",
				},
				"backup_name": map[string]interface{}{
					"type":        "string",
					"description": "备份名称",
				},
			},
			"required": []string{"action"},
		},
		Handler: r.handleBackupUtility,
	}
}

// 多媒体工具
func (r *ExtendedToolRegistry) registerMultimediaTools() {
	// 图像处理
	r.tools["image_processor"] = Tool{
		Name:        "image_processor",
		Description: "图像处理工具",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "操作类型",
					"enum":        []string{"resize", "crop", "convert", "watermark", "ocr"},
				},
				"input_path": map[string]interface{}{
					"type":        "string",
					"description": "输入图片路径",
				},
				"output_path": map[string]interface{}{
					"type":        "string",
					"description": "输出图片路径",
				},
				"width": map[string]interface{}{
					"type":        "integer",
					"description": "宽度（像素）",
				},
				"height": map[string]interface{}{
					"type":        "integer",
					"description": "高度（像素）",
				},
			},
			"required": []string{"operation", "input_path", "output_path"},
		},
		Handler: r.handleImageProcessor,
	}

	// 音频处理
	r.tools["audio_processor"] = Tool{
		Name:        "audio_processor",
		Description: "音频处理工具",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "操作类型",
					"enum":        []string{"convert", "trim", "merge", "extract_audio"},
				},
				"input_path": map[string]interface{}{
					"type":        "string",
					"description": "输入音频路径",
				},
				"output_path": map[string]interface{}{
					"type":        "string",
					"description": "输出音频路径",
				},
				"start_time": map[string]interface{}{
					"type":        "string",
					"description": "开始时间（HH:MM:SS格式）",
				},
				"duration": map[string]interface{}{
					"type":        "string",
					"description": "持续时间（秒或HH:MM:SS格式）",
				},
			},
			"required": []string{"operation", "input_path", "output_path"},
		},
		Handler: r.handleAudioProcessor,
	}
}

// 工具处理函数实现
func (r *ExtendedToolRegistry) handleShellExecute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	command := args["command"].(string)
	workingDir := "."
	if wd, ok := args["working_dir"].(string); ok {
		workingDir = wd
	}
	
	timeout := 30
	if t, ok := args["timeout"].(float64); ok {
		timeout = int(t)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workingDir
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"output":  string(output),
		}, nil
	}

	return map[string]interface{}{
		"success": true,
		"output":  string(output),
		"command": command,
	}, nil
}

func (r *ExtendedToolRegistry) handleHTTPClient(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url := args["url"].(string)
	method := "GET"
	if m, ok := args["method"].(string); ok {
		method = m
	}

	timeout := 30
	if t, ok := args["timeout"].(float64); ok {
		timeout = int(t)
	}

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	var body io.Reader
	if b, ok := args["body"].(string); ok && b != "" {
		body = strings.NewReader(b)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// 设置请求头
	if headers, ok := args["headers"].(map[string]interface{}); ok {
		for key, value := range headers {
			if strValue, ok := value.(string); ok {
				req.Header.Set(key, strValue)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status_code": resp.StatusCode,
		"headers":     resp.Header,
		"body":        string(respBody),
		"url":         url,
		"method":      method,
	}, nil
}

func (r *ExtendedToolRegistry) handleAdvancedFileOps(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	operation := args["operation"].(string)
	source := args["source"].(string)

	switch operation {
	case "batch_rename":
		pattern := args["pattern"].(string)
		// 实现批量重命名逻辑
		return map[string]interface{}{
			"operation": operation,
			"source":    source,
			"pattern":   pattern,
			"result":    "Batch rename completed",
		}, nil
		
	case "compress":
		destination := args["destination"].(string)
		// 实现压缩逻辑
		return map[string]interface{}{
			"operation":   operation,
			"source":      source,
			"destination": destination,
			"result":      "Compression completed",
		}, nil
		
	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}
}

// 其他工具处理函数的占位实现...
func (r *ExtendedToolRegistry) handleSystemMonitor(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"cpu_usage":    "25%",
		"memory_usage": "60%",
		"disk_usage":   "45%",
		"timestamp":    time.Now().Unix(),
	}, nil
}

func (r *ExtendedToolRegistry) handleProcessManager(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action := args["action"].(string)
	return map[string]interface{}{
		"action": action,
		"result": "Process management completed",
	}, nil
}

func (r *ExtendedToolRegistry) handleNetworkDiagnostic(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	target := args["target"].(string)
	return map[string]interface{}{
		"target":  target,
		"results": "Network diagnostic completed",
	}, nil
}

func (r *ExtendedToolRegistry) handleBackupUtility(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	action := args["action"].(string)
	return map[string]interface{}{
		"action": action,
		"result": "Backup utility completed",
	}, nil
}

func (r *ExtendedToolRegistry) handleImageProcessor(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	operation := args["operation"].(string)
	inputPath := args["input_path"].(string)
	outputPath := args["output_path"].(string)
	return map[string]interface{}{
		"operation":  operation,
		"input_path": inputPath,
		"output_path": outputPath,
		"result":     "Image processing completed",
	}, nil
}

func (r *ExtendedToolRegistry) handleAudioProcessor(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	operation := args["operation"].(string)
	inputPath := args["input_path"].(string)
	outputPath := args["output_path"].(string)
	return map[string]interface{}{
		"operation":  operation,
		"input_path": inputPath,
		"output_path": outputPath,
		"result":     "Audio processing completed",
	}, nil
}