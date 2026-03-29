// Package message - A2A (Agent-to-Agent) 协议
// 支持 Agent 间点对点通信、协作请求/响应、任务协同
package message

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// A2AProtocol Agent-to-Agent 通信协议
type A2AProtocol struct {
	bus         MessageBus
	agents      map[string]*AgentEndpoint // agentID -> endpoint
	handlers    map[string]A2AHandler     // messageType -> handler
	sessions    map[string]*CollabSession // sessionID -> session
	mu          sync.RWMutex
}

// AgentEndpoint Agent 端点信息
type AgentEndpoint struct {
	AgentID      string   `json:"agent_id"`
	AgentType    string   `json:"agent_type"`
	Capabilities []string `json:"capabilities"`
	Status       string   `json:"status"` // online, busy, offline
	RegisteredAt int64    `json:"registered_at"`
}

// A2AHandler Agent 间消息处理函数
type A2AHandler func(ctx context.Context, msg *Message) (*Message, error)

// CollabSession 协作会话
type CollabSession struct {
	ID          string                 `json:"id"`
	InitiatorID string                 `json:"initiator_id"`
	TargetIDs   []string               `json:"target_ids"`
	TaskID      string                 `json:"task_id"`
	Type        CollabType             `json:"type"`
	Status      string                 `json:"status"` // pending, active, completed, failed
	Messages    []*Message             `json:"messages"`
	Context     map[string]interface{} `json:"context"`
	CreatedAt   int64                  `json:"created_at"`
	UpdatedAt   int64                  `json:"updated_at"`
	mu          sync.Mutex
}

// CollabType 协作类型
type CollabType string

const (
	CollabTypeReview     CollabType = "code_review"    // 代码审查请求
	CollabTypeAssist     CollabType = "assist"         // 请求协助
	CollabTypeHandoff    CollabType = "handoff"        // 任务交接
	CollabTypeConsult    CollabType = "consult"        // 技术咨询
	CollabTypeBroadcast  CollabType = "broadcast"      // 广播通知
)

// NewA2AProtocol 创建 A2A 协议实例
func NewA2AProtocol(bus MessageBus) *A2AProtocol {
	return &A2AProtocol{
		bus:      bus,
		agents:   make(map[string]*AgentEndpoint),
		handlers: make(map[string]A2AHandler),
		sessions: make(map[string]*CollabSession),
	}
}

// RegisterAgent 注册 Agent 端点
func (p *A2AProtocol) RegisterAgent(endpoint *AgentEndpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	endpoint.Status = "online"
	endpoint.RegisteredAt = time.Now().UnixMilli()
	p.agents[endpoint.AgentID] = endpoint

	log.Printf("[A2A] Agent registered: %s (type=%s, capabilities=%v)",
		endpoint.AgentID, endpoint.AgentType, endpoint.Capabilities)
}

// UnregisterAgent 注销 Agent 端点
func (p *A2AProtocol) UnregisterAgent(agentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.agents, agentID)
	log.Printf("[A2A] Agent unregistered: %s", agentID)
}

// RegisterHandler 注册消息类型处理器
func (p *A2AProtocol) RegisterHandler(msgType string, handler A2AHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[msgType] = handler
}

// SendDirect 点对点发送消息
func (p *A2AProtocol) SendDirect(ctx context.Context, from, to string, msgType MessageType, content map[string]interface{}) error {
	p.mu.RLock()
	_, fromExists := p.agents[from]
	_, toExists := p.agents[to]
	p.mu.RUnlock()

	if !fromExists {
		return fmt.Errorf("sender agent not registered: %s", from)
	}
	if !toExists {
		return fmt.Errorf("target agent not registered: %s", to)
	}

	msg := &Message{
		ID:        fmt.Sprintf("a2a-%d", time.Now().UnixNano()),
		Type:      msgType,
		From:      from,
		To:        to,
		Content:   content,
		Metadata:  map[string]interface{}{"protocol": "a2a"},
		Timestamp: time.Now().UnixMilli(),
		Priority:  5,
	}

	return p.bus.Publish(ctx, msg)
}

// RequestCollab 发起协作请求
func (p *A2AProtocol) RequestCollab(ctx context.Context, initiatorID string, targetIDs []string, collabType CollabType, taskID string, content map[string]interface{}) (*CollabSession, error) {
	session := &CollabSession{
		ID:          fmt.Sprintf("collab-%d", time.Now().UnixNano()),
		InitiatorID: initiatorID,
		TargetIDs:   targetIDs,
		TaskID:      taskID,
		Type:        collabType,
		Status:      "pending",
		Messages:    []*Message{},
		Context:     content,
		CreatedAt:   time.Now().UnixMilli(),
		UpdatedAt:   time.Now().UnixMilli(),
	}

	p.mu.Lock()
	p.sessions[session.ID] = session
	p.mu.Unlock()

	// 向所有目标 Agent 发送协作请求
	for _, targetID := range targetIDs {
		reqContent := map[string]interface{}{
			"session_id":  session.ID,
			"collab_type": string(collabType),
			"task_id":     taskID,
			"initiator":   initiatorID,
		}
		for k, v := range content {
			reqContent[k] = v
		}

		if err := p.SendDirect(ctx, initiatorID, targetID, MessageTypeRequest, reqContent); err != nil {
			log.Printf("[A2A] Failed to send collab request to %s: %v", targetID, err)
		}
	}

	log.Printf("[A2A] Collaboration session created: %s (type=%s, initiator=%s, targets=%v)",
		session.ID, collabType, initiatorID, targetIDs)

	return session, nil
}

// RespondCollab 响应协作请求
func (p *A2AProtocol) RespondCollab(ctx context.Context, sessionID, responderID string, accepted bool, content map[string]interface{}) error {
	p.mu.Lock()
	session, exists := p.sessions[sessionID]
	p.mu.Unlock()

	if !exists {
		return fmt.Errorf("collaboration session not found: %s", sessionID)
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	status := "accepted"
	if !accepted {
		status = "rejected"
	}

	respContent := map[string]interface{}{
		"session_id":  sessionID,
		"status":      status,
		"responder":   responderID,
	}
	for k, v := range content {
		respContent[k] = v
	}

	msg := &Message{
		ID:        fmt.Sprintf("resp-%d", time.Now().UnixNano()),
		Type:      MessageTypeResponse,
		From:      responderID,
		To:        session.InitiatorID,
		SessionID: sessionID,
		Content:   respContent,
		Timestamp: time.Now().UnixMilli(),
	}

	session.Messages = append(session.Messages, msg)
	session.UpdatedAt = time.Now().UnixMilli()

	if accepted {
		session.Status = "active"
	}

	return p.bus.Publish(ctx, msg)
}

// CompleteSession 完成协作会话
func (p *A2AProtocol) CompleteSession(sessionID string, result map[string]interface{}) {
	p.mu.Lock()
	session, exists := p.sessions[sessionID]
	p.mu.Unlock()

	if !exists {
		return
	}

	session.mu.Lock()
	session.Status = "completed"
	session.Context["result"] = result
	session.UpdatedAt = time.Now().UnixMilli()
	session.mu.Unlock()

	log.Printf("[A2A] Collaboration session completed: %s", sessionID)
}

// FindAgentsByCapability 按能力查找 Agent
func (p *A2AProtocol) FindAgentsByCapability(capability string) []*AgentEndpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*AgentEndpoint
	for _, agent := range p.agents {
		if agent.Status != "online" {
			continue
		}
		for _, cap := range agent.Capabilities {
			if cap == capability {
				result = append(result, agent)
				break
			}
		}
	}
	return result
}

// FindAgentsByType 按类型查找 Agent
func (p *A2AProtocol) FindAgentsByType(agentType string) []*AgentEndpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*AgentEndpoint
	for _, agent := range p.agents {
		if agent.AgentType == agentType && agent.Status == "online" {
			result = append(result, agent)
		}
	}
	return result
}

// ListSessions 列出协作会话
func (p *A2AProtocol) ListSessions(status string) []*CollabSession {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*CollabSession
	for _, s := range p.sessions {
		if status == "" || s.Status == status {
			result = append(result, s)
		}
	}
	return result
}

// GetSession 获取协作会话
func (p *A2AProtocol) GetSession(sessionID string) *CollabSession {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sessions[sessionID]
}

// OnlineAgents 返回在线 Agent 列表
func (p *A2AProtocol) OnlineAgents() []*AgentEndpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*AgentEndpoint
	for _, agent := range p.agents {
		if agent.Status == "online" {
			result = append(result, agent)
		}
	}
	return result
}
