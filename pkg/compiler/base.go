package compiler

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SandboxCompiler 沙箱编译器基类
// 封装了在 Docker/go-judge 沙箱中执行编译的通用逻辑
type SandboxCompiler struct {
	CompilerCmd  string   // 编译器命令 (如 gcc, clang, go)
	DefaultFlags []string // 默认编译标志
	WorkDir      string   // 工作目录
	UseSandbox   bool     // 是否使用沙箱
}

// SandboxExec 在沙箱中执行命令
func (s *SandboxCompiler) SandboxExec(ctx context.Context, cmdArgs []string, input string, timeout time.Duration) (stdout, stderr string, err error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if s.UseSandbox {
		// 通过 Docker 沙箱执行
		dockerArgs := []string{
			"run", "--rm",
			"--network=none",        // 禁用网络
			"--memory=512m",         // 限制内存
			"--cpus=1",              // 限制 CPU
			"--pids-limit=64",       // 限制进程数
			"-v", s.WorkDir + ":/workspace",
			"-w", "/workspace",
		}
		dockerArgs = append(dockerArgs, cmdArgs...)
		cmd = exec.CommandContext(ctx, "docker", dockerArgs...)
	} else {
		// 直接在本地执行
		cmd = exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
		cmd.Dir = s.WorkDir
	}

	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// ParseCompileErrors 解析编译错误输出
// 通用的 GCC/Clang 风格错误解析: file:line:col: error: message
func ParseCompileErrors(stderr string) []CompileError {
	var errors []CompileError

	lines := strings.Split(stderr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		ce := CompileError{Message: line, Type: "error"}

		// 尝试解析 file:line:col: type: message 格式
		parts := strings.SplitN(line, ":", 5)
		if len(parts) >= 4 {
			ce.File = parts[0]
			fmt.Sscanf(parts[1], "%d", &ce.Line)
			fmt.Sscanf(parts[2], "%d", &ce.Column)

			typeAndMsg := strings.TrimSpace(parts[3])
			if strings.HasPrefix(typeAndMsg, " error") {
				ce.Type = "error"
			} else if strings.HasPrefix(typeAndMsg, " warning") {
				ce.Type = "warning"
			} else if strings.HasPrefix(typeAndMsg, " note") {
				ce.Type = "note"
			}

			if len(parts) >= 5 {
				ce.Message = strings.TrimSpace(parts[4])
			}
		}

		errors = append(errors, ce)
	}

	return errors
}
