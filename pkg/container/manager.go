package container

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ============================================
// Agent 容器管理器 - 使用 Docker CLI
// ============================================

// AgentContainer Agent 容器定义
type AgentContainer struct {
	ID          string
	Name        string
	Image       string
	AgentType   string
	Status      string // running/stopped/paused/error
	Resources   ResourceConfig
	Environment map[string]string
	Ports       map[int]int // host:container
	Volumes     []VolumeMount
	CreatedAt   time.Time
	StartedAt   *time.Time
}

// ResourceConfig 资源配置
type ResourceConfig struct {
	CPUQuota  int64  // CPU 配额
	CPUPeriod int64  // CPU 周期
	Memory    int64  // 内存限制 (MB)
	Swap      int64  // 交换分区限制
}

// VolumeMount 卷挂载配置
type VolumeMount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// ContainerManager 容器管理器
type ContainerManager struct {
	containers map[string]*AgentContainer
	mu         sync.RWMutex
}

// NewContainerManager 创建容器管理器
func NewContainerManager() (*ContainerManager, error) {
	// 检查 Docker 是否可用
	if err := exec.Command("docker", "version").Run(); err != nil {
		log.Printf("Warning: Docker not available: %v", err)
	}
	
	return &ContainerManager{
		containers: make(map[string]*AgentContainer),
	}, nil
}

// CreateAgentContainer 创建 Agent 容器
func (cm *ContainerManager) CreateAgentContainer(config *AgentContainer) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 构造环境变量
	envArgs := []string{}
	for key, value := range config.Environment {
		envArgs = append(envArgs, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	// 构造端口映射
	portArgs := []string{}
	for hostPort, containerPort := range config.Ports {
		portArgs = append(portArgs, "-p", fmt.Sprintf("%d:%d", hostPort, containerPort))
	}

	// 构造卷挂载
	volumeArgs := []string{}
	for _, vol := range config.Volumes {
		roFlag := ""
		if vol.ReadOnly {
			roFlag = ":ro"
		}
		volumeArgs = append(volumeArgs, "-v", fmt.Sprintf("%s:%s%s", vol.HostPath, vol.ContainerPath, roFlag))
	}

	// 构造资源限制
	resourceArgs := []string{}
	if config.Resources.Memory > 0 {
		resourceArgs = append(resourceArgs, "--memory", fmt.Sprintf("%dm", config.Resources.Memory))
	}
	if config.Resources.CPUQuota > 0 {
		resourceArgs = append(resourceArgs, "--cpu-quota", fmt.Sprintf("%d", config.Resources.CPUQuota))
	}

	// 构建 docker run 命令
	args := []string{"run", "-d", "--name", config.Name}
	args = append(args, envArgs...)
	args = append(args, portArgs...)
	args = append(args, volumeArgs...)
	args = append(args, resourceArgs...)
	args = append(args, config.Image)

	// 执行命令
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	// 获取容器 ID
	containerID := strings.TrimSpace(string(output))
	config.ID = containerID
	config.CreatedAt = time.Now()
	config.Status = "running"
	cm.containers[containerID] = config

	log.Printf("Created agent container %s (%s) with ID: %s", config.Name, config.AgentType, config.ID)
	return nil
}

// StartContainer 启动容器
func (cm *ContainerManager) StartContainer(containerID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cmd := exec.Command("docker", "start", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start container: %w, output: %s", err, string(output))
	}

	if container, exists := cm.containers[containerID]; exists {
		now := time.Now()
		container.Status = "running"
		container.StartedAt = &now
	}

	log.Printf("Started container: %s", containerID)
	return nil
}

// StopContainer 停止容器
func (cm *ContainerManager) StopContainer(containerID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cmd := exec.Command("docker", "stop", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop container: %w, output: %s", err, string(output))
	}

	if container, exists := cm.containers[containerID]; exists {
		container.Status = "stopped"
	}

	log.Printf("Stopped container: %s", containerID)
	return nil
}

// RemoveContainer 删除容器
func (cm *ContainerManager) RemoveContainer(containerID string, force bool) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, containerID)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove container: %w, output: %s", err, string(output))
	}

	delete(cm.containers, containerID)
	log.Printf("Removed container: %s", containerID)
	return nil
}

// GetContainer 获取容器信息
func (cm *ContainerManager) GetContainer(containerID string) (*AgentContainer, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	container, exists := cm.containers[containerID]
	if !exists {
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	// 使用 docker inspect 更新状态
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", containerID)
	output, err := cmd.Output()
	if err == nil {
		container.Status = strings.TrimSpace(string(output))
	}

	return container, nil
}

// ListContainers 列出所有容器
func (cm *ContainerManager) ListContainers() []*AgentContainer {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	containers := make([]*AgentContainer, 0, len(cm.containers))
	for _, container := range cm.containers {
		containers = append(containers, container)
	}
	return containers
}

// GetContainerLogs 获取容器日志
func (cm *ContainerManager) GetContainerLogs(containerID string, tail string) (string, error) {
	args := []string{"logs"}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	args = append(args, containerID)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return string(output), nil
}

// ExecInContainer 在容器中执行命令
func (cm *ContainerManager) ExecInContainer(containerID string, command []string) (string, error) {
	args := []string{"exec", containerID}
	args = append(args, command...)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec failed: %w", err)
	}

	return string(output), nil
}

// Close 关闭管理器
func (cm *ContainerManager) Close() error {
	// 清理资源
	return nil
}
