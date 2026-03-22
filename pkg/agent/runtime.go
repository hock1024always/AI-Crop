package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"ai-corp/pkg/message"
	"ai-corp/pkg/skill"
)

// Runtime Agent 运行时
type Runtime struct {
	// 基本信息
	ID       string
	Name     string
	Type     string // developer/tester/architect/devops

	// 配置
	Config RuntimeConfig

	// 组件
	bus      message.MessageBus
	messenger *AgentMessenger
	registry  *skill.Registry
	state     *State

	// 任务处理
	runningTasks map[string]context.CancelFunc

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
}

// RuntimeConfig 运行时配置
type RuntimeConfig struct {
	OrchestratorURL   string
	NATSURL           string
	LLMAPIKey         string
	WorkspaceDir      string
	MaxConcurrent     int
	HeartbeatInterval time.Duration
}

// State Agent 状态
type State struct {
	Status      string                 `json:"status"` // idle/busy/offline
	CurrentTask string                 `json:"current_task,omitempty"`
	Stats       map[string]interface{} `json:"stats"`
	mu          sync.RWMutex
}

// Task 任务定义
type Task struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Content  map[string]interface{} `json:"content"`
	Deadline *time.Time             `json:"deadline,omitempty"`
	Priority int                    `json:"priority"`
}

// NewRuntime 创建 Agent 运行时
func NewRuntime(id, name, agentType string, config RuntimeConfig) (*Runtime, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建消息总线
	var bus message.MessageBus
	var err error

	if config.NATSURL != "" {
		bus, err = message.NewNATSMessageBus(config.NATSURL)
		if err != nil {
			log.Printf("Failed to connect to NATS, using in-memory bus: %v", err)
			bus = message.NewInMemoryMessageBus()
		}
	} else {
		bus = message.NewInMemoryMessageBus()
	}

	// 创建消息通信器
	messenger := NewAgentMessenger(id, agentType, bus)

	// 创建 Skill 注册表
	registry := skill.NewRegistry()

	runtime := &Runtime{
		ID:           id,
		Name:         name,
		Type:         agentType,
		Config:       config,
		bus:          bus,
		messenger:    messenger,
		registry:     registry,
		state: &State{
			Status: "idle",
			Stats:  make(map[string]interface{}),
		},
		runningTasks: make(map[string]context.CancelFunc),
		ctx:          ctx,
		cancel:       cancel,
	}

	return runtime, nil
}

// Run 启动运行时
func (r *Runtime) Run() error {
	log.Printf("[%s] Agent runtime starting...", r.ID)

	// 注册消息处理器
	r.messenger.RegisterHandler(message.MessageTypeTaskAssign, r.handleTaskAssign)

	// 订阅任务
	go r.subscribeToTasks()

	// 启动心跳
	go r.startHeartbeat()

	log.Printf("[%s] Agent runtime started, waiting for tasks...", r.ID)

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Printf("[%s] Received shutdown signal", r.ID)
	case <-r.ctx.Done():
		log.Printf("[%s] Context cancelled", r.ID)
	}

	return r.Shutdown()
}

// subscribeToTasks 订阅任务
func (r *Runtime) subscribeToTasks() {
	// 订阅直接分配给该 Agent 的任务
	subject := fmt.Sprintf("task.assign.*.*.%s", r.ID)
	r.bus.Subscribe(subject, func(msg *message.Message) error {
		return r.handleTaskAssign(msg)
	})

	// 订阅广播给该类型 Agent 的任务
	broadcastSubject := fmt.Sprintf("task.assign.%s", r.Type)
	r.bus.Subscribe(broadcastSubject, func(msg *message.Message) error {
		return r.handleTaskAssign(msg)
	})

	<-r.ctx.Done()
}

// handleTaskAssign 处理任务分配
func (r *Runtime) handleTaskAssign(msg *message.Message) error {
	// 检查是否可以接受任务
	if !r.canAcceptTask() {
		log.Printf("[%s] Cannot accept task %s, currently busy", r.ID, msg.TaskID)
		return nil
	}

	log.Printf("[%s] Received task %s", r.ID, msg.TaskID)

	// 解析任务
	task := &Task{
		ID:      msg.TaskID,
		Content: msg.Content,
	}

	if taskType, ok := msg.Content["task_type"].(string); ok {
		task.Type = taskType
	}

	if priority, ok := msg.Content["priority"].(int); ok {
		task.Priority = priority
	}

	// 启动任务处理
	go r.processTask(task)

	return nil
}

// processTask 处理任务
func (r *Runtime) processTask(task *Task) {
	// 创建任务上下文
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()

	// 记录正在运行的任务
	r.runningTasks[task.ID] = cancel
	defer delete(r.runningTasks, task.ID)

	// 更新状态
	r.setBusy(task.ID)

	// 报告任务开始
	r.reportProgress(task.ID, 0, "Task started")

	// 执行任务
	result, err := r.executeTask(ctx, task)

	// 报告结果
	if err != nil {
		log.Printf("[%s] Task %s failed: %v", r.ID, task.ID, err)
		r.reportFailure(task.ID, err.Error())
	} else {
		log.Printf("[%s] Task %s completed", r.ID, task.ID)
		r.reportCompletion(task.ID, result)
	}

	// 恢复状态
	r.setIdle()
}

// executeTask 执行任务
func (r *Runtime) executeTask(ctx context.Context, task *Task) (map[string]interface{}, error) {
	// 根据任务类型选择执行方式
	switch task.Type {
	case "skill":
		return r.executeSkillTask(ctx, task)
	case "llm":
		return r.executeLLMTask(ctx, task)
	case "composite":
		return r.executeCompositeTask(ctx, task)
	default:
		// 默认尝试使用 skill
		return r.executeSkillTask(ctx, task)
	}
}

// executeSkillTask 执行 Skill 任务
func (r *Runtime) executeSkillTask(ctx context.Context, task *Task) (map[string]interface{}, error) {
	// 获取 skill 名称
	skillName, ok := task.Content["skill"].(string)
	if !ok {
		return nil, fmt.Errorf("skill name not specified")
	}

	// 获取参数
	params, _ := task.Content["params"].(map[string]interface{})

	// 报告进度
	r.reportProgress(task.ID, 30, fmt.Sprintf("Executing skill: %s", skillName))

	// 执行 skill
	result, err := r.registry.Execute(ctx, skillName, params)
	if err != nil {
		return nil, fmt.Errorf("skill execution failed: %w", err)
	}

	// 报告进度
	r.reportProgress(task.ID, 80, "Skill execution completed")

	return result, nil
}

// executeLLMTask 执行 LLM 任务
func (r *Runtime) executeLLMTask(ctx context.Context, task *Task) (map[string]interface{}, error) {
	// 报告进度
	r.reportProgress(task.ID, 20, "Calling LLM")

	// TODO: 实现 LLM 调用
	// 这里使用模拟实现
	time.Sleep(2 * time.Second)

	result := map[string]interface{}{
		"message":    "LLM task completed",
		"agent_type": r.Type,
		"task_id":    task.ID,
	}

	// 报告进度
	r.reportProgress(task.ID, 80, "LLM response received")

	return result, nil
}

// executeCompositeTask 执行复合任务
func (r *Runtime) executeCompositeTask(ctx context.Context, task *Task) (map[string]interface{}, error) {
	// 复合任务需要分解为多个子任务
	subTasks, ok := task.Content["sub_tasks"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("sub_tasks not specified")
	}

	results := make([]map[string]interface{}, 0, len(subTasks))

	for i, subTask := range subTasks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		subTaskMap, ok := subTask.(map[string]interface{})
		if !ok {
			continue
		}

		// 报告进度
		progress := (i + 1) * 100 / len(subTasks)
		r.reportProgress(task.ID, progress, fmt.Sprintf("Executing sub-task %d/%d", i+1, len(subTasks)))

		// 执行子任务
		subResult, err := r.executeTask(ctx, &Task{
			ID:      fmt.Sprintf("%s-sub-%d", task.ID, i),
			Type:    getString(subTaskMap, "type", "skill"),
			Content: subTaskMap,
		})
		if err != nil {
			return nil, fmt.Errorf("sub-task %d failed: %w", i, err)
		}

		results = append(results, subResult)
	}

	return map[string]interface{}{
		"sub_results": results,
		"total":       len(subTasks),
	}, nil
}

// canAcceptTask 检查是否可以接受任务
func (r *Runtime) canAcceptTask() bool {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()

	return r.state.Status == "idle"
}

// setBusy 设置为忙碌状态
func (r *Runtime) setBusy(taskID string) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()

	r.state.Status = "busy"
	r.state.CurrentTask = taskID
}

// setIdle 设置为空闲状态
func (r *Runtime) setIdle() {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()

	r.state.Status = "idle"
	r.state.CurrentTask = ""
}

// GetState 获取当前状态
func (r *Runtime) GetState() State {
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()

	return *r.state
}

// CancelTask 取消任务
func (r *Runtime) CancelTask(taskID string) error {
	if cancel, exists := r.runningTasks[taskID]; exists {
		cancel()
		return nil
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// Shutdown 关闭运行时
func (r *Runtime) Shutdown() error {
	log.Printf("[%s] Shutting down...", r.ID)

	// 取消所有任务
	for taskID, cancel := range r.runningTasks {
		log.Printf("[%s] Cancelling task: %s", r.ID, taskID)
		cancel()
	}

	// 关闭上下文
	r.cancel()

	// 关闭消息总线
	if err := r.bus.Close(); err != nil {
		log.Printf("[%s] Failed to close message bus: %v", r.ID, err)
	}

	log.Printf("[%s] Shutdown complete", r.ID)
	return nil
}

// RegisterSkill 注册自定义 Skill
func (r *Runtime) RegisterSkill(s *skill.Skill) error {
	return r.registry.Register(s)
}

// reportProgress 报告进度
func (r *Runtime) reportProgress(taskID string, progress int, detail string) {
	msg := &message.Message{
		Type:   message.MessageTypeTaskProgress,
		From:   r.ID,
		TaskID: taskID,
		Content: map[string]interface{}{
			"progress":   progress,
			"detail":     detail,
			"agent_type": r.Type,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	if err := r.bus.Publish(r.ctx, msg); err != nil {
		log.Printf("[%s] Failed to report progress: %v", r.ID, err)
	}
}

// reportCompletion 报告完成
func (r *Runtime) reportCompletion(taskID string, result map[string]interface{}) {
	msg := &message.Message{
		Type:   message.MessageTypeTaskComplete,
		From:   r.ID,
		TaskID: taskID,
		Content: map[string]interface{}{
			"result":       result,
			"agent_type":   r.Type,
			"completed_at": time.Now().UnixMilli(),
		},
		Timestamp: time.Now().UnixMilli(),
	}

	if err := r.bus.Publish(r.ctx, msg); err != nil {
		log.Printf("[%s] Failed to report completion: %v", r.ID, err)
	}
}

// reportFailure 报告失败
func (r *Runtime) reportFailure(taskID string, reason string) {
	msg := &message.Message{
		Type:   message.MessageTypeTaskFail,
		From:   r.ID,
		TaskID: taskID,
		Content: map[string]interface{}{
			"reason":     reason,
			"agent_type": r.Type,
			"failed_at":  time.Now().UnixMilli(),
		},
		Timestamp: time.Now().UnixMilli(),
	}

	if err := r.bus.Publish(r.ctx, msg); err != nil {
		log.Printf("[%s] Failed to report failure: %v", r.ID, err)
	}
}

// startHeartbeat 启动心跳
func (r *Runtime) startHeartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			msg := &message.Message{
				Type:    message.MessageTypeHeartbeat,
				From:    r.ID,
				Content: map[string]interface{}{
					"agent_type": r.Type,
					"status":     r.state.Status,
					"timestamp":  time.Now().UnixMilli(),
				},
				Timestamp: time.Now().UnixMilli(),
			}

			if err := r.bus.Publish(r.ctx, msg); err != nil {
				log.Printf("[%s] Failed to send heartbeat: %v", r.ID, err)
			}

		case <-r.ctx.Done():
			return
		}
	}
}

// 辅助函数
func getString(m map[string]interface{}, key, defaultValue string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultValue
}

// AgentMessenger 简化的 Agent 消息通信器
type AgentMessenger struct {
	agentID   string
	agentType string
	bus       message.MessageBus
	handlers  map[message.MessageType]message.MessageHandler
	mu        sync.RWMutex
}

// NewAgentMessenger 创建 Agent 消息通信器
func NewAgentMessenger(agentID, agentType string, bus message.MessageBus) *AgentMessenger {
	return &AgentMessenger{
		agentID:   agentID,
		agentType: agentType,
		bus:       bus,
		handlers:  make(map[message.MessageType]message.MessageHandler),
	}
}

// RegisterHandler 注册消息处理器
func (m *AgentMessenger) RegisterHandler(msgType message.MessageType, handler message.MessageHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[msgType] = handler
}
