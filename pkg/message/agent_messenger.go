package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// AgentMessenger Agent 消息通信器
// 封装了 Agent 常用的消息通信模式
type AgentMessenger struct {
	agentID   string
	agentType string
	bus       MessageBus

	// 消息处理器
	handlers map[MessageType]MessageHandler

	// 待处理的请求
	pendingRequests map[string]chan *Message
	mu              sync.RWMutex

	// 上下文
	ctx    context.Context
	cancel context.CancelFunc
}

// NewAgentMessenger 创建 Agent 消息通信器
func NewAgentMessenger(agentID, agentType string, bus MessageBus) *AgentMessenger {
	ctx, cancel := context.WithCancel(context.Background())

	m := &AgentMessenger{
		agentID:         agentID,
		agentType:       agentType,
		bus:             bus,
		handlers:        make(map[MessageType]MessageHandler),
		pendingRequests: make(map[string]chan *Message),
		ctx:             ctx,
		cancel:          cancel,
	}

	// 启动消息监听
	go m.startListening()

	// 启动心跳
	go m.startHeartbeat()

	return m
}

// RegisterHandler 注册消息处理器
func (m *AgentMessenger) RegisterHandler(msgType MessageType, handler MessageHandler) {
	m.handlers[msgType] = handler
}

// SendTask 发送任务给指定 Agent
func (m *AgentMessenger) SendTask(to string, taskID string, content map[string]interface{}) error {
	msg := &Message{
		Type:      MessageTypeTaskAssign,
		From:      m.agentID,
		To:        to,
		TaskID:    taskID,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// BroadcastTask 广播任务
func (m *AgentMessenger) BroadcastTask(taskID string, content map[string]interface{}) error {
	msg := &Message{
		Type:      MessageTypeTaskAssign,
		From:      m.agentID,
		To:        "", // 广播
		TaskID:    taskID,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// ReportProgress 报告任务进度
func (m *AgentMessenger) ReportProgress(taskID string, progress int, detail string) error {
	msg := &Message{
		Type:   MessageTypeTaskProgress,
		From:   m.agentID,
		TaskID: taskID,
		Content: map[string]interface{}{
			"progress": progress,
			"detail":   detail,
			"agent_type": m.agentType,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// CompleteTask 完成任务
func (m *AgentMessenger) CompleteTask(taskID string, result map[string]interface{}) error {
	msg := &Message{
		Type:   MessageTypeTaskComplete,
		From:   m.agentID,
		TaskID: taskID,
		Content: map[string]interface{}{
			"result":     result,
			"agent_type": m.agentType,
			"completed_at": time.Now().UnixMilli(),
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// FailTask 任务失败
func (m *AgentMessenger) FailTask(taskID string, reason string) error {
	msg := &Message{
		Type:   MessageTypeTaskFail,
		From:   m.agentID,
		TaskID: taskID,
		Content: map[string]interface{}{
			"reason":     reason,
			"agent_type": m.agentType,
			"failed_at":  time.Now().UnixMilli(),
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// RequestHelp 请求协助
func (m *AgentMessenger) RequestHelp(to string, taskID string, helpType string, detail string) (*Message, error) {
	requestID := generateID()

	msg := &Message{
		ID:      requestID,
		Type:    MessageTypeRequest,
		From:    m.agentID,
		To:      to,
		TaskID:  taskID,
		Content: map[string]interface{}{
			"help_type": helpType,
			"detail":    detail,
			"request_id": requestID,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	// 创建响应通道
	responseChan := make(chan *Message, 1)
	m.mu.Lock()
	m.pendingRequests[requestID] = responseChan
	m.mu.Unlock()

	// 发送请求
	if err := m.bus.Publish(m.ctx, msg); err != nil {
		m.mu.Lock()
		delete(m.pendingRequests, requestID)
		m.mu.Unlock()
		return nil, err
	}

	// 等待响应
	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(30 * time.Second):
		m.mu.Lock()
		delete(m.pendingRequests, requestID)
		m.mu.Unlock()
		return nil, fmt.Errorf("request timeout")
	}
}

// ReplyHelp 回复协助请求
func (m *AgentMessenger) ReplyHelp(requestMsg *Message, content map[string]interface{}) error {
	msg := &Message{
		Type:    MessageTypeResponse,
		From:    m.agentID,
		To:      requestMsg.From,
		TaskID:  requestMsg.TaskID,
		Content: content,
		Metadata: map[string]interface{}{
			"in_reply_to": requestMsg.ID,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// SubscribeToTasks 订阅任务消息
func (m *AgentMessenger) SubscribeToTasks(handler MessageHandler) error {
	// 订阅分配给该 Agent 的任务
	subject := fmt.Sprintf("task.assign.*.*.%s", m.agentID)
	_, err := m.bus.Subscribe(subject, handler)
	return err
}

// SubscribeToBroadcasts 订阅广播消息
func (m *AgentMessenger) SubscribeToBroadcasts(handler MessageHandler) error {
	// 订阅广播任务
	subject := fmt.Sprintf("task.assign.%s.%s", m.agentType, m.agentID)
	_, err := m.bus.Subscribe(subject, handler)
	return err
}

// startListening 启动消息监听
func (m *AgentMessenger) startListening() {
	// 订阅直接消息
	directSubject := fmt.Sprintf("*.*.*.%s", m.agentID)
	m.bus.Subscribe(directSubject, func(msg *Message) error {
		return m.handleMessage(msg)
	})

	// 订阅广播消息
	broadcastSubject := fmt.Sprintf("*.*.%s", m.agentID)
	m.bus.Subscribe(broadcastSubject, func(msg *Message) error {
		return m.handleMessage(msg)
	})

	<-m.ctx.Done()
}

// handleMessage 处理接收到的消息
func (m *AgentMessenger) handleMessage(msg *Message) error {
	// 检查是否是响应
	if msg.Type == MessageTypeResponse {
		if requestID, ok := msg.Metadata["in_reply_to"].(string); ok {
			m.mu.RLock()
			ch, exists := m.pendingRequests[requestID]
			m.mu.RUnlock()

			if exists {
				ch <- msg
				m.mu.Lock()
				delete(m.pendingRequests, requestID)
				m.mu.Unlock()
				return nil
			}
		}
	}

	// 调用注册的处理器
	if handler, exists := m.handlers[msg.Type]; exists {
		return handler(msg)
	}

	// 默认处理
	log.Printf("[%s] Received message: type=%s, from=%s, task=%s",
		m.agentID, msg.Type, msg.From, msg.TaskID)

	return nil
}

// startHeartbeat 启动心跳
func (m *AgentMessenger) startHeartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			msg := &Message{
				Type:    MessageTypeHeartbeat,
				From:    m.agentID,
				Content: map[string]interface{}{
					"agent_type": m.agentType,
					"status":     "healthy",
					"timestamp":  time.Now().UnixMilli(),
				},
				Timestamp: time.Now().UnixMilli(),
			}

			if err := m.bus.Publish(m.ctx, msg); err != nil {
				log.Printf("[%s] Failed to send heartbeat: %v", m.agentID, err)
			}

		case <-m.ctx.Done():
			return
		}
	}
}

// Close 关闭通信器
func (m *AgentMessenger) Close() error {
	m.cancel()
	return nil
}

// OrchestratorMessenger Orchestrator 消息通信器
type OrchestratorMessenger struct {
	bus       MessageBus
	handlers  map[MessageType]MessageHandler
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewOrchestratorMessenger 创建 Orchestrator 消息通信器
func NewOrchestratorMessenger(bus MessageBus) *OrchestratorMessenger {
	ctx, cancel := context.WithCancel(context.Background())

	m := &OrchestratorMessenger{
		bus:      bus,
		handlers: make(map[MessageType]MessageHandler),
		ctx:      ctx,
		cancel:   cancel,
	}

	// 启动消息监听
	go m.startListening()

	return m
}

// RegisterHandler 注册消息处理器
func (m *OrchestratorMessenger) RegisterHandler(msgType MessageType, handler MessageHandler) {
	m.handlers[msgType] = handler
}

// BroadcastTask 广播任务
func (m *OrchestratorMessenger) BroadcastTask(taskID string, taskType string, content map[string]interface{}) error {
	msg := &Message{
		Type:      MessageTypeTaskAssign,
		From:      "orchestrator",
		To:        "", // 广播
		TaskID:    taskID,
		Content: map[string]interface{}{
			"task_type": taskType,
			"data":      content,
		},
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// AssignTask 分配任务给指定 Agent
func (m *OrchestratorMessenger) AssignTask(to string, taskID string, content map[string]interface{}) error {
	msg := &Message{
		Type:      MessageTypeTaskAssign,
		From:      "orchestrator",
		To:        to,
		TaskID:    taskID,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}

	return m.bus.Publish(m.ctx, msg)
}

// SubscribeToProgress 订阅任务进度
func (m *OrchestratorMessenger) SubscribeToProgress(handler func(taskID string, progress int, detail string)) {
	m.RegisterHandler(MessageTypeTaskProgress, func(msg *Message) error {
		progress, _ := msg.Content["progress"].(int)
		detail, _ := msg.Content["detail"].(string)
		handler(msg.TaskID, progress, detail)
		return nil
	})
}

// SubscribeToCompletions 订阅任务完成
func (m *OrchestratorMessenger) SubscribeToCompletions(handler func(taskID string, result map[string]interface{})) {
	m.RegisterHandler(MessageTypeTaskComplete, func(msg *Message) error {
		result, _ := msg.Content["result"].(map[string]interface{})
		handler(msg.TaskID, result)
		return nil
	})
}

// SubscribeToFailures 订阅任务失败
func (m *OrchestratorMessenger) SubscribeToFailures(handler func(taskID string, reason string)) {
	m.RegisterHandler(MessageTypeTaskFail, func(msg *Message) error {
		reason, _ := msg.Content["reason"].(string)
		handler(msg.TaskID, reason)
		return nil
	})
}

// startListening 启动消息监听
func (m *OrchestratorMessenger) startListening() {
	// 订阅所有消息
	m.bus.Subscribe(">", func(msg *Message) error {
		return m.handleMessage(msg)
	})

	<-m.ctx.Done()
}

// handleMessage 处理消息
func (m *OrchestratorMessenger) handleMessage(msg *Message) error {
	m.mu.RLock()
	handler, exists := m.handlers[msg.Type]
	m.mu.RUnlock()

	if exists {
		return handler(msg)
	}

	return nil
}

// Close 关闭通信器
func (m *OrchestratorMessenger) Close() error {
	m.cancel()
	return nil
}

// MessageLogger 消息日志记录器
type MessageLogger struct {
	bus MessageBus
}

// NewMessageLogger 创建消息日志记录器
func NewMessageLogger(bus MessageBus) *MessageLogger {
	return &MessageLogger{bus: bus}
}

// Start 开始记录日志
func (l *MessageLogger) Start() {
	l.bus.Subscribe(">", func(msg *Message) error {
		data, _ := json.Marshal(msg)
		log.Printf("[MESSAGE] %s", string(data))
		return nil
	})
}
