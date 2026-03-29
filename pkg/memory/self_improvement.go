package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================
// AI Agent Memory System - 自我迭代与经验共享
// 参考: MemGPT/Letta Memory Blocks, Reflexion Framework
// ============================================

// MemoryType 记忆类型
type MemoryType string

const (
	MemoryTypeShortTerm  MemoryType = "short_term"  // 短期记忆（工作上下文）
	MemoryTypeLongTerm   MemoryType = "long_term"   // 长期记忆（持久化经验）
	MemoryTypeReflection MemoryType = "reflection"  // 反思记忆（自我改进）
	MemoryTypeSkill      MemoryType = "skill"       // 技能记忆（动态学习）
	MemoryTypeShared     MemoryType = "shared"      // 共享记忆（Agent间传递）
)

// MemoryBlock 记忆块 - 结构化记忆单元
type MemoryBlock struct {
	ID          string                 `json:"id"`
	AgentID     string                 `json:"agent_id"`
	Type        MemoryType             `json:"type"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	Importance  float64                `json:"importance"`  // 重要性评分 0-1
	AccessCount int                    `json:"access_count"`
	Metadata    map[string]interface{} `json:"metadata"`
	Embedding   []float32              `json:"embedding,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	LastAccess  time.Time              `json:"last_access"`
	ExpiresAt   *time.Time             `json:"expires_at,omitempty"`
}

// Experience 经验记录
type Experience struct {
	ID           string                 `json:"id"`
	AgentID      string                 `json:"agent_id"`
	TaskID       string                 `json:"task_id"`
	TaskType     string                 `json:"task_type"`
	Input        string                 `json:"input"`
	Output       string                 `json:"output"`
	Success      bool                   `json:"success"`
	Lessons      []string               `json:"lessons"`      // 学到的教训
	Patterns     []string               `json:"patterns"`     // 识别的模式
	Suggestions  []string               `json:"suggestions"`  // 改进建议
	Confidence   float64                `json:"confidence"`   // 置信度
	Metadata     map[string]interface{} `json:"metadata"`
	CreatedAt    time.Time              `json:"created_at"`
}

// Reflection 反思记录
type Reflection struct {
	ID             string                 `json:"id"`
	AgentID        string                 `json:"agent_id"`
	TriggerTaskID  string                 `json:"trigger_task_id"`
	TriggerType    string                 `json:"trigger_type"` // success, failure, timeout
	Analysis       string                 `json:"analysis"`     // 分析过程
	Insights       []string               `json:"insights"`     // 洞察
	ActionItems    []string               `json:"action_items"` // 行动项
	SkillLearned   *SkillDefinition       `json:"skill_learned,omitempty"`
	Confidence     float64                `json:"confidence"`
	Metadata       map[string]interface{} `json:"metadata"`
	CreatedAt      time.Time              `json:"created_at"`
}

// SkillDefinition 技能定义
type SkillDefinition struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Category    string                 `json:"category"`
	Template    string                 `json:"template"`    // 执行模板
	Parameters  map[string]ParamDef    `json:"parameters"`  // 参数定义
	Examples    []SkillExample         `json:"examples"`    // 示例
	SuccessRate float64                `json:"success_rate"`
	UseCount    int                    `json:"use_count"`
	Source      string                 `json:"source"`      // learned, predefined
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ParamDef 参数定义
type ParamDef struct {
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
	Description string      `json:"description"`
}

// SkillExample 技能示例
type SkillExample struct {
	Input  map[string]interface{} `json:"input"`
	Output map[string]interface{} `json:"output"`
	Result string                 `json:"result"`
}

// MemoryStore 记忆存储接口
type MemoryStore interface {
	// Memory Block 操作
	StoreBlock(ctx context.Context, block *MemoryBlock) error
	GetBlock(ctx context.Context, id string) (*MemoryBlock, error)
	QueryBlocks(ctx context.Context, agentID string, memType MemoryType, limit int) ([]*MemoryBlock, error)
	SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]*MemoryBlock, error)

	// Experience 操作
	StoreExperience(ctx context.Context, exp *Experience) error
	GetExperiences(ctx context.Context, agentID string, limit int) ([]*Experience, error)

	// Reflection 操作
	StoreReflection(ctx context.Context, ref *Reflection) error
	GetReflections(ctx context.Context, agentID string, limit int) ([]*Reflection, error)

	// Skill 操作
	StoreSkill(ctx context.Context, skill *SkillDefinition) error
	GetSkill(ctx context.Context, name string) (*SkillDefinition, error)
	ListSkills(ctx context.Context, category string) ([]*SkillDefinition, error)
}

// MemoryManager 记忆管理器
type MemoryManager struct {
	store       MemoryStore
	shortTerm   map[string][]*MemoryBlock // agentID -> short term memories
	mu          sync.RWMutex
	maxShortTerm int // 短期记忆最大数量
}

// NewMemoryManager 创建记忆管理器
func NewMemoryManager(store MemoryStore, maxShortTerm int) *MemoryManager {
	if maxShortTerm <= 0 {
		maxShortTerm = 10
	}
	return &MemoryManager{
		store:       store,
		shortTerm:   make(map[string][]*MemoryBlock),
		maxShortTerm: maxShortTerm,
	}
}

// AddShortTermMemory 添加短期记忆
func (mm *MemoryManager) AddShortTermMemory(ctx context.Context, agentID, title, content string, importance float64) (*MemoryBlock, error) {
	block := &MemoryBlock{
		ID:         fmt.Sprintf("mem-%d", time.Now().UnixNano()),
		AgentID:    agentID,
		Type:       MemoryTypeShortTerm,
		Title:      title,
		Content:    content,
		Importance: importance,
		Metadata:   make(map[string]interface{}),
		CreatedAt:  time.Now(),
		LastAccess: time.Now(),
	}

	// 设置短期记忆过期时间（默认1小时）
	expires := time.Now().Add(time.Hour)
	block.ExpiresAt = &expires

	// 添加到内存
	mm.mu.Lock()
	defer mm.mu.Unlock()

	memories := mm.shortTerm[agentID]
	memories = append(memories, block)

	// 超过限制，移除最不重要的
	if len(memories) > mm.maxShortTerm {
		// 按重要性排序
		for i := 0; i < len(memories)-1; i++ {
			for j := i + 1; j < len(memories); j++ {
				if memories[i].Importance < memories[j].Importance {
					memories[i], memories[j] = memories[j], memories[i]
				}
			}
		}
		memories = memories[:mm.maxShortTerm]
	}

	mm.shortTerm[agentID] = memories

	// 持久化到存储
	if err := mm.store.StoreBlock(ctx, block); err != nil {
		log.Printf("[Memory] Warning: failed to persist short-term memory: %v", err)
	}

	return block, nil
}

// GetShortTermMemories 获取短期记忆
func (mm *MemoryManager) GetShortTermMemories(agentID string) []*MemoryBlock {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	memories := mm.shortTerm[agentID]
	now := time.Now()

	// 过滤过期的记忆
	var active []*MemoryBlock
	for _, m := range memories {
		if m.ExpiresAt == nil || m.ExpiresAt.After(now) {
			m.AccessCount++
			m.LastAccess = now
			active = append(active, m)
		}
	}

	return active
}

// ConsolidateToLongTerm 将短期记忆固化为长期记忆
func (mm *MemoryManager) ConsolidateToLongTerm(ctx context.Context, agentID string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	memories := mm.shortTerm[agentID]
	var consolidated int

	for _, m := range memories {
		// 高重要性或高访问次数的记忆固化为长期记忆
		if m.Importance > 0.7 || m.AccessCount > 3 {
			longTerm := &MemoryBlock{
				ID:          fmt.Sprintf("lt-%d", time.Now().UnixNano()),
				AgentID:     agentID,
				Type:        MemoryTypeLongTerm,
				Title:       m.Title,
				Content:     m.Content,
				Importance:  m.Importance,
				AccessCount: m.AccessCount,
				Metadata:    m.Metadata,
				CreatedAt:   m.CreatedAt,
				LastAccess:  time.Now(),
			}

			if err := mm.store.StoreBlock(ctx, longTerm); err != nil {
				log.Printf("[Memory] Failed to consolidate memory %s: %v", m.ID, err)
				continue
			}
			consolidated++
		}
	}

	// 清空短期记忆
	mm.shortTerm[agentID] = nil

	log.Printf("[Memory] Consolidated %d memories to long-term for agent %s", consolidated, agentID)
	return nil
}

// ============================================
// Reflection Engine - 自我反思引擎
// ============================================

// ReflectionEngine 反思引擎
type ReflectionEngine struct {
	store       MemoryStore
	llmClient   LLMClient
}

// LLMClient LLM 客户端接口
type LLMClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
	GenerateWithSystem(ctx context.Context, system, prompt string) (string, error)
}

// EmbeddingClient 向量嵌入客户端接口
type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// EmbeddingClientAdapter 适配外部 EmbeddingClient 到 memory 包接口
type EmbeddingClientAdapter struct {
	embedFunc func(ctx context.Context, text string) ([]float32, error)
}

// NewEmbeddingClientAdapter 创建 EmbeddingClient 适配器
func NewEmbeddingClientAdapter(embedFunc func(ctx context.Context, text string) ([]float32, error)) *EmbeddingClientAdapter {
	return &EmbeddingClientAdapter{embedFunc: embedFunc}
}

func (a *EmbeddingClientAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return a.embedFunc(ctx, text)
}

// NewReflectionEngine 创建反思引擎
func NewReflectionEngine(store MemoryStore, llmClient LLMClient) *ReflectionEngine {
	return &ReflectionEngine{
		store:     store,
		llmClient: llmClient,
	}
}

// ReflectOnTask 对任务进行反思
func (re *ReflectionEngine) ReflectOnTask(ctx context.Context, agentID, taskID string, taskResult *TaskResult) (*Reflection, error) {
	// 构建反思提示
	prompt := re.buildReflectionPrompt(taskResult)

	// 调用 LLM 进行反思
	response, err := re.llmClient.GenerateWithSystem(ctx, re.getReflectionSystemPrompt(), prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM reflection failed: %w", err)
	}

	// 解析反思结果
	reflection := re.parseReflectionResponse(response)
	reflection.ID = fmt.Sprintf("ref-%d", time.Now().UnixNano())
	reflection.AgentID = agentID
	reflection.TriggerTaskID = taskID
	reflection.CreatedAt = time.Now()

	// 根据任务结果设置触发类型
	if taskResult.Success {
		reflection.TriggerType = "success"
	} else if taskResult.Timeout {
		reflection.TriggerType = "timeout"
	} else {
		reflection.TriggerType = "failure"
	}

	// 存储反思
	if err := re.store.StoreReflection(ctx, reflection); err != nil {
		log.Printf("[Reflection] Failed to store reflection: %v", err)
	}

	// 如果学到了新技能，存储技能
	if reflection.SkillLearned != nil {
		reflection.SkillLearned.Source = "learned"
		reflection.SkillLearned.CreatedAt = time.Now()
		reflection.SkillLearned.UpdatedAt = time.Now()
		if err := re.store.StoreSkill(ctx, reflection.SkillLearned); err != nil {
			log.Printf("[Reflection] Failed to store learned skill: %v", err)
		}
	}

	return reflection, nil
}

// TaskResult 任务结果
type TaskResult struct {
	TaskID       string                 `json:"task_id"`
	TaskType     string                 `json:"task_type"`
	Input        map[string]interface{} `json:"input"`
	Output       map[string]interface{} `json:"output"`
	Success      bool                   `json:"success"`
	Timeout      bool                   `json:"timeout"`
	Error        string                 `json:"error,omitempty"`
	TokensUsed   int                    `json:"tokens_used"`
	LatencyMs    int                    `json:"latency_ms"`
	RetryCount   int                    `json:"retry_count"`
}

func (re *ReflectionEngine) buildReflectionPrompt(result *TaskResult) string {
	var sb strings.Builder

	sb.WriteString("请对以下任务执行结果进行深度反思分析：\n\n")
	sb.WriteString(fmt.Sprintf("任务类型: %s\n", result.TaskType))
	sb.WriteString(fmt.Sprintf("执行状态: %s\n", map[bool]string{true: "成功", false: "失败"}[result.Success]))

	if result.Error != "" {
		sb.WriteString(fmt.Sprintf("错误信息: %s\n", result.Error))
	}

	sb.WriteString(fmt.Sprintf("\n输入数据:\n%v\n", result.Input))
	sb.WriteString(fmt.Sprintf("\n输出数据:\n%v\n", result.Output))
	sb.WriteString(fmt.Sprintf("\n重试次数: %d\n", result.RetryCount))
	sb.WriteString(fmt.Sprintf("Token 消耗: %d\n", result.TokensUsed))
	sb.WriteString(fmt.Sprintf("执行耗时: %dms\n", result.LatencyMs))

	sb.WriteString("\n请从以下角度进行分析：\n")
	sb.WriteString("1. 成功/失败的根本原因是什么？\n")
	sb.WriteString("2. 有哪些可以改进的地方？\n")
	sb.WriteString("3. 是否发现了可复用的模式或方法？\n")
	sb.WriteString("4. 如果是失败，如何避免类似问题？\n")
	sb.WriteString("5. 是否可以从中提炼出新的技能？\n")

	return sb.String()
}

func (re *ReflectionEngine) getReflectionSystemPrompt() string {
	return `你是一个 AI Agent 的自我反思系统。你的任务是分析任务执行结果，提取有价值的经验和教训。

请以 JSON 格式输出分析结果：
{
  "analysis": "详细的分析过程",
  "insights": ["洞察1", "洞察2"],
  "action_items": ["改进行动1", "改进行动2"],
  "confidence": 0.85,
  "skill_learned": {
    "name": "技能名称",
    "description": "技能描述",
    "category": "技能类别",
    "template": "执行模板或代码片段"
  }
}

注意：
- 只有在确实发现了可复用的新技能时才填写 skill_learned
- confidence 表示对分析结果的置信度 (0-1)
- insights 应该是深刻的、可操作的洞察`
}

func (re *ReflectionEngine) parseReflectionResponse(response string) *Reflection {
	reflection := &Reflection{
		Insights:    []string{},
		ActionItems: []string{},
		Metadata:    make(map[string]interface{}),
		Confidence:  0.5,
	}

	// 尝试解析 JSON
	var parsed struct {
		Analysis    string   `json:"analysis"`
		Insights    []string `json:"insights"`
		ActionItems []string `json:"action_items"`
		Confidence  float64  `json:"confidence"`
		SkillLearned *struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Category    string `json:"category"`
			Template    string `json:"template"`
		} `json:"skill_learned"`
	}

	// 提取 JSON 部分
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		jsonStr := response[jsonStart : jsonEnd+1]
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			reflection.Analysis = parsed.Analysis
			reflection.Insights = parsed.Insights
			reflection.ActionItems = parsed.ActionItems
			reflection.Confidence = parsed.Confidence

			if parsed.SkillLearned != nil && parsed.SkillLearned.Name != "" {
				reflection.SkillLearned = &SkillDefinition{
					ID:          fmt.Sprintf("skill-%d", time.Now().UnixNano()),
					Name:        parsed.SkillLearned.Name,
					Description: parsed.SkillLearned.Description,
					Category:    parsed.SkillLearned.Category,
					Template:    parsed.SkillLearned.Template,
					Parameters:  make(map[string]ParamDef),
					SuccessRate: 1.0,
				}
			}
		}
	}

	if reflection.Analysis == "" {
		reflection.Analysis = response
	}

	return reflection
}

// ============================================
// Experience Extractor - 经验提取器
// ============================================

// ExperienceExtractor 经验提取器
type ExperienceExtractor struct {
	store     MemoryStore
	llmClient LLMClient
}

// NewExperienceExtractor 创建经验提取器
func NewExperienceExtractor(store MemoryStore, llmClient LLMClient) *ExperienceExtractor {
	return &ExperienceExtractor{
		store:     store,
		llmClient: llmClient,
	}
}

// ExtractExperience 从任务结果提取经验
func (ee *ExperienceExtractor) ExtractExperience(ctx context.Context, agentID string, result *TaskResult) (*Experience, error) {
	prompt := ee.buildExtractionPrompt(result)

	response, err := ee.llmClient.GenerateWithSystem(ctx, ee.getExtractionSystemPrompt(), prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM extraction failed: %w", err)
	}

	exp := ee.parseExperienceResponse(response)
	exp.ID = fmt.Sprintf("exp-%d", time.Now().UnixNano())
	exp.AgentID = agentID
	exp.TaskID = result.TaskID
	exp.TaskType = result.TaskType
	exp.Success = result.Success
	exp.CreatedAt = time.Now()

	// 存储经验
	if err := ee.store.StoreExperience(ctx, exp); err != nil {
		log.Printf("[Experience] Failed to store experience: %v", err)
	}

	return exp, nil
}

func (ee *ExperienceExtractor) buildExtractionPrompt(result *TaskResult) string {
	var sb strings.Builder

	sb.WriteString("请从以下任务执行中提取可复用的经验：\n\n")
	sb.WriteString(fmt.Sprintf("任务类型: %s\n", result.TaskType))
	sb.WriteString(fmt.Sprintf("执行状态: %s\n", map[bool]string{true: "成功", false: "失败"}[result.Success]))
	sb.WriteString(fmt.Sprintf("输入: %v\n", result.Input))
	sb.WriteString(fmt.Sprintf("输出: %v\n", result.Output))

	if result.Error != "" {
		sb.WriteString(fmt.Sprintf("错误: %s\n", result.Error))
	}

	return sb.String()
}

func (ee *ExperienceExtractor) getExtractionSystemPrompt() string {
	return `你是一个经验提取系统。从任务执行结果中提取可复用的经验和教训。

请以 JSON 格式输出：
{
  "input_summary": "输入的简要描述",
  "output_summary": "输出的简要描述",
  "lessons": ["教训1", "教训2"],
  "patterns": ["发现的模式1", "发现的模式2"],
  "suggestions": ["改进建议1", "改进建议2"],
  "confidence": 0.85
}`
}

func (ee *ExperienceExtractor) parseExperienceResponse(response string) *Experience {
	exp := &Experience{
		Lessons:     []string{},
		Patterns:    []string{},
		Suggestions: []string{},
		Metadata:    make(map[string]interface{}),
		Confidence:  0.5,
	}

	var parsed struct {
		InputSummary  string   `json:"input_summary"`
		OutputSummary string   `json:"output_summary"`
		Lessons       []string `json:"lessons"`
		Patterns      []string `json:"patterns"`
		Suggestions   []string `json:"suggestions"`
		Confidence    float64  `json:"confidence"`
	}

	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		jsonStr := response[jsonStart : jsonEnd+1]
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			exp.Input = parsed.InputSummary
			exp.Output = parsed.OutputSummary
			exp.Lessons = parsed.Lessons
			exp.Patterns = parsed.Patterns
			exp.Suggestions = parsed.Suggestions
			exp.Confidence = parsed.Confidence
		}
	}

	return exp
}

// ============================================
// Knowledge Sharing - 知识共享机制
// ============================================

// KnowledgeSharer 知识共享器
type KnowledgeSharer struct {
	store           MemoryStore
	embeddingClient EmbeddingClient
}

// NewKnowledgeSharer 创建知识共享器
func NewKnowledgeSharer(store MemoryStore) *KnowledgeSharer {
	return &KnowledgeSharer{store: store}
}

// SetEmbeddingClient 设置向量嵌入客户端
func (ks *KnowledgeSharer) SetEmbeddingClient(client EmbeddingClient) {
	ks.embeddingClient = client
}

// ShareExperience 共享经验给其他 Agent
func (ks *KnowledgeSharer) ShareExperience(ctx context.Context, exp *Experience, targetAgentIDs []string) error {
	for _, agentID := range targetAgentIDs {
		content := fmt.Sprintf("来源Agent: %s\n任务类型: %s\n教训: %v\n模式: %v", exp.AgentID, exp.TaskType, exp.Lessons, exp.Patterns)

		sharedBlock := &MemoryBlock{
			ID:         fmt.Sprintf("shared-%d-%s", time.Now().UnixNano(), agentID),
			AgentID:    agentID,
			Type:       MemoryTypeShared,
			Title:      fmt.Sprintf("共享经验: %s", exp.TaskType),
			Content:    content,
			Importance: exp.Confidence,
			Metadata: map[string]interface{}{
				"source_agent_id": exp.AgentID,
				"source_task_id":  exp.TaskID,
				"experience_id":   exp.ID,
			},
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
		}

		// 生成向量嵌入
		if ks.embeddingClient != nil {
			if embedding, err := ks.embeddingClient.Embed(ctx, content); err == nil {
				sharedBlock.Embedding = embedding
			} else {
				log.Printf("[KnowledgeShare] Failed to generate embedding for shared experience: %v", err)
			}
		}

		if err := ks.store.StoreBlock(ctx, sharedBlock); err != nil {
			log.Printf("[KnowledgeShare] Failed to share to agent %s: %v", agentID, err)
			continue
		}
	}

	log.Printf("[KnowledgeShare] Shared experience %s to %d agents", exp.ID, len(targetAgentIDs))
	return nil
}

// ShareSkill 共享技能给其他 Agent
func (ks *KnowledgeSharer) ShareSkill(ctx context.Context, skill *SkillDefinition, targetAgentIDs []string) error {
	for _, agentID := range targetAgentIDs {
		content := fmt.Sprintf("%s\n\n模板:\n%s", skill.Description, skill.Template)

		sharedBlock := &MemoryBlock{
			ID:         fmt.Sprintf("skill-shared-%d-%s", time.Now().UnixNano(), agentID),
			AgentID:    agentID,
			Type:       MemoryTypeSkill,
			Title:      fmt.Sprintf("共享技能: %s", skill.Name),
			Content:    content,
			Importance: skill.SuccessRate,
			Metadata: map[string]interface{}{
				"skill_id":   skill.ID,
				"skill_name": skill.Name,
				"category":   skill.Category,
			},
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
		}

		// 生成向量嵌入
		if ks.embeddingClient != nil {
			if embedding, err := ks.embeddingClient.Embed(ctx, content); err == nil {
				sharedBlock.Embedding = embedding
			} else {
				log.Printf("[KnowledgeShare] Failed to generate embedding for shared skill: %v", err)
			}
		}

		if err := ks.store.StoreBlock(ctx, sharedBlock); err != nil {
			log.Printf("[KnowledgeShare] Failed to share skill to agent %s: %v", agentID, err)
			continue
		}
	}

	log.Printf("[KnowledgeShare] Shared skill %s to %d agents", skill.Name, len(targetAgentIDs))
	return nil
}

// ============================================
// Self-Improvement Loop - 自我改进循环
// ============================================

// SelfImprovementLoop 自我改进循环
type SelfImprovementLoop struct {
	store           MemoryStore
	memoryManager   *MemoryManager
	reflection      *ReflectionEngine
	extractor       *ExperienceExtractor
	sharer          *KnowledgeSharer
	agentIDs        []string // 所有 Agent ID 列表
	agentIDsMu      sync.RWMutex
	embeddingClient EmbeddingClient // 向量嵌入客户端
	taskCounters    sync.Map        // agentID -> *int32, 每个 Agent 的任务计数
	consolidateN    int             // 每处理 N 个任务触发一次固化
}

// NewSelfImprovementLoop 创建自我改进循环
func NewSelfImprovementLoop(
	store MemoryStore,
	llmClient LLMClient,
	agentIDs []string,
	embeddingClient EmbeddingClient,
) *SelfImprovementLoop {
	sharer := NewKnowledgeSharer(store)
	if embeddingClient != nil {
		sharer.SetEmbeddingClient(embeddingClient)
	}

	return &SelfImprovementLoop{
		store:           store,
		memoryManager:   NewMemoryManager(store, 10),
		reflection:      NewReflectionEngine(store, llmClient),
		extractor:       NewExperienceExtractor(store, llmClient),
		sharer:          sharer,
		agentIDs:        agentIDs,
		embeddingClient: embeddingClient,
		consolidateN:    10, // 默认每 10 个任务固化一次
	}
}

// SetAgentIDs 设置所有 Agent ID 列表（线程安全）
func (sil *SelfImprovementLoop) SetAgentIDs(agentIDs []string) {
	sil.agentIDsMu.Lock()
	defer sil.agentIDsMu.Unlock()
	// 复制切片避免外部修改
	sil.agentIDs = append([]string(nil), agentIDs...)
	log.Printf("[SelfImprove] Agent IDs updated: %v", sil.agentIDs)
}

// GetAgentIDs 获取当前 Agent ID 列表（线程安全）
func (sil *SelfImprovementLoop) GetAgentIDs() []string {
	sil.agentIDsMu.RLock()
	defer sil.agentIDsMu.RUnlock()
	return append([]string(nil), sil.agentIDs...)
}

// AddAgentID 添加单个 Agent ID（线程安全）
func (sil *SelfImprovementLoop) AddAgentID(agentID string) {
	sil.agentIDsMu.Lock()
	defer sil.agentIDsMu.Unlock()
	// 检查是否已存在
	for _, id := range sil.agentIDs {
		if id == agentID {
			return
		}
	}
	sil.agentIDs = append(sil.agentIDs, agentID)
	log.Printf("[SelfImprove] Agent added: %s, current list: %v", agentID, sil.agentIDs)
}

// RemoveAgentID 移除单个 Agent ID（线程安全）
func (sil *SelfImprovementLoop) RemoveAgentID(agentID string) {
	sil.agentIDsMu.Lock()
	defer sil.agentIDsMu.Unlock()
	for i, id := range sil.agentIDs {
		if id == agentID {
			sil.agentIDs = append(sil.agentIDs[:i], sil.agentIDs[i+1:]...)
			log.Printf("[SelfImprove] Agent removed: %s, current list: %v", agentID, sil.agentIDs)
			return
		}
	}
}

// embedContent 为文本生成向量嵌入（带降级处理）
func (sil *SelfImprovementLoop) embedContent(ctx context.Context, content string) []float32 {
	if sil.embeddingClient == nil {
		log.Printf("[SelfImprove] Embedding client not configured, skipping vector generation")
		return nil
	}

	embedding, err := sil.embeddingClient.Embed(ctx, content)
	if err != nil {
		log.Printf("[SelfImprove] Failed to generate embedding: %v", err)
		return nil
	}

	return embedding
}

// ProcessTaskResult 处理任务结果（完整的自我改进流程）
func (sil *SelfImprovementLoop) ProcessTaskResult(ctx context.Context, agentID string, result *TaskResult) error {
	// 获取当前 Agent 列表（线程安全）
	agentIDs := sil.GetAgentIDs()

	// 1. 提取经验
	exp, err := sil.extractor.ExtractExperience(ctx, agentID, result)
	if err != nil {
		log.Printf("[SelfImprove] Experience extraction failed: %v", err)
	} else {
		// 添加到短期记忆
		sil.memoryManager.AddShortTermMemory(ctx, agentID,
			fmt.Sprintf("经验: %s", result.TaskType),
			fmt.Sprintf("教训: %v", exp.Lessons),
			exp.Confidence,
		)

		// 共享经验给其他 Agent
		var otherAgents []string
		for _, id := range agentIDs {
			if id != agentID {
				otherAgents = append(otherAgents, id)
			}
		}
		if len(otherAgents) > 0 {
			sil.sharer.ShareExperience(ctx, exp, otherAgents)
		} else {
			log.Printf("[SelfImprove] No other agents to share experience, agentIDs: %v", agentIDs)
		}
	}

	// 2. 反思分析
	ref, err := sil.reflection.ReflectOnTask(ctx, agentID, result.TaskID, result)
	if err != nil {
		log.Printf("[SelfImprove] Reflection failed: %v", err)
	} else {
		// 存储反思记忆
		reflectionBlock := &MemoryBlock{
			ID:         fmt.Sprintf("reflection-%d", time.Now().UnixNano()),
			AgentID:    agentID,
			Type:       MemoryTypeReflection,
			Title:      fmt.Sprintf("反思: %s", result.TaskType),
			Content:    ref.Analysis,
			Importance: ref.Confidence,
			Metadata: map[string]interface{}{
				"insights":     ref.Insights,
				"action_items": ref.ActionItems,
			},
			CreatedAt:  time.Now(),
			LastAccess: time.Now(),
		}

		// 为反思内容生成向量嵌入
		reflectionBlock.Embedding = sil.embedContent(ctx, ref.Analysis)

		sil.store.StoreBlock(ctx, reflectionBlock)

		// 如果学到了新技能，共享给其他 Agent
		if ref.SkillLearned != nil {
			var otherAgents []string
			for _, id := range agentIDs {
				if id != agentID {
					otherAgents = append(otherAgents, id)
				}
			}
			if len(otherAgents) > 0 {
				sil.sharer.ShareSkill(ctx, ref.SkillLearned, otherAgents)
			}
		}
	}

	// 3. 定期固化长期记忆（基于任务计数）
	if sil.shouldConsolidate(agentID) {
		sil.memoryManager.ConsolidateToLongTerm(ctx, agentID)
	}

	return nil
}

// shouldConsolidate 判断是否应该固化记忆
// 策略：每处理 N 个任务固化一次（默认 N=10）
func (sil *SelfImprovementLoop) shouldConsolidate(agentID string) bool {
	// 获取或创建该 Agent 的计数器
	var counter *int32
	if v, ok := sil.taskCounters.Load(agentID); ok {
		counter = v.(*int32)
	} else {
		var newCounter int32
		counter = &newCounter
		sil.taskCounters.Store(agentID, counter)
	}

	// 原子递增并检查
	count := atomic.AddInt32(counter, 1)
	if count >= int32(sil.consolidateN) {
		// 重置计数器
		atomic.StoreInt32(counter, 0)
		log.Printf("[SelfImprove] Consolidating memories for agent %s after %d tasks", agentID, count)
		return true
	}
	return false
}

// GetRelevantMemories 获取相关记忆（用于任务执行前的上下文构建）
// 支持基于 taskType 和任务描述的语义检索
func (sil *SelfImprovementLoop) GetRelevantMemories(ctx context.Context, agentID, taskType string) ([]*MemoryBlock, error) {
	return sil.GetRelevantMemoriesWithQuery(ctx, agentID, taskType, "")
}

// GetRelevantMemoriesWithQuery 获取相关记忆，支持语义检索
// agentID: Agent 标识
// taskType: 任务类型（用于过滤和检索）
// query: 可选的查询文本，用于语义相似度检索
func (sil *SelfImprovementLoop) GetRelevantMemoriesWithQuery(ctx context.Context, agentID, taskType, query string) ([]*MemoryBlock, error) {
	var allMemories []*MemoryBlock
	seenIDs := make(map[string]bool)

	// 辅助函数：去重添加
	addMemories := func(memories []*MemoryBlock) {
		for _, m := range memories {
			if !seenIDs[m.ID] {
				seenIDs[m.ID] = true
				allMemories = append(allMemories, m)
			}
		}
	}

	// 1. 获取短期记忆（高优先级，来自内存）
	shortTerm := sil.memoryManager.GetShortTermMemories(agentID)
	addMemories(shortTerm)

	// 2. 语义相似度检索（如果配置了 embedding client 且有查询文本）
	if sil.embeddingClient != nil && query != "" {
		if embedding, err := sil.embeddingClient.Embed(ctx, query); err == nil {
			// 向量相似度检索 Top5
			similar, err := sil.store.SearchSimilar(ctx, embedding, 5)
			if err != nil {
				log.Printf("[SelfImprove] SearchSimilar failed: %v", err)
			} else {
				// 按类型过滤：优先返回当前 taskType 相关的记忆
				var filtered []*MemoryBlock
				for _, m := range similar {
					// 包含当前 agent 的记忆或共享记忆
					if m.AgentID == agentID || m.Type == MemoryTypeShared {
						// 如果记忆有 task_type 元数据，优先匹配
						if m.Metadata != nil {
							if memTaskType, ok := m.Metadata["task_type"].(string); ok && memTaskType == taskType {
								filtered = append([]*MemoryBlock{m}, filtered...) // 前置匹配项
								continue
							}
						}
						filtered = append(filtered, m)
					}
				}
				addMemories(filtered)
			}
		} else {
			log.Printf("[SelfImprove] Failed to generate query embedding: %v", err)
		}
	}

	// 3. 如果没有查询文本或语义检索失败，按 taskType 查询长期记忆
	longTerm, err := sil.store.QueryBlocks(ctx, agentID, MemoryTypeLongTerm, 5)
	if err != nil {
		log.Printf("[SelfImprove] Failed to query long-term memory: %v", err)
	}
	// 过滤：优先返回与 taskType 相关的记忆
	if taskType != "" {
		var filtered []*MemoryBlock
		for _, m := range longTerm {
			if m.Metadata != nil {
				if memTaskType, ok := m.Metadata["task_type"].(string); ok && memTaskType == taskType {
					filtered = append([]*MemoryBlock{m}, filtered...)
					continue
				}
			}
			filtered = append(filtered, m)
		}
		longTerm = filtered
	}
	addMemories(longTerm)

	// 4. 获取共享记忆
	shared, err := sil.store.QueryBlocks(ctx, agentID, MemoryTypeShared, 3)
	if err != nil {
		log.Printf("[SelfImprove] Failed to query shared memory: %v", err)
	}
	addMemories(shared)

	// 5. 按重要性排序
	for i := 0; i < len(allMemories)-1; i++ {
		for j := i + 1; j < len(allMemories); j++ {
			if allMemories[i].Importance < allMemories[j].Importance {
				allMemories[i], allMemories[j] = allMemories[j], allMemories[i]
			}
		}
	}

	// 限制返回数量
	maxMemories := 15
	if len(allMemories) > maxMemories {
		allMemories = allMemories[:maxMemories]
	}

	return allMemories, nil
}
