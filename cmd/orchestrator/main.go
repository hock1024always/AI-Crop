package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"ai-corp/pkg/database"
	"ai-corp/pkg/llm"
	"ai-corp/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// 简化版 Orchestrator - 用于快速测试

// Config 配置
type Config struct {
	LLM struct {
		Provider string `yaml:"provider"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
		Timeout  string `yaml:"timeout"`
	} `yaml:"llm"`
	Orchestrator struct {
		Port    int    `yaml:"port"`
		NATSURL string `yaml:"nats_url"`
	} `yaml:"orchestrator"`
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		DBName   string `yaml:"dbname"`
		SSLMode  string `yaml:"sslmode"`
	} `yaml:"database"`
}

// Agent 定义
type Agent struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	Skills    []string `json:"skills"`
	CreatedAt int64    `json:"created_at"`
	Model     string   `json:"model,omitempty"`
}

// Task 定义
type Task struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	AssignedTo  string                 `json:"assigned_to,omitempty"`
	CreatedBy   string                 `json:"created_by"`
	CreatedAt   int64                  `json:"created_at"`
	Result      map[string]interface{} `json:"result,omitempty"`
}

// Orchestrator 总控
type Orchestrator struct {
	agents    map[string]*Agent
	tasks     map[string]*Task
	wsClients map[*websocket.Conn]bool
	broadcast chan WSMessage
	mu        sync.RWMutex

	// 消息队列
	taskQueue chan *Task

	// LLM 客户端
	llmClient *llm.Client
	config    *Config

	// Database
	db *database.DB

	// Inference service (LLM + metrics)
	inference *llm.InferenceService
}

type WSMessage struct {
	Type    string      `json:"type"`
	From    string      `json:"from"`
	To      string      `json:"to"`
	Content interface{} `json:"content"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// loadConfig 加载配置
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 展开环境变量
	content := os.ExpandEnv(string(data))

	var config Config
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return nil, err
	}

	// 优先使用环境变量
	if apiKey := os.Getenv("LLM_API_KEY"); apiKey != "" {
		config.LLM.APIKey = apiKey
	}
	if provider := os.Getenv("LLM_PROVIDER"); provider != "" {
		config.LLM.Provider = provider
	}
	if model := os.Getenv("LLM_MODEL"); model != "" {
		config.LLM.Model = model
	}

	return &config, nil
}

func NewOrchestrator(configPath string) *Orchestrator {
	// 加载配置
	config, err := loadConfig(configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s: %v, using defaults", configPath, err)
		config = &Config{}
		config.Orchestrator.Port = 8080
	}

	// 初始化 LLM 客户端
	var llmClient *llm.Client
	if config.LLM.APIKey != "" {
		llmClient = llm.NewClient(llm.Config{
			Provider: llm.Provider(config.LLM.Provider),
			APIKey:   config.LLM.APIKey,
			Model:    config.LLM.Model,
		})
		log.Printf("LLM client initialized: provider=%s, model=%s", config.LLM.Provider, config.LLM.Model)
	} else {
		log.Println("Warning: No LLM API key configured, LLM features disabled")
	}

	// 初始化数据库
	var db *database.DB
	dbCfg := database.DefaultConfig()
	if config.Database.Host != "" {
		dbCfg.Host = config.Database.Host
	}
	if config.Database.Port > 0 {
		dbCfg.Port = config.Database.Port
	}
	if config.Database.User != "" {
		dbCfg.User = config.Database.User
	}
	if config.Database.Password != "" {
		dbCfg.Password = config.Database.Password
	}
	if config.Database.DBName != "" {
		dbCfg.DBName = config.Database.DBName
	}
	if config.Database.SSLMode != "" {
		dbCfg.SSLMode = config.Database.SSLMode
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err = database.New(ctx, dbCfg)
	if err != nil {
		log.Printf("Warning: Database connection failed: %v (running without persistence)", err)
		db = nil
	} else {
		log.Printf("Database connected: %s@%s:%d/%s", dbCfg.User, dbCfg.Host, dbCfg.Port, dbCfg.DBName)
	}

	// 初始化推理服务
	var inference *llm.InferenceService
	if llmClient != nil {
		inference = llm.NewInferenceService(llmClient, db)
		log.Println("Inference service initialized with metrics recording")
	}

	return &Orchestrator{
		agents:    make(map[string]*Agent),
		tasks:     make(map[string]*Task),
		wsClients: make(map[*websocket.Conn]bool),
		broadcast: make(chan WSMessage, 100),
		taskQueue: make(chan *Task, 100),
		llmClient: llmClient,
		config:    config,
		db:        db,
		inference: inference,
	}
}

func (o *Orchestrator) Run() {
	// 启动消息广播
	go o.handleBroadcast()

	// 启动任务调度
	go o.taskScheduler()

	// 启动系统指标采集 (CPU/内存/网络)
	sysCollector := metrics.NewSystemCollector()
	sysCollector.StartPeriodicCollection(5 * time.Second)

	// 设置路由
	r := gin.Default()

	// CORS
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// API 路由
	api := r.Group("/api/v1")
	{
		api.GET("/agents", o.listAgents)
		api.POST("/agents", o.createAgent)
		api.GET("/agents/:id", o.getAgent)
		api.DELETE("/agents/:id", o.deleteAgent)

		api.GET("/tasks", o.listTasks)
		api.POST("/tasks", o.createTask)
		api.GET("/tasks/:id", o.getTask)
		api.POST("/tasks/:id/assign", o.assignTask)

		api.GET("/skills", o.listSkills)

		// NLP 任务解析
		api.POST("/nlp/parse", o.parseTask)

		// LLM 相关
		api.GET("/llm/status", o.llmStatus)
		api.POST("/chat", o.chat)

		// Database-backed endpoints
		api.GET("/db/health", o.dbHealth)
		api.GET("/db/agents", o.dbListAgents)
		api.GET("/db/models", o.dbListModels)
		api.GET("/db/stats", o.dbInferenceStats)
		api.GET("/db/audit", o.dbAuditRecent)
	}

	// WebSocket
	r.GET("/ws", o.handleWS)

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "timestamp": time.Now().UnixMilli()})
	})

	// 监控端点
	mc := metrics.Global()
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))            // Standard Prometheus scrape endpoint
	r.GET("/api/v1/metrics", gin.WrapF(mc.HTTPHandler()))       // Custom JSON metrics for frontend
	r.GET("/api/v1/metrics/prom", gin.WrapF(mc.PrometheusHandler())) // Legacy text format

	// 启动定时采集
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mc.StartCollecting(ctx, 10*time.Second)

	// 静态文件 (像素风前端)
	r.StaticFile("/", "./web/pixel/index.html")
	r.StaticFile("/index.html", "./web/pixel/index.html")
	r.StaticFile("/style.css", "./web/pixel/style.css")
	r.StaticFile("/app.js", "./web/pixel/app.js")
	r.Static("/assets", "./web/pixel/assets")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Orchestrator starting on :%s", port)

	// 优雅关闭
	go func() {
		if err := r.Run(":" + port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// 等待中断
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
}

// HTTP Handlers

func (o *Orchestrator) listAgents(c *gin.Context) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	agents := make([]*Agent, 0, len(o.agents))
	for _, a := range o.agents {
		agents = append(agents, a)
	}

	c.JSON(200, gin.H{"agents": agents, "count": len(agents)})
}

func (o *Orchestrator) createAgent(c *gin.Context) {
	var req struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Type  string `json:"type"`
		Model string `json:"model"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 自动生成 ID
	id := req.ID
	if id == "" {
		id = fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	agent := &Agent{
		ID:        id,
		Name:      req.Name,
		Type:      req.Type,
		Status:    "idle",
		Skills:    o.getSkillsForType(req.Type),
		CreatedAt: time.Now().UnixMilli(),
		Model:     req.Model,
	}

	o.agents[agent.ID] = agent

	// 广播新 Agent 加入
	o.broadcast <- WSMessage{
		Type:    "agent_joined",
		From:    "orchestrator",
		Content: agent,
	}

	c.JSON(201, agent)
}

func (o *Orchestrator) getAgent(c *gin.Context) {
	id := c.Param("id")

	o.mu.RLock()
	agent, exists := o.agents[id]
	o.mu.RUnlock()

	if !exists {
		c.JSON(404, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(200, agent)
}

func (o *Orchestrator) deleteAgent(c *gin.Context) {
	id := c.Param("id")

	o.mu.Lock()
	delete(o.agents, id)
	o.mu.Unlock()

	c.JSON(200, gin.H{"message": "agent deleted"})
}

func (o *Orchestrator) listTasks(c *gin.Context) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	tasks := make([]*Task, 0, len(o.tasks))
	for _, t := range o.tasks {
		tasks = append(tasks, t)
	}

	c.JSON(200, gin.H{"tasks": tasks, "count": len(tasks)})
}

func (o *Orchestrator) createTask(c *gin.Context) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		CreatedBy   string `json:"created_by"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	task := &Task{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Title:       req.Title,
		Description: req.Description,
		Status:      "pending",
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now().UnixMilli(),
	}

	o.tasks[task.ID] = task

	// 加入任务队列
	select {
	case o.taskQueue <- task:
	default:
		log.Printf("Task queue full, task %s dropped", task.ID)
	}

	c.JSON(201, task)
}

func (o *Orchestrator) getTask(c *gin.Context) {
	id := c.Param("id")

	o.mu.RLock()
	task, exists := o.tasks[id]
	o.mu.RUnlock()

	if !exists {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}

	c.JSON(200, task)
}

func (o *Orchestrator) assignTask(c *gin.Context) {
	taskID := c.Param("id")

	var req struct {
		AgentID string `json:"agent_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	task, exists := o.tasks[taskID]
	if !exists {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}

	agent, exists := o.agents[req.AgentID]
	if !exists {
		c.JSON(404, gin.H{"error": "agent not found"})
		return
	}

	task.AssignedTo = agent.ID
	task.Status = "running"

	// 广播任务分配
	o.broadcast <- WSMessage{
		Type:    "task_assigned",
		From:    "orchestrator",
		To:      agent.ID,
		Content: task,
	}

	c.JSON(200, task)
}

func (o *Orchestrator) listSkills(c *gin.Context) {
	skills := []map[string]string{
		{"name": "code_generation", "description": "生成代码"},
		{"name": "code_review", "description": "代码审查"},
		{"name": "debug", "description": "调试代码"},
		{"name": "test_generation", "description": "生成测试"},
		{"name": "system_design", "description": "系统设计"},
		{"name": "deploy", "description": "部署应用"},
	}

	c.JSON(200, gin.H{"skills": skills})
}

func (o *Orchestrator) parseTask(c *gin.Context) {
	var req struct {
		Input string `json:"input"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 简单的关键词匹配
	taskType := "general"
	subTasks := []map[string]interface{}{}

	if containsAny(req.Input, []string{"开发", "写代码", "实现", "编写"}) {
		taskType = "development"
		subTasks = []map[string]interface{}{
			{"step": 1, "agent_type": "architect", "action": "设计架构"},
			{"step": 2, "agent_type": "developer", "action": "编写代码"},
			{"step": 3, "agent_type": "tester", "action": "编写测试"},
		}
	} else if containsAny(req.Input, []string{"测试", "验证", "检查"}) {
		taskType = "testing"
		subTasks = []map[string]interface{}{
			{"step": 1, "agent_type": "tester", "action": "分析测试需求"},
			{"step": 2, "agent_type": "tester", "action": "生成测试用例"},
		}
	} else if containsAny(req.Input, []string{"部署", "发布", "上线"}) {
		taskType = "deployment"
		subTasks = []map[string]interface{}{
			{"step": 1, "agent_type": "devops", "action": "准备部署环境"},
			{"step": 2, "agent_type": "devops", "action": "执行部署"},
		}
	}

	c.JSON(200, gin.H{
		"input":      req.Input,
		"task_type":  taskType,
		"sub_tasks":  subTasks,
		"timestamp":  time.Now().UnixMilli(),
	})
}

// llmStatus 返回 LLM 状态
func (o *Orchestrator) llmStatus(c *gin.Context) {
	if o.llmClient == nil {
		c.JSON(200, gin.H{
			"available": false,
			"message":   "LLM client not configured",
		})
		return
	}

	c.JSON(200, gin.H{
		"available": true,
		"provider":  o.config.LLM.Provider,
		"model":     o.config.LLM.Model,
	})
}

// chat 与 LLM 对话
func (o *Orchestrator) chat(c *gin.Context) {
	var req struct {
		Message   string `json:"message"`
		AgentType string `json:"agent_type,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Use InferenceService if available (records metrics to DB)
	if o.inference != nil {
		systemPrompt := llm.AgentSystemPrompt(req.AgentType)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		result, err := o.inference.ChatWithSystem(ctx, systemPrompt, req.Message, nil)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"response":          result.Content,
			"provider":          result.Provider,
			"model":             result.Model,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"total_tokens":      result.TotalTokens,
			"latency_ms":        result.LatencyMs,
			"tps":               result.TPS,
		})
		return
	}

	// Fallback to raw LLM client
	if o.llmClient == nil {
		c.JSON(503, gin.H{"error": "LLM client not configured"})
		return
	}

	systemPrompt := llm.AgentSystemPrompt(req.AgentType)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	response, err := o.llmClient.ChatWithSystem(ctx, systemPrompt, req.Message)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"response": response,
		"provider": o.config.LLM.Provider,
		"model":    o.config.LLM.Model,
	})
}

// WebSocket Handler

func (o *Orchestrator) handleWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	o.mu.Lock()
	o.wsClients[conn] = true
	o.mu.Unlock()

	log.Printf("WebSocket client connected, total: %d", len(o.wsClients))

	// 发送欢迎消息
	conn.WriteJSON(WSMessage{
		Type:    "connected",
		From:    "orchestrator",
		Content: gin.H{"message": "Welcome to Multi-Agent Platform"},
	})

	// 读取消息
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		var wsMsg WSMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			continue
		}

		o.handleWSMessage(conn, &wsMsg)
	}

	o.mu.Lock()
	delete(o.wsClients, conn)
	o.mu.Unlock()
}

func (o *Orchestrator) handleWSMessage(conn *websocket.Conn, msg *WSMessage) {
	switch msg.Type {
	case "register":
		// Agent 注册
		if content, ok := msg.Content.(map[string]interface{}); ok {
			agent := &Agent{
				ID:        getString(content, "agent_id"),
				Name:      getString(content, "agent_name"),
				Type:      getString(content, "agent_type"),
				Status:    "idle",
				CreatedAt: time.Now().UnixMilli(),
			}
			o.mu.Lock()
			o.agents[agent.ID] = agent
			o.mu.Unlock()
			log.Printf("Agent registered: %s (%s)", agent.Name, agent.Type)
		}

	case "heartbeat":
		// 心跳
		if content, ok := msg.Content.(map[string]interface{}); ok {
			agentID := getString(content, "agent_id")
			o.mu.Lock()
			if agent, exists := o.agents[agentID]; exists {
				agent.Status = getString(content, "status")
			}
			o.mu.Unlock()
		}

	case "task_complete":
		// 任务完成
		if content, ok := msg.Content.(map[string]interface{}); ok {
			taskID := getString(content, "task_id")
			o.mu.Lock()
			if task, exists := o.tasks[taskID]; exists {
				task.Status = "completed"
				if result, ok := content["result"].(map[string]interface{}); ok {
					task.Result = result
				}
			}
			o.mu.Unlock()
		}

	case "task_fail":
		// 任务失败
		if content, ok := msg.Content.(map[string]interface{}); ok {
			taskID := getString(content, "task_id")
			o.mu.Lock()
			if task, exists := o.tasks[taskID]; exists {
				task.Status = "failed"
			}
			o.mu.Unlock()
		}
	}
}

// Background Workers

func (o *Orchestrator) handleBroadcast() {
	for msg := range o.broadcast {
		o.mu.RLock()
		clients := make([]*websocket.Conn, 0, len(o.wsClients))
		for c := range o.wsClients {
			clients = append(clients, c)
		}
		o.mu.RUnlock()

		for _, client := range clients {
			if err := client.WriteJSON(msg); err != nil {
				log.Printf("WebSocket write error: %v", err)
				client.Close()
				o.mu.Lock()
				delete(o.wsClients, client)
				o.mu.Unlock()
			}
		}
	}
}

func (o *Orchestrator) taskScheduler() {
	for task := range o.taskQueue {
		// 找到空闲的 Agent
		o.mu.RLock()
		var targetAgent *Agent
		for _, agent := range o.agents {
			if agent.Status == "idle" {
				targetAgent = agent
				break
			}
		}
		o.mu.RUnlock()

		if targetAgent != nil {
			o.mu.Lock()
			task.AssignedTo = targetAgent.ID
			task.Status = "running"
			o.mu.Unlock()

			// 广播任务分配
			o.broadcast <- WSMessage{
				Type:    "task_assigned",
				From:    "orchestrator",
				To:      targetAgent.ID,
				Content: task,
			}

			log.Printf("Task %s assigned to %s", task.ID, targetAgent.Name)
		} else {
			// 没有空闲 Agent，重新入队
			go func(t *Task) {
				time.Sleep(5 * time.Second)
				o.taskQueue <- t
			}(task)
		}
	}
}

// Helpers

func (o *Orchestrator) getSkillsForType(agentType string) []string {
	switch agentType {
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

func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// Database-backed handlers

func (o *Orchestrator) dbHealth(c *gin.Context) {
	if o.db == nil {
		c.JSON(503, gin.H{"status": "unavailable", "message": "database not connected"})
		return
	}
	health, err := o.db.HealthCheck(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(200, health)
}

func (o *Orchestrator) dbListAgents(c *gin.Context) {
	if o.db == nil {
		c.JSON(503, gin.H{"error": "database not connected"})
		return
	}
	role := c.Query("role")
	agents, err := o.db.Agents.List(c.Request.Context(), role)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"agents": agents, "count": len(agents)})
}

func (o *Orchestrator) dbListModels(c *gin.Context) {
	if o.db == nil {
		c.JSON(503, gin.H{"error": "database not connected"})
		return
	}
	models, err := o.db.Models.ListActive(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"models": models, "count": len(models)})
}

func (o *Orchestrator) dbInferenceStats(c *gin.Context) {
	if o.db == nil {
		c.JSON(503, gin.H{"error": "database not connected"})
		return
	}
	stats, err := o.db.Metrics.GetStats(c.Request.Context(), 24)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, stats)
}

func (o *Orchestrator) dbAuditRecent(c *gin.Context) {
	if o.db == nil {
		c.JSON(503, gin.H{"error": "database not connected"})
		return
	}
	entries, err := o.db.Audit.Recent(c.Request.Context(), 50)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"entries": entries, "count": len(entries)})
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	orch := NewOrchestrator(configPath)
	orch.Run()
}
