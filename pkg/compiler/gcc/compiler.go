package gcc

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

// Compiler GCC 编译器实现
type Compiler struct {
	sandbox *compiler.SandboxCompiler
}

func New() *Compiler {
	return &Compiler{
		sandbox: &compiler.SandboxCompiler{
			CompilerCmd:  "gcc",
			DefaultFlags: []string{"-Wall", "-Wextra"},
			WorkDir:      "/tmp/gcc-compile",
			UseSandbox:   false,
		},
	}
}

func (c *Compiler) Name() string    { return "gcc" }
func (c *Compiler) Version() string {
	out, err := exec.Command("gcc", "--version").Output()
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
	return []string{"-O0", "-O1", "-O2", "-O3", "-Os"}
}

func (c *Compiler) Validate() error {
	_, err := exec.LookPath("gcc")
	if err != nil {
		return fmt.Errorf("gcc 未安装: %w", err)
	}
	return nil
}

func (c *Compiler) Compile(ctx context.Context, req *compiler.CompileRequest) (*compiler.CompileResult, error) {
	start := time.Now()

	workDir, err := os.MkdirTemp("", "gcc-compile-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)

	var ext, compilerCmd string
	switch req.Language {
	case "c":
		ext = ".c"
		compilerCmd = "gcc"
	case "cpp":
		ext = ".cpp"
		compilerCmd = "g++"
	default:
		return nil, fmt.Errorf("不支持的语言: %s", req.Language)
	}

	srcFile := filepath.Join(workDir, "main"+ext)
	if err := os.WriteFile(srcFile, []byte(req.SourceCode), 0644); err != nil {
		return nil, fmt.Errorf("写入源文件失败: %w", err)
	}

	outFile := filepath.Join(workDir, "main.out")
	args := []string{compilerCmd}
	args = append(args, c.sandbox.DefaultFlags...)

	if req.OptimizationLevel != "" {
		args = append(args, req.OptimizationLevel)
	}

	for k, v := range req.Options {
		if v != "" {
			args = append(args, fmt.Sprintf("-%s=%s", k, v))
		} else {
			args = append(args, fmt.Sprintf("-%s", k))
		}
	}

	args = append(args, "-o", outFile, srcFile)

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
		result.Errors = compiler.ParseCompileErrors(stderr)
		return result, nil
	}

	result.Success = true
	result.BinaryPath = outFile

	if info, err := os.Stat(outFile); err == nil {
		result.Stats.OutputSize = info.Size()
		result.Binary, _ = os.ReadFile(outFile)
	}

	if stderr != "" {
		for _, e := range compiler.ParseCompileErrors(stderr) {
			if e.Type == "warning" {
				result.Warnings = append(result.Warnings, e.Message)
			}
		}
	}

	return result, nil
}
