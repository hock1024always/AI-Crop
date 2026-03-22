package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// ============================================
// 本地 AI 集群管理器 - 类似 K8s 的 Master-Worker 架构
// ============================================

// NodeState 节点状态
type NodeState string

const (
	NodeStateReady    NodeState = "Ready"
	NodeStateBusy     NodeState = "Busy"
	NodeStateOffline  NodeState = "Offline"
	NodeStateError    NodeState = "Error"
	NodeStateDraining NodeState = "Draining"
)

// NodeType 节点类型
type NodeType string

const (
	NodeTypeMaster    NodeType = "master"    // 主节点：调度、管理
	NodeTypeWorker    NodeType = "worker"    // 工作节点：执行任务
	NodeTypeInference NodeType = "inference" // 推理节点：运行模型
)

// ModelInfo 模型信息
type ModelInfo struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`        // code/chat/reasoning
	Parameters   string   `json:"parameters"`  // 7B, 13B, 70B
	MemoryReq    int64    `json:"memory_req"`  // 所需内存 (MB)
	Capabilities []string `json:"capabilities"`
	Status       string   `json:"status"`
	Endpoint     string   `json:"endpoint"`
}

// Node 节点定义
type Node struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Type          NodeType          `json:"type"`
	State         NodeState         `json:"state"`
	IPAddress     string            `json:"ip_address"`
	Port          int               `json:"port"`
	Models        []*ModelInfo      `json:"models"`         // 该节点可用的模型
	Resources     ResourceStatus    `json:"resources"`
	Labels        map[string]string `json:"labels"`         // 标签：用于调度
	Annotations   map[string]string `json:"annotations"`    // 注解：额外信息
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	CreatedAt     time.Time         `json:"created_at"`
}

// ResourceStatus 资源状态
type ResourceStatus struct {
	CPUcores    int     `json:"cpu_cores"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryTotal int64   `json:"memory_total"` // MB
	MemoryUsed  int64   `json:"memory_used"`
	GPUCount    int     `json:"gpu_count"`
	GPUMemory   int64   `json:"gpu_memory"`   // MB
	GPUUsed     int64   `json:"gpu_used"`
	DiskTotal   int64   `json:"disk_total"`   // GB
	DiskUsed    int64   `json:"disk_used"`
}

// Workspace 工作区（外包公司部门）
type Workspace struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	DisplayName string        `json:"display_name"`
	Description string        `json:"description"`
	Nodes       []string      `json:"nodes"`        // 属于该工作区的节点
	Agents      []string      `json:"agents"`       // 属于该工作区的 Agent
	Quota       ResourceQuota `json:"quota"`        // 资源配额
	CreatedAt   time.Time     `json:"created_at"`
}

// ResourceQuota 资源配额
type ResourceQuota struct {
	MaxCPU     int   `json:"max_cpu"`
	MaxMemory  int64 `json:"max_memory"` // MB
	MaxGPU     int   `json:"max_gpu"`
	MaxAgents  int   `json:"max_agents"`
	MaxTasks   int   `json:"max_tasks"`
}

// Task 任务定义
type Task struct {
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	Type         string                 `json:"type"`
	Priority     int                    `json:"priority"`
	Status       string                 `json:"status"`
	AssignedNode string                 `json:"assigned_node"`
	AssignedAgent string                `json:"assigned_agent"`
	Workspace    string                 `json:"workspace"`
	Model        string                 `json:"model"`        // 指定使用的模型
	Params       map[string]interface{} `json:"params"`
	Result       interface{}            `json:"result"`
	Error        string                 `json:"error,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
}

// Scheduler 调度器
type Scheduler struct {
	policies map[string]SchedulePolicy
}

// SchedulePolicy 调度策略
type SchedulePolicy struct {
	Name        string
	Description string
	Priority    int
	MatchFunc   func(task *Task, node *Node) bool
	ScoreFunc   func(task *Task, node *Node) int
}

// ClusterManager 集群管理器
type ClusterManager struct {
	nodes      map[string]*Node
	workspaces map[string]*Workspace
	tasks      map[string]*Task
	scheduler  *Scheduler
	
	// 任务队列
	taskQueue   chan *Task
	resultQueue chan *Task
	
	// 事件总线
	events chan Event
	
	mu sync.RWMutex
	ctx context.Context
	cancel context.CancelFunc
}

// Event 事件
type Event struct {
	Type      string      `json:"type"`
	Object    string      `json:"object"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewClusterManager 创建集群管理器
func NewClusterManager() *ClusterManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	cm := &ClusterManager{
		nodes:       make(map[string]*Node),
		workspaces:  make(map[string]*Workspace),
		tasks:       make(map[string]*Task),
		taskQueue:   make(chan *Task, 1000),
		resultQueue: make(chan *Task, 1000),
		events:      make(chan Event, 100),
		scheduler:   NewScheduler(),
		ctx:         ctx,
		cancel:      cancel,
	}
	
	// 初始化默认工作区
	cm.initDefaultWorkspaces()
	
	return cm
}

// NewScheduler 创建调度器
func NewScheduler() *Scheduler {
	s := &Scheduler{
		policies: make(map[string]SchedulePolicy),
	}
	
	// 默认调度策略
	s.RegisterPolicy(SchedulePolicy{
		Name:        "RoundRobin",
		Description: "轮询调度",
		Priority:    100,
		MatchFunc: func(task *Task, node *Node) bool {
			return node.State == NodeStateReady
		},
		ScoreFunc: func(task *Task, node *Node) int {
			return 50
		},
	})
	
	s.RegisterPolicy(SchedulePolicy{
		Name:        "LeastLoaded",
		Description: "最少负载优先",
		Priority:    200,
		MatchFunc: func(task *Task, node *Node) bool {
			return node.State == NodeStateReady
		},
		ScoreFunc: func(task *Task, node *Node) int {
			// CPU 和内存使用率越低分数越高
			cpuScore := int(100 - node.Resources.CPUUsage)
			memScore := int(100 - float64(node.Resources.MemoryUsed) / float64(node.Resources.MemoryTotal) * 100)
			return cpuScore + memScore
		},
	})
	
	s.RegisterPolicy(SchedulePolicy{
		Name:        "ModelAware",
		Description: "模型感知调度",
		Priority:    300,
		MatchFunc: func(task *Task, node *Node) bool {
			if task.Model == "" {
				return node.State == NodeStateReady
			}
			// 检查节点是否有需要的模型
			for _, model := range node.Models {
				if model.Name == task.Model && model.Status == "ready" {
					return node.State == NodeStateReady
				}
			}
			return false
		},
		ScoreFunc: func(task *Task, node *Node) int {
			if task.Model == "" {
				return 50
			}
			for _, model := range node.Models {
				if model.Name == task.Model {
					return 100
				}
			}
			return 0
		},
	})
	
	return s
}

// RegisterPolicy 注册调度策略
func (s *Scheduler) RegisterPolicy(policy SchedulePolicy) {
	s.policies[policy.Name] = policy
}

// initDefaultWorkspaces 初始化默认工作区
func (cm *ClusterManager) initDefaultWorkspaces() {
	// 外包公司部门划分
	workspaces := []Workspace{
		{
			ID:          "frontend",
			Name:        "frontend",
			DisplayName: "前端开发组",
			Description: "负责 Web 前端、移动端开发",
			Quota: ResourceQuota{
				MaxCPU:    4,
				MaxMemory: 16384,
				MaxGPU:    1,
				MaxAgents: 5,
				MaxTasks:  20,
			},
		},
		{
			ID:          "backend",
			Name:        "backend",
			DisplayName: "后端开发组",
			Description: "负责服务端、API、数据库开发",
			Quota: ResourceQuota{
				MaxCPU:    8,
				MaxMemory: 32768,
				MaxGPU:    2,
				MaxAgents: 8,
				MaxTasks:  30,
			},
		},
		{
			ID:          "testing",
			Name:        "testing",
			DisplayName: "测试组",
			Description: "负责自动化测试、质量保证",
			Quota: ResourceQuota{
				MaxCPU:    4,
				MaxMemory: 8192,
				MaxGPU:    0,
				MaxAgents: 4,
				MaxTasks:  50,
			},
		},
		{
			ID:          "devops",
			Name:        "devops",
			DisplayName: "运维组",
			Description: "负责部署、监控、基础设施",
			Quota: ResourceQuota{
				MaxCPU:    4,
				MaxMemory: 8192,
				MaxGPU:    0,
				MaxAgents: 3,
				MaxTasks:  20,
			},
		},
		{
			ID:          "ai",
			Name:        "ai",
			DisplayName: "AI 算法组",
			Description: "负责机器学习、数据分析",
			Quota: ResourceQuota{
				MaxCPU:    8,
				MaxMemory: 65536,
				MaxGPU:    4,
				MaxAgents: 4,
				MaxTasks:  15,
			},
		},
		{
			ID:          "pm",
			Name:        "pm",
			DisplayName: "项目管理组",
			Description: "负责项目规划、进度管理、客户沟通",
			Quota: ResourceQuota{
				MaxCPU:    2,
				MaxMemory: 4096,
				MaxGPU:    0,
				MaxAgents: 2,
				MaxTasks:  30,
			},
		},
	}
	
	for _, ws := range workspaces {
		ws.CreatedAt = time.Now()
		cm.workspaces[ws.ID] = &ws
	}
}

// RegisterNode 注册节点
func (cm *ClusterManager) RegisterNode(node *Node) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	node.CreatedAt = time.Now()
	node.LastHeartbeat = time.Now()
	cm.nodes[node.ID] = node
	
	log.Printf("[Cluster] Node registered: %s (%s)", node.Name, node.Type)
	
	// 发送事件
	cm.events <- Event{
		Type:      "NodeRegistered",
		Object:    node.ID,
		Data:      node,
		Timestamp: time.Now(),
	}
	
	return nil
}

// UnregisterNode 注销节点
func (cm *ClusterManager) UnregisterNode(nodeID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	if node, exists := cm.nodes[nodeID]; exists {
		node.State = NodeStateOffline
		delete(cm.nodes, nodeID)
		log.Printf("[Cluster] Node unregistered: %s", node.Name)
	}
	
	return nil
}

// UpdateNodeHeartbeat 更新节点心跳
func (cm *ClusterManager) UpdateNodeHeartbeat(nodeID string, resources ResourceStatus) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	if node, exists := cm.nodes[nodeID]; exists {
		node.LastHeartbeat = time.Now()
		node.Resources = resources
		
		// 检查资源状态更新节点状态
		if resources.CPUUsage > 90 || float64(resources.MemoryUsed)/float64(resources.MemoryTotal) > 0.9 {
			node.State = NodeStateBusy
		} else if node.State == NodeStateBusy {
			node.State = NodeStateReady
		}
	}
	
	return nil
}

// ListNodes 列出所有节点
func (cm *ClusterManager) ListNodes() []*Node {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	nodes := make([]*Node, 0, len(cm.nodes))
	for _, node := range cm.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetNode 获取节点
func (cm *ClusterManager) GetNode(nodeID string) *Node {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.nodes[nodeID]
}

// ListWorkspaces 列出所有工作区
func (cm *ClusterManager) ListWorkspaces() []*Workspace {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	workspaces := make([]*Workspace, 0, len(cm.workspaces))
	for _, ws := range cm.workspaces {
		workspaces = append(workspaces, ws)
	}
	return workspaces
}

// GetWorkspace 获取工作区
func (cm *ClusterManager) GetWorkspace(workspaceID string) *Workspace {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.workspaces[workspaceID]
}

// SubmitTask 提交任务
func (cm *ClusterManager) SubmitTask(task *Task) error {
	cm.mu.Lock()
	task.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	task.Status = "pending"
	task.CreatedAt = time.Now()
	cm.tasks[task.ID] = task
	cm.mu.Unlock()
	
	// 加入任务队列
	cm.taskQueue <- task
	
	log.Printf("[Cluster] Task submitted: %s (%s)", task.ID, task.Title)
	
	return nil
}

// ScheduleTask 调度任务
func (cm *ClusterManager) ScheduleTask(task *Task) *Node {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	var bestNode *Node
	bestScore := -1
	
	// 遍历所有节点，使用最高优先级的匹配策略
	for _, node := range cm.nodes {
		if node.Type != NodeTypeWorker && node.Type != NodeTypeInference {
			continue
		}
		
		// 检查工作区约束
		if task.Workspace != "" {
			if ws, exists := cm.workspaces[task.Workspace]; exists {
				nodeInWorkspace := false
				for _, nodeID := range ws.Nodes {
					if nodeID == node.ID {
						nodeInWorkspace = true
						break
					}
				}
				if !nodeInWorkspace {
					continue
				}
			}
		}
		
		// 应用调度策略
		for _, policy := range cm.scheduler.policies {
			if policy.MatchFunc(task, node) {
				score := policy.ScoreFunc(task, node)
				if score > bestScore {
					bestScore = score
					bestNode = node
				}
			}
		}
	}
	
	return bestNode
}

// Run 启动集群管理器
func (cm *ClusterManager) Run() {
	log.Println("[Cluster] Starting cluster manager...")
	
	// 启动任务调度循环
	go cm.taskScheduler()
	
	// 启动心跳检测
	go cm.healthCheck()
	
	// 启动事件处理
	go cm.eventHandler()
	
	log.Println("[Cluster] Cluster manager started")
}

// taskScheduler 任务调度循环
func (cm *ClusterManager) taskScheduler() {
	for {
		select {
		case task := <-cm.taskQueue:
			// 调度任务
			node := cm.ScheduleTask(task)
			if node == nil {
				log.Printf("[Cluster] No available node for task: %s", task.ID)
				task.Status = "failed"
				task.Error = "No available node"
				cm.resultQueue <- task
				continue
			}
			
			// 分配任务到节点
			cm.mu.Lock()
			task.AssignedNode = node.ID
			task.Status = "scheduled"
			now := time.Now()
			task.StartedAt = &now
			node.State = NodeStateBusy
			cm.mu.Unlock()
			
			log.Printf("[Cluster] Task %s scheduled to node %s", task.ID, node.Name)
			
			// 发送事件
			cm.events <- Event{
				Type:      "TaskScheduled",
				Object:    task.ID,
				Data:      task,
				Timestamp: time.Now(),
			}
			
		case task := <-cm.resultQueue:
			// 处理任务结果
			cm.mu.Lock()
			if storedTask, exists := cm.tasks[task.ID]; exists {
				storedTask.Status = task.Status
				storedTask.Result = task.Result
				storedTask.Error = task.Error
				now := time.Now()
				storedTask.CompletedAt = &now
				
				// 释放节点
				if node := cm.nodes[task.AssignedNode]; node != nil {
					node.State = NodeStateReady
				}
			}
			cm.mu.Unlock()
			
			log.Printf("[Cluster] Task %s completed with status: %s", task.ID, task.Status)
			
		case <-cm.ctx.Done():
			return
		}
	}
}

// healthCheck 心跳检测
func (cm *ClusterManager) healthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			cm.mu.Lock()
			for _, node := range cm.nodes {
				if time.Since(node.LastHeartbeat) > 60*time.Second {
					node.State = NodeStateOffline
					log.Printf("[Cluster] Node %s marked as offline (heartbeat timeout)", node.Name)
				}
			}
			cm.mu.Unlock()
			
		case <-cm.ctx.Done():
			return
		}
	}
}

// eventHandler 事件处理器
func (cm *ClusterManager) eventHandler() {
	for {
		select {
		case event := <-cm.events:
			// 可以在这里处理事件，如发送到 WebSocket、记录日志等
			eventJSON, _ := json.Marshal(event)
			log.Printf("[Event] %s", string(eventJSON))
			
		case <-cm.ctx.Done():
			return
		}
	}
}

// Shutdown 关闭集群管理器
func (cm *ClusterManager) Shutdown() {
	log.Println("[Cluster] Shutting down cluster manager...")
	cm.cancel()
}

// GetClusterStatus 获取集群状态
func (cm *ClusterManager) GetClusterStatus() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	totalNodes := len(cm.nodes)
	readyNodes := 0
	busyNodes := 0
	offlineNodes := 0
	
	for _, node := range cm.nodes {
		switch node.State {
		case NodeStateReady:
			readyNodes++
		case NodeStateBusy:
			busyNodes++
		case NodeStateOffline:
			offlineNodes++
		}
	}
	
	totalTasks := len(cm.tasks)
	pendingTasks := 0
	runningTasks := 0
	completedTasks := 0
	
	for _, task := range cm.tasks {
		switch task.Status {
		case "pending":
			pendingTasks++
		case "scheduled", "running":
			runningTasks++
		case "completed":
			completedTasks++
		}
	}
	
	return map[string]interface{}{
		"nodes": map[string]int{
			"total":   totalNodes,
			"ready":   readyNodes,
			"busy":    busyNodes,
			"offline": offlineNodes,
		},
		"tasks": map[string]int{
			"total":     totalTasks,
			"pending":   pendingTasks,
			"running":   runningTasks,
			"completed": completedTasks,
		},
		"workspaces": len(cm.workspaces),
	}
}
