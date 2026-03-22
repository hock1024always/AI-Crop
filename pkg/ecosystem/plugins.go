// Package ecosystem 提供 AI 生态拓展能力
// 包含：插件注册、Webhook 事件、外部集成（Slack/钉钉/飞书）、市场插件
package ecosystem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ---- Plugin Registry ----

type PluginType string

const (
	PluginLLM       PluginType = "llm"
	PluginTool      PluginType = "tool"
	PluginNotifier  PluginType = "notifier"
	PluginStorage   PluginType = "storage"
)

type Plugin struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        PluginType             `json:"type"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Author      string                 `json:"author"`
	Config      map[string]interface{} `json:"config"`
	Enabled     bool                   `json:"enabled"`
	RegisteredAt time.Time             `json:"registered_at"`
	Handler     interface{}            `json:"-"` // 实际处理器
}

type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin
}

func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{plugins: make(map[string]*Plugin)}
}

func (r *PluginRegistry) Register(p *Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plugins[p.ID]; exists {
		return fmt.Errorf("plugin %s already registered", p.ID)
	}
	p.RegisteredAt = time.Now()
	p.Enabled = true
	r.plugins[p.ID] = p
	return nil
}

func (r *PluginRegistry) Get(id string) (*Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	return p, ok
}

func (r *PluginRegistry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		out = append(out, p)
	}
	return out
}

func (r *PluginRegistry) Disable(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.plugins[id]; ok {
		p.Enabled = false
	}
}

// ---- Webhook Event Bus ----

type EventType string

const (
	EventAgentCreated   EventType = "agent.created"
	EventAgentDeleted   EventType = "agent.deleted"
	EventTaskCreated    EventType = "task.created"
	EventTaskCompleted  EventType = "task.completed"
	EventTaskFailed     EventType = "task.failed"
	EventWorkflowDone   EventType = "workflow.completed"
)

type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp time.Time              `json:"timestamp"`
}

type Webhook struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Events  []EventType `json:"events"`
	Secret  string    `json:"secret,omitempty"`
	Enabled bool      `json:"enabled"`
}

type EventBus struct {
	mu       sync.RWMutex
	webhooks []*Webhook
	client   *http.Client
}

func NewEventBus() *EventBus {
	return &EventBus{
		webhooks: make([]*Webhook, 0),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (eb *EventBus) Subscribe(wh *Webhook) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	wh.Enabled = true
	eb.webhooks = append(eb.webhooks, wh)
}

func (eb *EventBus) Publish(evt Event) {
	evt.Timestamp = time.Now()
	eb.mu.RLock()
	hooks := make([]*Webhook, len(eb.webhooks))
	copy(hooks, eb.webhooks)
	eb.mu.RUnlock()

	for _, wh := range hooks {
		if !wh.Enabled {
			continue
		}
		for _, t := range wh.Events {
			if t == evt.Type {
				go eb.deliver(wh, evt)
				break
			}
		}
	}
}

func (eb *EventBus) deliver(wh *Webhook, evt Event) {
	body, _ := json.Marshal(evt)
	req, _ := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if wh.Secret != "" {
		req.Header.Set("X-Webhook-Secret", wh.Secret)
	}
	resp, err := eb.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// ---- IM 通知集成 ----

type IMNotifier interface {
	SendText(channel, text string) error
	SendCard(channel string, card map[string]interface{}) error
}

// DingTalkNotifier 钉钉群机器人
type DingTalkNotifier struct {
	WebhookURL string
	client     *http.Client
}

func NewDingTalkNotifier(webhookURL string) *DingTalkNotifier {
	return &DingTalkNotifier{
		WebhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *DingTalkNotifier) SendText(_, text string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	})
	resp, err := d.client.Post(d.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (d *DingTalkNotifier) SendCard(_ string, card map[string]interface{}) error {
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype":    "actionCard",
		"actionCard": card,
	})
	resp, err := d.client.Post(d.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// LarkNotifier 飞书群机器人
type LarkNotifier struct {
	WebhookURL string
	client     *http.Client
}

func NewLarkNotifier(webhookURL string) *LarkNotifier {
	return &LarkNotifier{
		WebhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (l *LarkNotifier) SendText(_, text string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	})
	resp, err := l.client.Post(l.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (l *LarkNotifier) SendCard(_ string, card map[string]interface{}) error {
	body, _ := json.Marshal(map[string]interface{}{
		"msg_type": "interactive",
		"card":     card,
	})
	resp, err := l.client.Post(l.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
