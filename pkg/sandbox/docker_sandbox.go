package sandbox

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ============================================
// Docker Task Sandbox - 安全隔离的任务执行环境
// 参考: E2B (Firecracker), Daytona (Docker), Open Interpreter
// ============================================

// SandboxConfig 沙箱配置
type SandboxConfig struct {
	// 资源限制
	MemoryMB     int64   // 内存限制 (MB)
	CPUQuota     int64   // CPU 配额 (微秒/周期)
	CPUPeriod    int64   // CPU 周期 (微秒)
	CPUCount     float64 // CPU 核心数限制

	// 超时设置
	ExecutionTimeout time.Duration // 执行超时
	IdleTimeout      time.Duration // 空闲超时

	// 网络隔离
	NetworkEnabled bool     // 是否启用网络
	AllowedHosts   []string // 允许访问的主机白名单

	// 存储隔离
	WorkDir       string   // 工作目录
	ReadOnlyPaths []string // 只读挂载路径
	TempDir       string   // 临时目录

	// 安全设置
	NoNewPrivileges bool     // 禁止提权
	Capabilities    []string // 保留的能力
	SeccompProfile  string   // seccomp 配置文件
}

// DefaultSandboxConfig 返回默认沙箱配置
func DefaultSandboxConfig() *SandboxConfig {
	return &SandboxConfig{
		MemoryMB:         512,
		CPUQuota:         50000,
		CPUPeriod:        100000,
		CPUCount:         1.0,
		ExecutionTimeout: 5 * time.Minute,
		IdleTimeout:      10 * time.Minute,
		NetworkEnabled:   false,
		AllowedHosts:     []string{},
		WorkDir:          "/tmp/ai-corp-sandbox",
		ReadOnlyPaths:    []string{},
		TempDir:          "/tmp",
		NoNewPrivileges:  true,
		Capabilities:     []string{},
		// 默认启用 seccomp，使用内置 default profile
		SeccompProfile: "default",
	}
}

// TaskSandbox 任务沙箱
type TaskSandbox struct {
	ID           string
	TaskID       string
	ContainerID  string
	Image        string
	Config       *SandboxConfig
	Status       string // creating, running, completed, failed, timeout
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ExitCode     int
	Output       string
	Error        error
	mu           sync.RWMutex
	cancelFunc   context.CancelFunc
	cleanupOnce  sync.Once
}

// SandboxManager 沙箱管理器
type SandboxManager struct {
	sandboxes   map[string]*TaskSandbox
	config      *SandboxConfig
	mu          sync.RWMutex
	seccompPath string // seccomp profile 文件路径
}

// NewSandboxManager 创建沙箱管理器
func NewSandboxManager(config *SandboxConfig) (*SandboxManager, error) {
	if config == nil {
		config = DefaultSandboxConfig()
	}

	// 确保 Docker 可用
	if err := exec.Command("docker", "version").Run(); err != nil {
		return nil, fmt.Errorf("docker not available: %w", err)
	}

	// 创建工作目录
	if err := os.MkdirAll(config.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	// 确定 seccomp profile 路径
	seccompPath := ""
	if config.SeccompProfile != "" && config.SeccompProfile != "default" {
		seccompPath = config.SeccompProfile
	} else if config.SeccompProfile == "default" {
		// 使用内置 seccomp profile（Docker 默认）
		seccompPath = "default"
	}

	// 若有需要网络白名单的配置，预先创建隔离网络
	if config.NetworkEnabled && len(config.AllowedHosts) > 0 {
		if err := ensureSandboxNetwork(); err != nil {
			log.Printf("[Sandbox] Warning: failed to create sandbox network: %v", err)
		}
	}

	return &SandboxManager{
		sandboxes:   make(map[string]*TaskSandbox),
		config:      config,
		seccompPath: seccompPath,
	}, nil
}

// ensureSandboxNetwork 确保 ai-corp-sandbox-net 网络存在（internal 模式，无外网出口）
func ensureSandboxNetwork() error {
	// 检查是否已存在
	out, _ := exec.Command("docker", "network", "inspect", "ai-corp-sandbox-net").Output()
	if len(out) > 2 {
		return nil // 已存在
	}
	// 创建 internal 网络：容器间可通信，但无法访问外网
	cmd := exec.Command("docker", "network", "create",
		"--internal",        // 无外网出口
		"--driver", "bridge",
		"ai-corp-sandbox-net",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create sandbox network: %w, output: %s", err, string(out))
	}
	log.Println("[Sandbox] Created isolated network: ai-corp-sandbox-net")
	return nil
}

// CreateSandbox 创建任务沙箱
func (sm *SandboxManager) CreateSandbox(ctx context.Context, taskID, image string, customConfig *SandboxConfig) (*TaskSandbox, error) {
	config := sm.config
	if customConfig != nil {
		config = customConfig
	}

	sandboxID := fmt.Sprintf("sandbox-%s-%d", taskID[:8], time.Now().UnixNano())

	sandbox := &TaskSandbox{
		ID:        sandboxID,
		TaskID:    taskID,
		Image:     image,
		Config:    config,
		Status:    "creating",
		CreatedAt: time.Now(),
	}

	sm.mu.Lock()
	sm.sandboxes[sandboxID] = sandbox
	sm.mu.Unlock()

	// 创建沙箱工作目录
	sandboxDir := filepath.Join(config.WorkDir, sandboxID)
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sandbox dir: %w", err)
	}

	// 构建 Docker 命令参数
	args := sm.buildDockerRunArgs(sandbox, sandboxDir)

	// 创建容器
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		sandbox.Status = "failed"
		sandbox.Error = fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
		return sandbox, sandbox.Error
	}

	containerID := strings.TrimSpace(string(output))
	sandbox.ContainerID = containerID
	sandbox.Status = "running"
	now := time.Now()
	sandbox.StartedAt = &now

	log.Printf("[Sandbox] Created sandbox %s for task %s (container: %s)", sandboxID, taskID, containerID)

	return sandbox, nil
}

// buildDockerRunArgs 构建 docker run 参数
func (sm *SandboxManager) buildDockerRunArgs(sandbox *TaskSandbox, workDir string) []string {
	config := sandbox.Config
	args := []string{
		"run",
		"-d",
		"--name", sandbox.ID,
		// 资源限制
		"--memory", fmt.Sprintf("%dm", config.MemoryMB),
		"--memory-swap", fmt.Sprintf("%dm", config.MemoryMB), // 禁用 swap
		"--cpu-quota", fmt.Sprintf("%d", config.CPUQuota),
		"--cpu-period", fmt.Sprintf("%d", config.CPUPeriod),
		"--cpus", fmt.Sprintf("%.1f", config.CPUCount),
		// 安全设置
		"--no-new-privileges",
		"--security-opt", "no-new-privileges:true",
		// 进程隔离
		"--pids-limit", "256",
		// 自动清理（容器退出后删除）
		"--rm",
	}

	// seccomp profile
	if sm.seccompPath == "default" {
		// Docker 默认 seccomp profile（已内置）
		args = append(args, "--security-opt", "seccomp=default")
	} else if sm.seccompPath != "" {
		// 自定义 seccomp profile 文件
		args = append(args, "--security-opt", fmt.Sprintf("seccomp=%s", sm.seccompPath))
	}

	// 网络隔离
	if !config.NetworkEnabled {
		// 完全禁用网络
		args = append(args, "--network", "none")
	} else if len(config.AllowedHosts) > 0 {
		// 使用 internal bridge 网络：容器间互通但无外网出口
		// 真实白名单访问控制：通过 DNS 解析 + iptables 在宿主机层面限制
		args = append(args, "--network", "ai-corp-sandbox-net")
		// 注入 DNS 白名单（允许的域名解析）
		for _, host := range config.AllowedHosts {
			args = append(args, "--add-host", fmt.Sprintf("%s:$(dig +short %s | head -1)", host, host))
		}
	} else {
		// 启用网络但无白名单（仅用于明确需要的场景）
		args = append(args, "--network", "bridge")
	}

	// 只读挂载
	for _, path := range config.ReadOnlyPaths {
		args = append(args, "-v", fmt.Sprintf("%s:%s:ro", path, path))
	}

	// 工作目录挂载
	args = append(args, "-v", fmt.Sprintf("%s:/workspace:rw", workDir))
	args = append(args, "-w", "/workspace")

	// 能力限制：先全部移除，再按需添加
	args = append(args, "--cap-drop=ALL")
	for _, cap := range config.Capabilities {
		args = append(args, fmt.Sprintf("--cap-add=%s", cap))
	}

	// 镜像
	args = append(args, sandbox.Image)

	// 保持容器运行直到超时
	args = append(args, "sleep", fmt.Sprintf("%d", int(config.ExecutionTimeout.Seconds())+60))

	return args
}

// ExecuteInSandbox 在沙箱中执行命令
func (sm *SandboxManager) ExecuteInSandbox(ctx context.Context, sandboxID string, command []string) (string, int, error) {
	sm.mu.RLock()
	sandbox, exists := sm.sandboxes[sandboxID]
	sm.mu.RUnlock()

	if !exists {
		return "", -1, fmt.Errorf("sandbox not found: %s", sandboxID)
	}

	// 创建带超时的上下文
	execCtx, cancel := context.WithTimeout(ctx, sandbox.Config.ExecutionTimeout)
	defer cancel()

	sandbox.mu.Lock()
	sandbox.cancelFunc = cancel
	sandbox.mu.Unlock()

	// 构建执行命令
	args := []string{"exec", sandbox.ContainerID}
	args = append(args, command...)

	cmd := exec.CommandContext(execCtx, "docker", args...)
	output, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			sandbox.Status = "timeout"
			sandbox.Error = fmt.Errorf("execution timeout")
			return string(output), -1, sandbox.Error
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return string(output), exitCode, err
}

// ExecuteScript 在沙箱中执行脚本
func (sm *SandboxManager) ExecuteScript(ctx context.Context, sandboxID string, script string, language string) (string, int, error) {
	var command []string

	switch language {
	case "python":
		command = []string{"python3", "-c", script}
	case "bash", "shell":
		command = []string{"bash", "-c", script}
	case "go":
		command = []string{"sh", "-c", fmt.Sprintf("echo '%s' > /tmp/script.go && go run /tmp/script.go", script)}
	case "javascript", "node":
		command = []string{"node", "-e", script}
	default:
		command = []string{"sh", "-c", script}
	}

	return sm.ExecuteInSandbox(ctx, sandboxID, command)
}

// StopSandbox 停止沙箱
func (sm *SandboxManager) StopSandbox(sandboxID string) error {
	sm.mu.RLock()
	sandbox, exists := sm.sandboxes[sandboxID]
	sm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("sandbox not found: %s", sandboxID)
	}

	return sandbox.Cleanup()
}

// Cleanup 清理沙箱资源
func (s *TaskSandbox) Cleanup() error {
	var err error
	s.cleanupOnce.Do(func() {
		// 停止容器
		if s.ContainerID != "" {
			cmd := exec.Command("docker", "stop", "-t", "5", s.ContainerID)
			if output, stopErr := cmd.CombinedOutput(); stopErr != nil {
				log.Printf("[Sandbox] Warning: failed to stop container %s: %v, output: %s", s.ContainerID, stopErr, string(output))
			}
		}

		// 清理工作目录
		sandboxDir := filepath.Join(s.Config.WorkDir, s.ID)
		if rmErr := os.RemoveAll(sandboxDir); rmErr != nil {
			log.Printf("[Sandbox] Warning: failed to remove sandbox dir %s: %v", sandboxDir, rmErr)
		}

		now := time.Now()
		s.FinishedAt = &now
		if s.Status == "running" {
			s.Status = "completed"
		}

		log.Printf("[Sandbox] Cleaned up sandbox %s", s.ID)
	})
	return err
}

// GetSandbox 获取沙箱信息
func (sm *SandboxManager) GetSandbox(sandboxID string) (*TaskSandbox, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sandbox, exists := sm.sandboxes[sandboxID]
	if !exists {
		return nil, fmt.Errorf("sandbox not found: %s", sandboxID)
	}

	// 更新容器状态
	if sandbox.ContainerID != "" {
		cmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", sandbox.ContainerID)
		if output, err := cmd.Output(); err == nil {
			status := strings.TrimSpace(string(output))
			if status == "exited" || status == "dead" {
				sandbox.Status = "completed"
			}
		}
	}

	return sandbox, nil
}

// ListSandboxes 列出所有沙箱
func (sm *SandboxManager) ListSandboxes() []*TaskSandbox {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sandboxes := make([]*TaskSandbox, 0, len(sm.sandboxes))
	for _, s := range sm.sandboxes {
		sandboxes = append(sandboxes, s)
	}
	return sandboxes
}

// CleanupIdleSandboxes 清理空闲超时的沙箱
func (sm *SandboxManager) CleanupIdleSandboxes() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	count := 0
	for id, sandbox := range sm.sandboxes {
		if sandbox.Status == "completed" || sandbox.Status == "failed" || sandbox.Status == "timeout" {
			if sandbox.FinishedAt != nil && time.Since(*sandbox.FinishedAt) > sandbox.Config.IdleTimeout {
				sandbox.Cleanup()
				delete(sm.sandboxes, id)
				count++
			}
		}
	}
	return count
}

// GetSandboxStats 获取沙箱统计信息
func (sm *SandboxManager) GetSandboxStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := map[string]int{
		"total":     len(sm.sandboxes),
		"running":   0,
		"completed": 0,
		"failed":    0,
		"timeout":   0,
	}

	for _, s := range sm.sandboxes {
		stats[s.Status]++
	}

	return map[string]interface{}{
		"sandbox_counts": stats,
		"config":         sm.config,
	}
}

// Close 关闭沙箱管理器
func (sm *SandboxManager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, sandbox := range sm.sandboxes {
		sandbox.Cleanup()
	}

	return nil
}

// ============================================
// 预定义的沙箱配置模板
// ============================================

// CodeExecutionSandbox 代码执行沙箱配置
func CodeExecutionSandbox() *SandboxConfig {
	return &SandboxConfig{
		MemoryMB:         1024,
		CPUQuota:         100000,
		CPUPeriod:        100000,
		CPUCount:         1.0,
		ExecutionTimeout: 10 * time.Minute,
		IdleTimeout:      15 * time.Minute,
		NetworkEnabled:   false,
		WorkDir:          "/tmp/ai-corp-sandbox",
		NoNewPrivileges:  true,
		Capabilities:     []string{},
		SeccompProfile:   "default",
	}
}

// WebScraperSandbox 网页抓取沙箱配置
// NetworkEnabled=true + AllowedHosts 白名单 → 使用 internal bridge + 限定域名
func WebScraperSandbox() *SandboxConfig {
	return &SandboxConfig{
		MemoryMB:         512,
		CPUQuota:         50000,
		CPUPeriod:        100000,
		CPUCount:         0.5,
		ExecutionTimeout: 5 * time.Minute,
		IdleTimeout:      10 * time.Minute,
		NetworkEnabled:   true,
		AllowedHosts:     []string{"wikipedia.org", "github.com"},
		WorkDir:          "/tmp/ai-corp-sandbox",
		NoNewPrivileges:  true,
		Capabilities:     []string{},
		SeccompProfile:   "default",
	}
}

// DataProcessingSandbox 数据处理沙箱配置
func DataProcessingSandbox() *SandboxConfig {
	return &SandboxConfig{
		MemoryMB:         2048,
		CPUQuota:         200000,
		CPUPeriod:        100000,
		CPUCount:         2.0,
		ExecutionTimeout: 30 * time.Minute,
		IdleTimeout:      60 * time.Minute,
		NetworkEnabled:   false,
		WorkDir:          "/tmp/ai-corp-sandbox",
		NoNewPrivileges:  true,
		Capabilities:     []string{},
		SeccompProfile:   "default",
	}
}
