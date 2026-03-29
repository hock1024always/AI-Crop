package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"ai-corp/pkg/database"
	"ai-corp/pkg/llm"
	"ai-corp/pkg/memory"
	"ai-corp/pkg/metrics"
	"ai-corp/pkg/problem"
	"ai-corp/pkg/rag"
	"ai-corp/pkg/security"
	"ai-corp/pkg/workflow"

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
		Local    struct {
			Enabled bool   `yaml:"enabled"`
			BaseURL string `yaml:"base_url"`
			Model   string `yaml:"model"`
		} `yaml:"local"`
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
	llmClient    *llm.Client
	ollamaClient *llm.OllamaClient // 本地 Ollama
	config       *Config

	// Database
	db *database.DB

	// Inference service (LLM + metrics)
	inference *llm.InferenceService

	// Self-improvement loop
	selfImprove *memory.SelfImprovementLoop

	// RAG service
	ragService *rag.RAGService

	// Security: Quota + PII + Audit
	quotaManager *security.QuotaManager
	piiSanitizer *security.PIISanitizer
	auditLog     *security.AuditLog

	// Workflow engine
	workflows  map[string]*workflow.Workflow
	workflowMu sync.RWMutex

	// Workflow visualizer
	workflowViz *workflow.WorkflowVisualizer

	// Container monitor
	containerMonitor *metrics.ContainerMonitor

	// JWT secret
	jwtSecret string
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

	// 初始化 LLM 客户端（云端）
	var llmClient *llm.Client
	if config.LLM.APIKey != "" {
		llmClient = llm.NewClient(llm.Config{
			Provider: llm.Provider(config.LLM.Provider),
			APIKey:   config.LLM.APIKey,
			Model:    config.LLM.Model,
		})
		log.Printf("LLM client initialized: provider=%s, model=%s", config.LLM.Provider, config.LLM.Model)
	} else {
		log.Println("Warning: No LLM API key configured, cloud LLM disabled")
	}

	// 初始化 Ollama 本地客户端
	var ollamaClient *llm.OllamaClient
	ollamaURL := config.LLM.Local.BaseURL
	if ollamaURL == "" {
		ollamaURL = os.Getenv("OLLAMA_BASE_URL")
	}
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434" // 默认地址
	}
	ollamaModel := config.LLM.Local.Model
	if ollamaModel == "" {
		ollamaModel = os.Getenv("OLLAMA_MODEL")
	}
	if ollamaModel == "" {
		ollamaModel = "deepseek-r1:1.5b" // 默认蒸馏模型
	}
	ollamaClient = llm.NewOllamaClient(ollamaURL, ollamaModel)
	// 启动时检查 Ollama 可用性
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
	if ollamaClient.IsAvailable(checkCtx) {
		log.Printf("Ollama client initialized: url=%s, model=%s", ollamaURL, ollamaModel)
	} else {
		log.Printf("Warning: Ollama not available at %s, local model disabled", ollamaURL)
		ollamaClient = nil
	}
	checkCancel()

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

	// 初始化 RAG 服务（使用内存向量存储）
	var ragService *rag.RAGService
	if llmClient != nil {
		// 使用内存向量存储（后续可以切换到 PostgreSQL）
		memStore := rag.NewMemoryVectorStore()
		// 使用模拟嵌入客户端
		embeddingClient := rag.NewMockEmbeddingClient()
		ragService = rag.NewRAGService(memStore, embeddingClient)
		log.Println("RAG service initialized with memory vector store")
	}

	// 初始化自我改进循环
	var selfImprove *memory.SelfImprovementLoop
	if db != nil && llmClient != nil {
		memStore := memory.NewPostgresMemoryStore(db.Pool)
		llmAdapter := memory.NewLLMClientAdapter(llmClient)

		// 创建 embedding 客户端适配器
		var embeddingAdapter memory.EmbeddingClient
		if ragService != nil {
			// 使用 RAG 服务的 embedding 客户端
			embeddingAdapter = memory.NewEmbeddingClientAdapter(func(ctx context.Context, text string) ([]float32, error) {
				return ragService.GetEmbedding(ctx, text)
			})
		}

		selfImprove = memory.NewSelfImprovementLoop(memStore, llmAdapter, []string{}, embeddingAdapter)
		log.Println("Self-improvement loop initialized with embedding support")
	}

	// 启动时检查并创建向量索引
	if db != nil {
		if err := db.EnsureVectorIndexes(context.Background()); err != nil {
			log.Printf("[WARN] Vector index check failed (non-fatal): %v", err)
		}
	}

	// 初始化安全组件
	quotaManager := security.NewQuotaManager(security.DefaultQuotaConfig(), nil)
	piiSanitizer := security.NewPIISanitizer()
	auditLog := security.NewAuditLog(10000)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "ai-corp-default-secret"
	}

	// 初始化容器监控器
	containerMonitor := metrics.NewContainerMonitor(5 * time.Second)
	containerMonitor.Start()
	log.Println("Container monitor started (5s interval)")

	// 初始化工作流可视化器
	workflowViz := workflow.NewWorkflowVisualizer()

	return &Orchestrator{
		agents:           make(map[string]*Agent),
		tasks:            make(map[string]*Task),
		wsClients:        make(map[*websocket.Conn]bool),
		broadcast:        make(chan WSMessage, 100),
		taskQueue:        make(chan *Task, 100),
		llmClient:        llmClient,
		ollamaClient:     ollamaClient,
		config:           config,
		db:               db,
		inference:        inference,
		selfImprove:      selfImprove,
		ragService:       ragService,
		quotaManager:     quotaManager,
		piiSanitizer:     piiSanitizer,
		auditLog:         auditLog,
		workflows:        make(map[string]*workflow.Workflow),
		workflowViz:      workflowViz,
		containerMonitor: containerMonitor,
		jwtSecret:        jwtSecret,
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

	// 可选认证中间件（有 Token 则验证，无 Token 以匿名身份继续）
	r.Use(security.OptionalAuthMiddleware(o.jwtSecret))

	// 审计日志中间件（记录所有 API 请求）
	r.Use(func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)

		userID, _ := c.Get("user_id")
		userIDStr, _ := userID.(string)
		resourceType, resourceID := parseResourcePath(c.FullPath(), c.Param("id"))

		event := security.AuditEvent{
			Time:     start,
			ClientIP: security.ClientIP(c.Request),
			Method:   c.Request.Method,
			Path:     c.Request.URL.Path,
			Status:   c.Writer.Status(),
			Duration: duration.Milliseconds(),
			UserID:   userIDStr,
		}
		o.auditLog.Record(event)

		// 持久化到数据库
		if o.db != nil {
			go func() {
				_ = o.db.Audit.Log(context.Background(), &database.AuditEntry{
					UserID:       userIDStr,
					Action:       methodToAuditAction(c.Request.Method),
					ResourceType: resourceType,
					ResourceID:   resourceID,
					Details: map[string]interface{}{
						"method":      c.Request.Method,
						"path":        c.Request.URL.Path,
						"status":      c.Writer.Status(),
						"duration_ms": duration.Milliseconds(),
					},
					IPAddress: security.ClientIP(c.Request),
					UserAgent: c.Request.UserAgent(),
				})
			}()
		}
	})

	// 速率限制
	rateLimiter := security.NewRateLimiter(100, time.Minute)
	r.Use(security.RateLimitMiddleware(rateLimiter))

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

		// RAG endpoints
		if o.ragService != nil {
			api.POST("/rag/ingest", o.ragIngestProblem)
			api.POST("/rag/search", o.ragSearch)
			api.GET("/rag/stats", o.ragStats)
		}

		// Workflow endpoints
		api.POST("/workflows", o.createWorkflow)
		api.GET("/workflows", o.listWorkflows)
		api.GET("/workflows/:id", o.getWorkflow)
		api.POST("/workflows/:id/run", o.runWorkflow)
		api.GET("/workflows/executions", o.listWorkflowExecutions)
		api.GET("/workflows/executions/:exec_id", o.getWorkflowExecution)

		// Container metrics endpoints
		api.GET("/containers", o.listContainers)
		api.GET("/containers/:id", o.getContainerMetrics)

		// Security endpoints
		api.POST("/auth/token", o.generateToken)
		api.GET("/quota/stats", o.quotaStats)
		api.POST("/pii/check", o.piiCheck)
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

	// 同步 Agent ID 到 SelfImprovementLoop
	o.syncAgentIDsToLoop()

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

	// 同步 Agent ID 到 SelfImprovementLoop
	o.syncAgentIDsToLoop()

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
		AgentID   string `json:"agent_id,omitempty"`
		AgentType string `json:"agent_type,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Message == "" {
		c.JSON(400, gin.H{"error": "message is required"})
		return
	}

	// Token 配额检查
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	if userIDStr == "" {
		userIDStr = "anonymous"
	}
	if err := o.quotaManager.CheckQuota(userIDStr, 2000); err != nil {
		c.JSON(429, gin.H{"error": err.Error()})
		return
	}

	// PII 脱敏：检测并清理输入中的敏感数据
	sanitizedMsg := o.piiSanitizer.Sanitize(req.Message)

	// 确定 Agent 标识（优先 agent_id，兼容 agent_type）
	agentIdentifier := req.AgentID
	if agentIdentifier == "" {
		agentIdentifier = req.AgentType
	}

	systemPrompt := llm.AgentSystemPrompt(req.AgentType)

	// 注入历史记忆（自我迭代闭环）
	if o.selfImprove != nil && agentIdentifier != "" {
		if memories, err := o.selfImprove.GetRelevantMemories(
			c.Request.Context(), agentIdentifier, "chat",
		); err == nil && len(memories) > 0 {
			var memCtx string
			for _, m := range memories {
				memCtx += fmt.Sprintf("- [%s] %s\n", m.Type, m.Content)
			}
			systemPrompt += "\n\n# 历史经验（来自记忆系统）\n" + memCtx
			log.Printf("[Chat] Injected %d memories for agent: %s", len(memories), agentIdentifier)
		}
	}

	// 使用脱敏后的消息
	chatMessage := sanitizedMsg

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	// ① 优先使用 Ollama 本地模型
	if o.ollamaClient != nil {
		response, err := o.ollamaClient.ChatWithSystem(ctx, systemPrompt, chatMessage)
		if err == nil {
			// 响应 PII 脱敏
			response = o.piiSanitizer.Sanitize(response)
			// 记录配额使用
			o.quotaManager.RecordUsage(userIDStr, len(response)/4)
			c.JSON(200, gin.H{
				"response":         response,
				"provider":         "ollama",
				"model":            o.ollamaClient.GetModel(),
				"agent_identifier": agentIdentifier,
			})
			return
		}
		log.Printf("[Chat] Ollama failed, falling back to cloud: %v", err)
	}

	// ② Fallback: InferenceService（云端 + 指标记录）
	if o.inference != nil {
		result, err := o.inference.ChatWithSystem(ctx, systemPrompt, chatMessage, nil)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		// 响应 PII 脱敏
		result.Content = o.piiSanitizer.Sanitize(result.Content)
		// 记录配额使用
		o.quotaManager.RecordUsage(userIDStr, result.TotalTokens)
		c.JSON(200, gin.H{
			"response":          result.Content,
			"provider":          result.Provider,
			"model":             result.Model,
			"prompt_tokens":     result.PromptTokens,
			"completion_tokens": result.CompletionTokens,
			"total_tokens":      result.TotalTokens,
			"latency_ms":        result.LatencyMs,
			"tps":               result.TPS,
			"agent_identifier":  agentIdentifier,
		})
		return
	}

	// ③ Fallback: 原始 LLM 客户端
	if o.llmClient != nil {
		response, err := o.llmClient.ChatWithSystem(ctx, systemPrompt, chatMessage)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		// 响应 PII 脱敏
		response = o.piiSanitizer.Sanitize(response)
		// 记录配额使用
		o.quotaManager.RecordUsage(userIDStr, len(response)/4)
		c.JSON(200, gin.H{
			"response":         response,
			"provider":         o.config.LLM.Provider,
			"model":            o.config.LLM.Model,
			"agent_identifier": agentIdentifier,
		})
		return
	}

	c.JSON(503, gin.H{"error": "No LLM backend available. Please start Ollama or configure an API key."})
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

			// 同步 Agent ID 到 SelfImprovementLoop
			o.syncAgentIDsToLoop()

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
			agentID := getString(content, "agent_id")
			o.mu.Lock()
			if task, exists := o.tasks[taskID]; exists {
				task.Status = "completed"
				if result, ok := content["result"].(map[string]interface{}); ok {
					task.Result = result
				}
			}
			o.mu.Unlock()

			// 触发自我改进循环
			if o.selfImprove != nil && agentID != "" {
				go func() {
					taskResult := &memory.TaskResult{
						TaskID:     taskID,
						TaskType:   getString(content, "task_type"),
						Success:    true,
						TokensUsed: getInt(content, "tokens_used"),
						LatencyMs:  getInt(content, "latency_ms"),
						RetryCount: getInt(content, "retry_count"),
					}
					// 提取输入输出
					if input, ok := content["input"].(map[string]interface{}); ok {
						taskResult.Input = input
					}
					if output, ok := content["output"].(map[string]interface{}); ok {
						taskResult.Output = output
					}
					// 提取结果
					if result, ok := content["result"].(map[string]interface{}); ok {
						if taskResult.Output == nil {
							taskResult.Output = result
						}
					}

					if err := o.selfImprove.ProcessTaskResult(context.Background(), agentID, taskResult); err != nil {
						log.Printf("[SelfImprove] task_complete processing error: %v", err)
					}
				}()
			}
		}

	case "task_fail":
		// 任务失败
		if content, ok := msg.Content.(map[string]interface{}); ok {
			taskID := getString(content, "task_id")
			agentID := getString(content, "agent_id")
			o.mu.Lock()
			if task, exists := o.tasks[taskID]; exists {
				task.Status = "failed"
			}
			o.mu.Unlock()

			// 失败同样触发自我改进，从错误中学习
			if o.selfImprove != nil && agentID != "" {
				go func() {
					taskResult := &memory.TaskResult{
						TaskID:     taskID,
						TaskType:   getString(content, "task_type"),
						Success:    false,
						Error:      getString(content, "reason"),
						TokensUsed: getInt(content, "tokens_used"),
						LatencyMs:  getInt(content, "latency_ms"),
						RetryCount: getInt(content, "retry_count"),
					}
					// 提取输入
					if input, ok := content["input"].(map[string]interface{}); ok {
						taskResult.Input = input
					}

					if err := o.selfImprove.ProcessTaskResult(context.Background(), agentID, taskResult); err != nil {
						log.Printf("[SelfImprove] task_fail processing error: %v", err)
					}
				}()
			}
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

// syncAgentIDsToLoop 同步当前 Agent ID 列表到 SelfImprovementLoop
func (o *Orchestrator) syncAgentIDsToLoop() {
	if o.selfImprove == nil {
		return
	}

	o.mu.RLock()
	agentIDs := make([]string, 0, len(o.agents))
	for id := range o.agents {
		agentIDs = append(agentIDs, id)
	}
	o.mu.RUnlock()

	o.selfImprove.SetAgentIDs(agentIDs)
	log.Printf("[Orchestrator] Synced %d agent IDs to SelfImprovementLoop: %v", len(agentIDs), agentIDs)
}

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

func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	}
	return 0
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

// RAG handlers

func (o *Orchestrator) ragIngestProblem(c *gin.Context) {
	if o.ragService == nil {
		c.JSON(503, gin.H{"error": "RAG service not available"})
		return
	}

	var req struct {
		ProblemID   string   `json:"problem_id"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Difficulty  string   `json:"difficulty"`
		Tags        []string `json:"tags"`
		Keywords    []string `json:"keywords"`
		Code        string   `json:"code,omitempty"`
		Solution    string   `json:"solution,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 创建问题对象（使用 problem 包中的实际结构）
	problem := &problem.Problem{
		ID:          req.ProblemID,
		Title:       req.Title,
		Description: req.Description,
		Difficulty:  req.Difficulty,
		Tags:        req.Tags,
		Keywords:    req.Keywords,
		// Code 和 Solution 不在标准结构中，暂时忽略
		// 可以考虑扩展结构或使用其他方式存储
	}

	ctx := c.Request.Context()
	if err := o.ragService.IngestProblem(ctx, problem); err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to ingest problem: %v", err)})
		return
	}

	c.JSON(200, gin.H{"message": "Problem ingested successfully", "problem_id": req.ProblemID})
}

func (o *Orchestrator) ragSearch(c *gin.Context) {
	if o.ragService == nil {
		c.JSON(503, gin.H{"error": "RAG service not available"})
		return
	}

	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if req.TopK <= 0 {
		req.TopK = 5
	}

	ctx := c.Request.Context()
	results, err := o.ragService.SearchSimilarProblems(ctx, req.Query, req.TopK)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Search failed: %v", err)})
		return
	}

	// 补充题目详情
	enrichedResults := make([]map[string]interface{}, len(results))
	for i, result := range results {
		problem := o.ragService.GetProblem(result.ID)
		enrichedResults[i] = map[string]interface{}{
			"id":           result.ID,
			"similarity":   result.Score,  // 使用 Score 而不是 Similarity
			"title":        "",
			"description":  "",
			"difficulty":   "",
			"tags":         []string{},
			"has_solution": false,
		}
		if problem != nil {
			enrichedResults[i]["title"] = problem.Title
			enrichedResults[i]["description"] = problem.Description
			enrichedResults[i]["difficulty"] = problem.Difficulty
			enrichedResults[i]["tags"] = problem.Tags
			// 检查是否有解决方案（如果 problem 类型支持）
			if hasSolutionField(problem) {
				enrichedResults[i]["has_solution"] = true
			}
		}
	}

	c.JSON(200, gin.H{
		"results": enrichedResults,
		"count":   len(enrichedResults),
		"query":   req.Query,
	})
}

// 辅助函数：检查 problem 是否有解决方案相关信息
func hasSolutionField(p *problem.Problem) bool {
	// 检查是否有解题模式或示例
	return p != nil && (len(p.SolutionPatterns) > 0 || len(p.Examples) > 0)
}

func (o *Orchestrator) ragStats(c *gin.Context) {
	if o.ragService == nil {
		c.JSON(503, gin.H{"error": "RAG service not available"})
		return
	}

	stats := o.ragService.GetStats()
	c.JSON(200, stats)
}

// ---- Workflow handlers ----

func (o *Orchestrator) createWorkflow(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Template string `json:"template"` // "outsourcing" or custom
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	var wf *workflow.Workflow
	switch req.Template {
	case "outsourcing":
		wf = workflow.NewOutsourcingWorkflow(req.Name)
	default:
		wf = workflow.NewWorkflow(fmt.Sprintf("wf-%d", time.Now().UnixNano()), req.Name)
	}

	o.workflowMu.Lock()
	o.workflows[wf.ID] = wf
	o.workflowMu.Unlock()

	c.JSON(201, gin.H{
		"id":      wf.ID,
		"name":    wf.Name,
		"status":  wf.Status,
		"summary": wf.Summary(),
	})
}

func (o *Orchestrator) listWorkflows(c *gin.Context) {
	o.workflowMu.RLock()
	defer o.workflowMu.RUnlock()

	result := make([]map[string]interface{}, 0, len(o.workflows))
	for _, wf := range o.workflows {
		result = append(result, wf.Summary())
	}

	c.JSON(200, gin.H{"workflows": result, "count": len(result)})
}

func (o *Orchestrator) getWorkflow(c *gin.Context) {
	id := c.Param("id")

	o.workflowMu.RLock()
	wf, exists := o.workflows[id]
	o.workflowMu.RUnlock()

	if !exists {
		c.JSON(404, gin.H{"error": "workflow not found"})
		return
	}

	// 返回工作流定义和 DAG 图
	c.JSON(200, gin.H{
		"id":      wf.ID,
		"name":    wf.Name,
		"status":  wf.Status,
		"summary": wf.Summary(),
		"dag":     wf.ToDAGGraph(),
	})
}

func (o *Orchestrator) listWorkflowExecutions(c *gin.Context) {
	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	executions := o.workflowViz.ListExecutions(limit)
	c.JSON(200, gin.H{
		"executions": executions,
		"count":      len(executions),
	})
}

func (o *Orchestrator) getWorkflowExecution(c *gin.Context) {
	execID := c.Param("exec_id")

	exec := o.workflowViz.GetExecution(execID)
	if exec == nil {
		c.JSON(404, gin.H{"error": "execution not found"})
		return
	}

	c.JSON(200, gin.H{
		"execution": exec,
		"dag":       exec.ToDAGGraph(),
	})
}

// ---- Container metrics handlers ----

func (o *Orchestrator) listContainers(c *gin.Context) {
	containers := o.containerMonitor.GetAllMetrics()
	c.JSON(200, gin.H{
		"containers": containers,
		"count":      len(containers),
		"timestamp":  time.Now().UnixMilli(),
	})
}

func (o *Orchestrator) getContainerMetrics(c *gin.Context) {
	containerID := c.Param("id")

	// 支持短 ID 匹配
	metrics := o.containerMonitor.GetMetrics(containerID)
	if metrics == nil {
		// 尝试匹配短 ID
		allMetrics := o.containerMonitor.GetAllMetrics()
		for _, m := range allMetrics {
			if strings.HasPrefix(m.ContainerID, containerID) || m.ContainerName == containerID {
				metrics = m
				break
			}
		}
	}

	if metrics == nil {
		c.JSON(404, gin.H{"error": "container not found"})
		return
	}

	c.JSON(200, metrics)
}

func (o *Orchestrator) runWorkflow(c *gin.Context) {
	id := c.Param("id")

	o.workflowMu.RLock()
	wf, exists := o.workflows[id]
	o.workflowMu.RUnlock()

	if !exists {
		c.JSON(404, gin.H{"error": "workflow not found"})
		return
	}

	// 为每个步骤绑定 LLM 执行函数
	for _, step := range wf.Steps {
		if step.Func == nil {
			agentType := step.AgentType
			stepDesc := step.Description
			step.Func = func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				prompt := fmt.Sprintf("你是一个 %s 角色。请完成以下任务: %s\n上下文: %v", agentType, stepDesc, input)

				var response string
				if o.ollamaClient != nil {
					resp, err := o.ollamaClient.ChatWithSystem(ctx, llm.AgentSystemPrompt(agentType), prompt)
					if err == nil {
						response = resp
					}
				}
				if response == "" && o.llmClient != nil {
					resp, err := o.llmClient.ChatWithSystem(ctx, llm.AgentSystemPrompt(agentType), prompt)
					if err != nil {
						return nil, err
					}
					response = resp
				}
				if response == "" {
					response = fmt.Sprintf("[模拟] %s 完成了任务: %s", agentType, stepDesc)
				}

				return map[string]interface{}{
					"agent_type": agentType,
					"result":     response,
				}, nil
			}
		}
	}

	// 创建带追踪的工作流
	trackedWf := workflow.NewTrackedWorkflow(wf, o.workflowViz)

	// 异步执行工作流
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := trackedWf.Run(ctx); err != nil {
			log.Printf("[Workflow] %s failed: %v", wf.ID, err)
		} else {
			log.Printf("[Workflow] %s completed successfully", wf.ID)
		}
		// 广播工作流完成事件
		o.broadcast <- WSMessage{
			Type:    "workflow_complete",
			From:    "orchestrator",
			Content: wf.Summary(),
		}
	}()

	c.JSON(200, gin.H{
		"message": "workflow started",
		"id":      wf.ID,
		"status":  "running",
	})
}

// ---- Security handlers ----

func (o *Orchestrator) generateToken(c *gin.Context) {
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.UserID == "" {
		c.JSON(400, gin.H{"error": "user_id is required"})
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}

	token := security.GenerateToken(req.UserID, req.Role, o.jwtSecret)

	c.JSON(200, gin.H{
		"token":      token,
		"user_id":    req.UserID,
		"role":       req.Role,
		"expires_in": 86400,
	})
}

func (o *Orchestrator) quotaStats(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	if userIDStr == "" {
		userIDStr = "anonymous"
	}

	stats := o.quotaManager.GetUsageStats(userIDStr)
	c.JSON(200, stats)
}

func (o *Orchestrator) piiCheck(c *gin.Context) {
	var req struct {
		Text string `json:"text"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	detections := o.piiSanitizer.Detect(req.Text)
	sanitized := o.piiSanitizer.Sanitize(req.Text)

	c.JSON(200, gin.H{
		"has_pii":    len(detections) > 0,
		"detections": detections,
		"sanitized":  sanitized,
		"original":   req.Text,
	})
}

// ---- Helpers ----

func methodToAuditAction(method string) string {
	switch method {
	case "GET":
		return "read"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "api_call"
	}
}

func parseResourcePath(fullPath, paramID string) (string, string) {
	switch {
	case containsStr(fullPath, "/agents"):
		return "agent", paramID
	case containsStr(fullPath, "/tasks"):
		return "task", paramID
	case containsStr(fullPath, "/chat"):
		return "chat", ""
	case containsStr(fullPath, "/workflows"):
		return "workflow", paramID
	case containsStr(fullPath, "/rag"):
		return "rag", ""
	case containsStr(fullPath, "/auth"):
		return "auth", ""
	case containsStr(fullPath, "/quota"):
		return "quota", ""
	case containsStr(fullPath, "/pii"):
		return "pii", ""
	default:
		return "system", ""
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	orch := NewOrchestrator(configPath)
	orch.Run()
}
