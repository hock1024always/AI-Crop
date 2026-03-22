package llvm

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

// Compiler LLVM/Clang 编译器实现
//
// 接入指南:
//   1. 确保系统已安装 clang (建议 >= 14.0)
//   2. 调用 compiler.Register(&llvm.Compiler{}) 注册
//   3. 在 configs/compilers.yaml 中配置启用
//
// 安装 clang:
//   Ubuntu/Debian: apt install clang
//   CentOS/RHEL:   yum install clang
//   macOS:         xcode-select --install
type Compiler struct {
	sandbox *compiler.SandboxCompiler
}

// New 创建 LLVM 编译器实例
func New() *Compiler {
	return &Compiler{
		sandbox: &compiler.SandboxCompiler{
			CompilerCmd:  "clang",
			DefaultFlags: []string{"-Wall", "-Wextra"},
			WorkDir:      "/tmp/llvm-compile",
			UseSandbox:   false,
		},
	}
}

func (c *Compiler) Name() string    { return "llvm-clang" }
func (c *Compiler) Version() string {
	out, err := exec.Command("clang", "--version").Output()
	if err != nil {
		return "unknown"
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return "unknown"
}

func (c *Compiler) SupportedLanguages() []string {
	return []string{"c", "cpp"}
}

func (c *Compiler) OptimizationLevels() []string {
	return []string{"-O0", "-O1", "-O2", "-O3", "-Os", "-Oz"}
}

func (c *Compiler) Validate() error {
	_, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang 未安装: %w (请安装: apt install clang 或 yum install clang)", err)
	}
	return nil
}

func (c *Compiler) Compile(ctx context.Context, req *compiler.CompileRequest) (*compiler.CompileResult, error) {
	start := time.Now()

	// 创建临时工作目录
	workDir, err := os.MkdirTemp("", "llvm-compile-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 确定文件扩展名和编译器
	var ext, compilerCmd string
	switch req.Language {
	case "c":
		ext = ".c"
		compilerCmd = "clang"
	case "cpp":
		ext = ".cpp"
		compilerCmd = "clang++"
	default:
		return nil, fmt.Errorf("不支持的语言: %s", req.Language)
	}

	// 写入源文件
	srcFile := filepath.Join(workDir, "main"+ext)
	if err := os.WriteFile(srcFile, []byte(req.SourceCode), 0644); err != nil {
		return nil, fmt.Errorf("写入源文件失败: %w", err)
	}

	// 构建编译命令
	outFile := filepath.Join(workDir, "main.out")
	args := []string{compilerCmd}
	args = append(args, c.sandbox.DefaultFlags...)

	// 优化级别
	if req.OptimizationLevel != "" {
		args = append(args, req.OptimizationLevel)
	}

	// 自定义编译选项
	for k, v := range req.Options {
		if v != "" {
			args = append(args, fmt.Sprintf("-%s=%s", k, v))
		} else {
			args = append(args, fmt.Sprintf("-%s", k))
		}
	}

	args = append(args, "-o", outFile, srcFile)

	// 设置超时
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// 执行编译
	stdout, stderr, execErr := c.sandbox.SandboxExec(ctx, args, "", timeout)

	duration := time.Since(start)

	result := &compiler.CompileResult{
		Stdout: stdout,
		Stderr: stderr,
		Stats: compiler.CompileStats{
			Duration: duration,
		},
	}

	if execErr != nil {
		result.Success = false
		result.Errors = compiler.ParseCompileErrors(stderr)
		return result, nil // 编译失败不返回 error
	}

	result.Success = true
	result.BinaryPath = outFile

	// 获取输出文件大小
	if info, err := os.Stat(outFile); err == nil {
		result.Stats.OutputSize = info.Size()
		result.Binary, _ = os.ReadFile(outFile)
	}

	// 提取 warnings
	if stderr != "" {
		for _, e := range compiler.ParseCompileErrors(stderr) {
			if e.Type == "warning" {
				result.Warnings = append(result.Warnings, e.Message)
			}
		}
	}

	return result, nil
}
