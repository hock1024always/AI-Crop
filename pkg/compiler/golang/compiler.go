package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ai-corp/pkg/compiler"
)

// Compiler Go 编译器实现
type Compiler struct {
	sandbox *compiler.SandboxCompiler
}

func New() *Compiler {
	return &Compiler{
		sandbox: &compiler.SandboxCompiler{
			CompilerCmd:  "go",
			DefaultFlags: []string{},
			WorkDir:      "/tmp/go-compile",
			UseSandbox:   false,
		},
	}
}

func (c *Compiler) Name() string    { return "go-compiler" }
func (c *Compiler) Version() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func (c *Compiler) SupportedLanguages() []string {
	return []string{"go"}
}

func (c *Compiler) OptimizationLevels() []string {
	return []string{} // Go 编译器不支持手动优化级别
}

func (c *Compiler) Validate() error {
	_, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go 未安装: %w", err)
	}
	return nil
}

func (c *Compiler) Compile(ctx context.Context, req *compiler.CompileRequest) (*compiler.CompileResult, error) {
	start := time.Now()

	workDir, err := os.MkdirTemp("", "go-compile-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 写入源文件
	srcFile := filepath.Join(workDir, "main.go")
	if err := os.WriteFile(srcFile, []byte(req.SourceCode), 0644); err != nil {
		return nil, fmt.Errorf("写入源文件失败: %w", err)
	}

	// 初始化 go module
	modContent := "module sandbox\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte(modContent), 0644); err != nil {
		return nil, fmt.Errorf("创建 go.mod 失败: %w", err)
	}

	// 编译
	outFile := filepath.Join(workDir, "main")
	args := []string{"go", "build", "-o", outFile, srcFile}

	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	stdout, stderr, execErr := c.sandbox.SandboxExec(ctx, args, "", timeout)
	duration := time.Since(start)

	result := &compiler.CompileResult{
		Stdout: stdout,
		Stderr: stderr,
		Stats:  compiler.CompileStats{Duration: duration},
	}

	if execErr != nil {
		result.Success = false
		result.Errors = parseGoErrors(stderr)
		return result, nil
	}

	result.Success = true
	result.BinaryPath = outFile

	if info, err := os.Stat(outFile); err == nil {
		result.Stats.OutputSize = info.Size()
		result.Binary, _ = os.ReadFile(outFile)
	}

	return result, nil
}

// parseGoErrors 解析 Go 编译错误
func parseGoErrors(stderr string) []compiler.CompileError {
	var errors []compiler.CompileError

	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		ce := compiler.CompileError{Message: line, Type: "error"}

		// Go 错误格式: ./file.go:line:col: message
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 3 {
			ce.File = parts[0]
			fmt.Sscanf(parts[1], "%d", &ce.Line)
			fmt.Sscanf(parts[2], "%d", &ce.Column)
			if len(parts) >= 4 {
				ce.Message = strings.TrimSpace(parts[3])
			}
		}

		errors = append(errors, ce)
	}

	return errors
}
