package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// 简化版 Agent Runtime - 用于快速测试

type AgentRuntime struct {
	ID             string
	Name           string
	Type           string
	OrchestratorURL string
	Status         string

	wsConn *websocket.Conn
	mu     sync.RWMutex

	// 任务处理
	currentTask *Task
}

type Task struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Content     map[string]interface{} `json:"content"`
}

type WSMessage struct {
	Type    string      `json:"type"`
	From    string      `json:"from"`
	To      string      `json:"to"`
	Content interface{} `json:"content"`
}

func NewAgentRuntime() *AgentRuntime {
	return &AgentRuntime{
		ID:              getEnv("AGENT_ID", fmt.Sprintf("agent-%d", time.Now().Unix())),
		Name:            getEnv("AGENT_NAME", "Agent"),
		Type:            getEnv("AGENT_TYPE", "developer"),
		OrchestratorURL: getEnv("ORCHESTRATOR_URL", "ws://localhost:8080"),
		Status:          "idle",
	}
}

func (a *AgentRuntime) Run() {
	log.Printf("========================================")
	log.Printf("Agent Runtime Starting")
	log.Printf("  ID:   %s", a.ID)
	log.Printf("  Name: %s", a.Name)
	log.Printf("  Type: %s", a.Type)
	log.Printf("========================================")

	// 连接到 Orchestrator
	if err := a.connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// 注册
	a.register()

	// 启动心跳
	go a.heartbeat()

	// 启动 MCP 服务 (可选)
	go a.startMCPServer()

	// 等待中断
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	a.disconnect()
}

func (a *AgentRuntime) connect() error {
	url := fmt.Sprintf("%s/ws", a.OrchestratorURL)
	log.Printf("Connecting to %s", url)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("dial error: %w", err)
	}

	a.wsConn = conn

	// 启动消息处理
	go a.handleMessages()

	return nil
}

func (a *AgentRuntime) disconnect() {
	if a.wsConn != nil {
		a.wsConn.Close()
	}
}

func (a *AgentRuntime) register() {
	msg := WSMessage{
		Type: "register",
		From: a.ID,
		Content: map[string]interface{}{
			"agent_id":   a.ID,
			"agent_name": a.Name,
			"agent_type": a.Type,
			"status":     "idle",
			"skills":     a.getSkills(),
		},
	}

	a.send(msg)
	log.Printf("Registered with orchestrator")
}

func (a *AgentRuntime) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		a.mu.RLock()
		status := a.Status
		a.mu.RUnlock()

		msg := WSMessage{
			Type: "heartbeat",
			From: a.ID,
			Content: map[string]interface{}{
				"agent_id":   a.ID,
				"status":     status,
				"timestamp":  time.Now().UnixMilli(),
			},
		}

		a.send(msg)
	}
}

func (a *AgentRuntime) handleMessages() {
	for {
		_, msgBytes, err := a.wsConn.ReadMessage()
		if err != nil {
			log.Printf("Read error: %v", err)
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		// 只处理发给自己的消息
		if msg.To != "" && msg.To != a.ID {
			continue
		}

		switch msg.Type {
		case "connected":
			log.Printf("Connected to orchestrator")

		case "task_assigned":
			a.handleTask(msg)

		case "ping":
			a.send(WSMessage{Type: "pong", From: a.ID})
		}
	}
}

func (a *AgentRuntime) handleTask(msg WSMessage) {
	content, ok := msg.Content.(map[string]interface{})
	if !ok {
		return
	}

	taskID, _ := content["id"].(string)
	title, _ := content["title"].(string)

	log.Printf("========================================")
	log.Printf("Received Task: %s", taskID)
	log.Printf("  Title: %s", title)
	log.Printf("========================================")

	// 更新状态
	a.mu.Lock()
	a.Status = "busy"
	a.currentTask = &Task{
		ID:          taskID,
		Title:       title,
		Description: getString(content, "description"),
		Content:     content,
	}
	a.mu.Unlock()

	// 模拟任务处理
	go a.processTask(a.currentTask)
}

func (a *AgentRuntime) processTask(task *Task) {
	log.Printf("Processing task: %s", task.ID)

	// 模拟处理时间
	for i := 0; i <= 100; i += 20 {
		time.Sleep(500 * time.Millisecond)
		log.Printf("  Progress: %d%%", i)
	}

	// 生成结果
	result := a.generateResult(task)

	// 发送完成消息
	a.send(WSMessage{
		Type: "task_complete",
		From: a.ID,
		Content: map[string]interface{}{
			"task_id":    task.ID,
			"status":     "completed",
			"result":     result,
			"agent_type": a.Type,
			"completed_at": time.Now().UnixMilli(),
		},
	})

	log.Printf("Task completed: %s", task.ID)

	// 恢复状态
	a.mu.Lock()
	a.Status = "idle"
	a.currentTask = nil
	a.mu.Unlock()
}

func (a *AgentRuntime) generateResult(task *Task) map[string]interface{} {
	switch a.Type {
	case "developer":
		return map[string]interface{}{
			"code": fmt.Sprintf("// Code for: %s\npackage main\n\nfunc main() {\n\t// TODO: implement\n}", task.Title),
			"language": "go",
		}
	case "tester":
		return map[string]interface{}{
			"test_cases": []string{"test_case_1", "test_case_2"},
			"coverage":   85.5,
		}
	case "architect":
		return map[string]interface{}{
			"design":    "System architecture design",
			"diagram":   "ASCII diagram here",
			"tech_stack": []string{"Go", "PostgreSQL", "Redis"},
		}
	case "devops":
		return map[string]interface{}{
			"deployment": "kubernetes",
			"status":     "deployed",
			"url":        "https://app.example.com",
		}
	default:
		return map[string]interface{}{
			"message": "Task completed",
		}
	}
}

func (a *AgentRuntime) send(msg WSMessage) {
	if a.wsConn == nil {
		return
	}

	a.wsConn.WriteJSON(msg)
}

func (a *AgentRuntime) getSkills() []string {
	switch a.Type {
	case "developer":
		return []string{"code_generation", "code_review", "debug"}
	case "tester":
		return []string{"test_generation", "code_review"}
	case "architect":
		return []string{"system_design", "code_review"}
	case "devops":
		return []string{"deploy", "monitor"}
	default:
		return []string{}
	}
}

// MCP Server (简化版)
func (a *AgentRuntime) startMCPServer() {
	port := getEnv("MCP_PORT", "8081")

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "ok",
			"agent_id":  a.ID,
			"agent_type": a.Type,
		})
	})

	http.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// 简化的 MCP 响应
		response := map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"agent_id":   a.ID,
				"agent_type": a.Type,
				"skills":     a.getSkills(),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	log.Printf("MCP Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("MCP Server error: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func main() {
	runtime := NewAgentRuntime()
	runtime.Run()
}
