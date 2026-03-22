package compiler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CompileRequest 编译请求
type CompileRequest struct {
	Language          string            `json:"language"`           // c, cpp, go, java, python, rust
	SourceCode        string            `json:"source_code"`       // 源代码
	Options           map[string]string `json:"options,omitempty"` // 编译选项
	OptimizationLevel string            `json:"optimization_level,omitempty"` // -O0, -O1, -O2, -O3
	Timeout           time.Duration     `json:"timeout,omitempty"` // 编译超时
}

// CompileResult 编译结果
type CompileResult struct {
	Success    bool           `json:"success"`
	Binary     []byte         `json:"-"`          // 编译产物
	BinaryPath string         `json:"binary_path,omitempty"`
	Stdout     string         `json:"stdout,omitempty"`
	Stderr     string         `json:"stderr,omitempty"`
	Errors     []CompileError `json:"errors,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Stats      CompileStats   `json:"stats"`
}

// CompileError 编译错误
type CompileError struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Message string `json:"message"`
	Type    string `json:"type"` // error, warning, note
}

// CompileStats 编译统计
type CompileStats struct {
	Duration    time.Duration `json:"duration"`
	MemoryUsed  int64         `json:"memory_used"`  // bytes
	OutputSize  int64         `json:"output_size"`   // bytes
}

// CompilerPlugin 编译器插件接口
//
// 合作者需要实现此接口来接入新的编译器。
// 实现步骤:
//   1. 创建新的包 (如 pkg/compiler/llvm/)
//   2. 实现 CompilerPlugin 接口
//   3. 调用 compiler.Register() 注册
//   4. 在 configs/compilers.yaml 中配置
type CompilerPlugin interface {
	// Name 编译器名称 (如: llvm-clang, gcc, go-compiler)
	Name() string

	// Version 编译器版本
	Version() string

	// SupportedLanguages 支持的语言列表
	SupportedLanguages() []string

	// OptimizationLevels 支持的优化级别
	OptimizationLevels() []string

	// Compile 编译入口
	Compile(ctx context.Context, req *CompileRequest) (*CompileResult, error)

	// Validate 验证编译器是否可用 (检查依赖等)
	Validate() error
}

// Factory 编译器工厂
type Factory struct {
	mu       sync.RWMutex
	plugins  map[string]CompilerPlugin
	langMap  map[string][]string // language -> []compiler_name
}

// 全局工厂实例
var globalFactory = &Factory{
	plugins: make(map[string]CompilerPlugin),
	langMap: make(map[string][]string),
}

// Register 注册编译器插件到全局工厂
func Register(plugin CompilerPlugin) error {
	return globalFactory.Register(plugin)
}

// Get 从全局工厂获取编译器
func Get(name string) (CompilerPlugin, error) {
	return globalFactory.Get(name)
}

// GetByLanguage 从全局工厂按语言获取编译器
func GetByLanguage(lang string) (CompilerPlugin, error) {
	return globalFactory.GetByLanguage(lang)
}

// ListAll 列出所有已注册的编译器
func ListAll() []CompilerInfo {
	return globalFactory.ListAll()
}

// Register 注册编译器插件
func (f *Factory) Register(plugin CompilerPlugin) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	name := plugin.Name()
	if _, exists := f.plugins[name]; exists {
		return fmt.Errorf("编译器 %s 已注册", name)
	}

	// 验证编译器是否可用
	if err := plugin.Validate(); err != nil {
		return fmt.Errorf("编译器 %s 验证失败: %w", name, err)
	}

	f.plugins[name] = plugin

	// 建立语言映射
	for _, lang := range plugin.SupportedLanguages() {
		f.langMap[lang] = append(f.langMap[lang], name)
	}

	return nil
}

// Get 按名称获取编译器
func (f *Factory) Get(name string) (CompilerPlugin, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	plugin, exists := f.plugins[name]
	if !exists {
		return nil, fmt.Errorf("编译器 %s 未注册", name)
	}
	return plugin, nil
}

// GetByLanguage 按语言获取编译器 (返回第一个匹配的)
func (f *Factory) GetByLanguage(lang string) (CompilerPlugin, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	names, exists := f.langMap[lang]
	if !exists || len(names) == 0 {
		return nil, fmt.Errorf("没有支持 %s 语言的编译器", lang)
	}

	return f.plugins[names[0]], nil
}

// CompilerInfo 编译器信息
type CompilerInfo struct {
	Name               string   `json:"name"`
	Version            string   `json:"version"`
	SupportedLanguages []string `json:"supported_languages"`
	OptimizationLevels []string `json:"optimization_levels"`
}

// ListAll 列出所有编译器
func (f *Factory) ListAll() []CompilerInfo {
	f.mu.RLock()
	defer f.mu.RUnlock()

	list := make([]CompilerInfo, 0, len(f.plugins))
	for _, plugin := range f.plugins {
		list = append(list, CompilerInfo{
			Name:               plugin.Name(),
			Version:            plugin.Version(),
			SupportedLanguages: plugin.SupportedLanguages(),
			OptimizationLevels: plugin.OptimizationLevels(),
		})
	}
	return list
}

// Compile 使用指定编译器编译
func (f *Factory) Compile(ctx context.Context, compilerName string, req *CompileRequest) (*CompileResult, error) {
	plugin, err := f.Get(compilerName)
	if err != nil {
		return nil, err
	}
	return plugin.Compile(ctx, req)
}

// CompileByLanguage 根据语言自动选择编译器编译
func (f *Factory) CompileByLanguage(ctx context.Context, req *CompileRequest) (*CompileResult, error) {
	plugin, err := f.GetByLanguage(req.Language)
	if err != nil {
		return nil, err
	}
	return plugin.Compile(ctx, req)
}
