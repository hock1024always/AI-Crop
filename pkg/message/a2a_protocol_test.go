package message

import (
	"context"
	"testing"
)

func newTestBus() MessageBus {
	return NewInMemoryMessageBus()
}

func TestA2ARegisterAgent(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{
		AgentID:      "agent-1",
		AgentType:    "developer",
		Capabilities: []string{"go", "python"},
	})

	agents := a2a.OnlineAgents()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].AgentID != "agent-1" {
		t.Errorf("agent_id: got %q, want %q", agents[0].AgentID, "agent-1")
	}
	if agents[0].Status != "online" {
		t.Errorf("status: got %q, want %q", agents[0].Status, "online")
	}
}

func TestA2AUnregisterAgent(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{AgentID: "agent-1", AgentType: "dev"})
	a2a.UnregisterAgent("agent-1")

	agents := a2a.OnlineAgents()
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestA2AFindByCapability(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{
		AgentID: "agent-1", AgentType: "developer",
		Capabilities: []string{"go", "api_design"},
	})
	a2a.RegisterAgent(&AgentEndpoint{
		AgentID: "agent-2", AgentType: "tester",
		Capabilities: []string{"testing", "go"},
	})
	a2a.RegisterAgent(&AgentEndpoint{
		AgentID: "agent-3", AgentType: "frontend",
		Capabilities: []string{"react", "css"},
	})

	goAgents := a2a.FindAgentsByCapability("go")
	if len(goAgents) != 2 {
		t.Errorf("expected 2 Go-capable agents, got %d", len(goAgents))
	}

	reactAgents := a2a.FindAgentsByCapability("react")
	if len(reactAgents) != 1 {
		t.Errorf("expected 1 React-capable agent, got %d", len(reactAgents))
	}
}

func TestA2AFindByType(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{AgentID: "a1", AgentType: "developer"})
	a2a.RegisterAgent(&AgentEndpoint{AgentID: "a2", AgentType: "developer"})
	a2a.RegisterAgent(&AgentEndpoint{AgentID: "a3", AgentType: "tester"})

	devs := a2a.FindAgentsByType("developer")
	if len(devs) != 2 {
		t.Errorf("expected 2 developers, got %d", len(devs))
	}
}

func TestA2ASendDirect(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{AgentID: "sender", AgentType: "dev"})
	a2a.RegisterAgent(&AgentEndpoint{AgentID: "receiver", AgentType: "qa"})

	err := a2a.SendDirect(context.Background(), "sender", "receiver",
		MessageTypeRequest, map[string]interface{}{"task": "review code"})
	if err != nil {
		t.Fatalf("send direct: %v", err)
	}
}

func TestA2ASendDirectUnregistered(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{AgentID: "sender", AgentType: "dev"})

	err := a2a.SendDirect(context.Background(), "sender", "unknown",
		MessageTypeRequest, map[string]interface{}{})
	if err == nil {
		t.Error("should fail for unregistered target")
	}
}

func TestA2ACollabSession(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{AgentID: "dev-1", AgentType: "developer"})
	a2a.RegisterAgent(&AgentEndpoint{AgentID: "qa-1", AgentType: "tester"})

	session, err := a2a.RequestCollab(context.Background(),
		"dev-1", []string{"qa-1"}, CollabTypeReview, "task-123",
		map[string]interface{}{"code": "func main() {}"})
	if err != nil {
		t.Fatalf("request collab: %v", err)
	}

	if session.Status != "pending" {
		t.Errorf("session status: got %q, want %q", session.Status, "pending")
	}
	if session.Type != CollabTypeReview {
		t.Errorf("collab type: got %q, want %q", session.Type, CollabTypeReview)
	}

	// QA 接受协作
	err = a2a.RespondCollab(context.Background(), session.ID, "qa-1", true,
		map[string]interface{}{"message": "I'll review it"})
	if err != nil {
		t.Fatalf("respond collab: %v", err)
	}

	// 验证会话状态
	s := a2a.GetSession(session.ID)
	if s.Status != "active" {
		t.Errorf("session should be active, got %q", s.Status)
	}
	if len(s.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(s.Messages))
	}

	// 完成会话
	a2a.CompleteSession(session.ID, map[string]interface{}{"approved": true})
	s = a2a.GetSession(session.ID)
	if s.Status != "completed" {
		t.Errorf("session should be completed, got %q", s.Status)
	}
}

func TestA2AListSessions(t *testing.T) {
	bus := newTestBus()
	a2a := NewA2AProtocol(bus)

	a2a.RegisterAgent(&AgentEndpoint{AgentID: "a1", AgentType: "dev"})
	a2a.RegisterAgent(&AgentEndpoint{AgentID: "a2", AgentType: "qa"})

	_, _ = a2a.RequestCollab(context.Background(), "a1", []string{"a2"},
		CollabTypeReview, "t1", nil)
	_, _ = a2a.RequestCollab(context.Background(), "a1", []string{"a2"},
		CollabTypeAssist, "t2", nil)

	all := a2a.ListSessions("")
	if len(all) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(all))
	}

	pending := a2a.ListSessions("pending")
	if len(pending) != 2 {
		t.Errorf("expected 2 pending sessions, got %d", len(pending))
	}
}
