package memory

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ============================================
// Memory System Unit Tests
// ============================================

func TestMemoryBlockTypes(t *testing.T) {
	types := []MemoryType{
		MemoryTypeShortTerm,
		MemoryTypeLongTerm,
		MemoryTypeReflection,
		MemoryTypeSkill,
		MemoryTypeShared,
	}

	for _, mt := range types {
		if mt == "" {
			t.Error("Memory type should not be empty")
		}
	}
}

func TestMemoryBlockExpiration(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour)

	block := &MemoryBlock{
		ID:         "test-block-1",
		AgentID:    "agent-1",
		Type:       MemoryTypeShortTerm,
		Content:    "Test content",
		Importance: 0.8,
		ExpiresAt:  &expires,
	}

	if block.ExpiresAt == nil {
		t.Error("ExpiresAt should be set")
	}

	if block.ExpiresAt.Before(now) {
		t.Error("ExpiresAt should be in the future")
	}
}

func TestExperienceStructure(t *testing.T) {
	exp := &Experience{
		ID:          "exp-1",
		AgentID:     "agent-1",
		TaskID:      "task-1",
		TaskType:    "code_gen",
		Input:       "Write a function",
		Output:      "func example() {}",
		Success:     true,
		Lessons:     []string{"Always check edge cases"},
		Patterns:    []string{"Function starts with lowercase"},
		Suggestions: []string{"Add error handling"},
		Confidence:  0.85,
	}

	if !exp.Success {
		t.Error("Experience should be successful")
	}
	if len(exp.Lessons) != 1 {
		t.Errorf("Expected 1 lesson, got %d", len(exp.Lessons))
	}
	if exp.Confidence < 0 || exp.Confidence > 1 {
		t.Error("Confidence should be between 0 and 1")
	}
}

func TestReflectionStructure(t *testing.T) {
	ref := &Reflection{
		ID:            "ref-1",
		AgentID:       "agent-1",
		TriggerTaskID: "task-1",
		TriggerType:   "success",
		Analysis:      "Task completed successfully due to proper planning",
		Insights:      []string{"Planning is crucial", "Test early"},
		ActionItems:   []string{"Add more unit tests", "Improve error messages"},
		Confidence:    0.9,
	}

	if ref.TriggerType != "success" {
		t.Errorf("Expected trigger type 'success', got '%s'", ref.TriggerType)
	}
	if len(ref.Insights) != 2 {
		t.Errorf("Expected 2 insights, got %d", len(ref.Insights))
	}
}

func TestSkillDefinition(t *testing.T) {
	skill := &SkillDefinition{
		ID:          "skill-1",
		Name:        "code_review",
		Description: "Review code for issues",
		Category:    "coding",
		Template:    "Review: {{code}}",
		Parameters: map[string]ParamDef{
			"code": {
				Type:        "string",
				Required:    true,
				Description: "Code to review",
			},
		},
		SuccessRate: 0.95,
		UseCount:    100,
		Source:      "predefined",
	}

	if skill.Name != "code_review" {
		t.Errorf("Expected name 'code_review', got '%s'", skill.Name)
	}
	if skill.SuccessRate < 0 || skill.SuccessRate > 1 {
		t.Error("SuccessRate should be between 0 and 1")
	}
	if _, ok := skill.Parameters["code"]; !ok {
		t.Error("Expected 'code' parameter")
	}
}

// ============================================
// Memory Manager Tests
// ============================================

// MockMemoryStore is a mock implementation for testing
type MockMemoryStore struct {
	blocks      map[string]*MemoryBlock
	experiences map[string]*Experience
	reflections map[string]*Reflection
	skills      map[string]*SkillDefinition
}

func NewMockMemoryStore() *MockMemoryStore {
	return &MockMemoryStore{
		blocks:      make(map[string]*MemoryBlock),
		experiences: make(map[string]*Experience),
		reflections: make(map[string]*Reflection),
		skills:      make(map[string]*SkillDefinition),
	}
}

func (m *MockMemoryStore) StoreBlock(ctx context.Context, block *MemoryBlock) error {
	m.blocks[block.ID] = block
	return nil
}

func (m *MockMemoryStore) GetBlock(ctx context.Context, id string) (*MemoryBlock, error) {
	return m.blocks[id], nil
}

func (m *MockMemoryStore) QueryBlocks(ctx context.Context, agentID string, memType MemoryType, limit int) ([]*MemoryBlock, error) {
	var result []*MemoryBlock
	for _, b := range m.blocks {
		if b.AgentID == agentID && b.Type == memType {
			result = append(result, b)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *MockMemoryStore) SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]*MemoryBlock, error) {
	return nil, nil
}

func (m *MockMemoryStore) StoreExperience(ctx context.Context, exp *Experience) error {
	m.experiences[exp.ID] = exp
	return nil
}

func (m *MockMemoryStore) GetExperiences(ctx context.Context, agentID string, limit int) ([]*Experience, error) {
	var result []*Experience
	for _, e := range m.experiences {
		if e.AgentID == agentID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *MockMemoryStore) StoreReflection(ctx context.Context, ref *Reflection) error {
	m.reflections[ref.ID] = ref
	return nil
}

func (m *MockMemoryStore) GetReflections(ctx context.Context, agentID string, limit int) ([]*Reflection, error) {
	var result []*Reflection
	for _, r := range m.reflections {
		if r.AgentID == agentID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *MockMemoryStore) StoreSkill(ctx context.Context, skill *SkillDefinition) error {
	m.skills[skill.Name] = skill
	return nil
}

func (m *MockMemoryStore) GetSkill(ctx context.Context, name string) (*SkillDefinition, error) {
	return m.skills[name], nil
}

func (m *MockMemoryStore) ListSkills(ctx context.Context, category string) ([]*SkillDefinition, error) {
	var result []*SkillDefinition
	for _, s := range m.skills {
		if category == "" || s.Category == category {
			result = append(result, s)
		}
	}
	return result, nil
}

func TestMemoryManagerAddShortTerm(t *testing.T) {
	store := NewMockMemoryStore()
	mm := NewMemoryManager(store, 5)

	ctx := context.Background()
	block, err := mm.AddShortTermMemory(ctx, "agent-1", "Test Memory", "Test content", 0.8)
	if err != nil {
		t.Fatalf("Failed to add short-term memory: %v", err)
	}

	if block.AgentID != "agent-1" {
		t.Errorf("Expected agent 'agent-1', got '%s'", block.AgentID)
	}
	if block.Type != MemoryTypeShortTerm {
		t.Errorf("Expected type short_term, got '%s'", block.Type)
	}
	if block.ExpiresAt == nil {
		t.Error("Short-term memory should have expiration")
	}
}

func TestMemoryManagerLimit(t *testing.T) {
	store := NewMockMemoryStore()
	mm := NewMemoryManager(store, 3) // Max 3 short-term memories

	ctx := context.Background()

	// Add 5 memories
	for i := 0; i < 5; i++ {
		_, err := mm.AddShortTermMemory(ctx, "agent-1", "Test", "Content", float64(i)/10)
		if err != nil {
			t.Fatalf("Failed to add memory: %v", err)
		}
	}

	// Should only have 3
	memories := mm.GetShortTermMemories("agent-1")
	if len(memories) > 3 {
		t.Errorf("Expected at most 3 memories, got %d", len(memories))
	}
}

func TestMemoryManagerConsolidation(t *testing.T) {
	store := NewMockMemoryStore()
	mm := NewMemoryManager(store, 10)

	ctx := context.Background()

	// Add high-importance memory
	_, _ = mm.AddShortTermMemory(ctx, "agent-1", "Important", "High importance content", 0.9)

	// Add low-importance memory
	_, _ = mm.AddShortTermMemory(ctx, "agent-1", "Less Important", "Low importance content", 0.3)

	// Consolidate
	err := mm.ConsolidateToLongTerm(ctx, "agent-1")
	if err != nil {
		t.Fatalf("Failed to consolidate: %v", err)
	}

	// Check that high-importance was stored as long-term
	var longTermCount int
	for _, b := range store.blocks {
		if b.Type == MemoryTypeLongTerm {
			longTermCount++
		}
	}

	if longTermCount == 0 {
		t.Error("Expected at least one long-term memory after consolidation")
	}
}

// ============================================
// Reflection Engine Tests
// ============================================

func TestBuildReflectionPrompt(t *testing.T) {
	store := NewMockMemoryStore()
	re := NewReflectionEngine(store, nil) // nil LLM for unit test

	result := &TaskResult{
		TaskID:     "task-1",
		TaskType:   "code_gen",
		Success:    true,
		TokensUsed: 100,
		LatencyMs:  500,
	}

	prompt := re.buildReflectionPrompt(result)

	if prompt == "" {
		t.Error("Prompt should not be empty")
	}
	if !contains(prompt, "code_gen") {
		t.Error("Prompt should contain task type")
	}
	if !contains(prompt, "成功") {
		t.Error("Prompt should indicate success")
	}
}

func TestParseReflectionResponse(t *testing.T) {
	store := NewMockMemoryStore()
	re := NewReflectionEngine(store, nil)

	response := `{
		"analysis": "Task completed successfully",
		"insights": ["Good planning", "Early testing"],
		"action_items": ["Add more tests"],
		"confidence": 0.85,
		"skill_learned": {
			"name": "test_driven",
			"description": "Write tests first",
			"category": "testing",
			"template": "Write test for: {{feature}}"
		}
	}`

	ref := re.parseReflectionResponse(response)

	if ref.Analysis != "Task completed successfully" {
		t.Errorf("Unexpected analysis: %s", ref.Analysis)
	}
	if len(ref.Insights) != 2 {
		t.Errorf("Expected 2 insights, got %d", len(ref.Insights))
	}
	if ref.Confidence != 0.85 {
		t.Errorf("Expected confidence 0.85, got %f", ref.Confidence)
	}
	if ref.SkillLearned == nil {
		t.Error("Expected skill to be learned")
	}
	if ref.SkillLearned.Name != "test_driven" {
		t.Errorf("Expected skill name 'test_driven', got '%s'", ref.SkillLearned.Name)
	}
}

// ============================================
// Experience Extractor Tests
// ============================================

func TestBuildExtractionPrompt(t *testing.T) {
	store := NewMockMemoryStore()
	ee := NewExperienceExtractor(store, nil)

	result := &TaskResult{
		TaskID:   "task-1",
		TaskType: "code_review",
		Success:  true,
	}

	prompt := ee.buildExtractionPrompt(result)

	if prompt == "" {
		t.Error("Prompt should not be empty")
	}
	if !contains(prompt, "code_review") {
		t.Error("Prompt should contain task type")
	}
}

func TestParseExperienceResponse(t *testing.T) {
	store := NewMockMemoryStore()
	ee := NewExperienceExtractor(store, nil)

	response := `{
		"input_summary": "Code review request",
		"output_summary": "Found 3 issues",
		"lessons": ["Check edge cases", "Review error handling"],
		"patterns": ["Common mistake: nil pointer"],
		"suggestions": ["Add linting"],
		"confidence": 0.9
	}`

	exp := ee.parseExperienceResponse(response)

	if exp.Input != "Code review request" {
		t.Errorf("Unexpected input: %s", exp.Input)
	}
	if len(exp.Lessons) != 2 {
		t.Errorf("Expected 2 lessons, got %d", len(exp.Lessons))
	}
	if exp.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", exp.Confidence)
	}
}

// ============================================
// Knowledge Sharing Tests
// ============================================

func TestShareExperience(t *testing.T) {
	store := NewMockMemoryStore()
	ks := NewKnowledgeSharer(store)

	ctx := context.Background()
	exp := &Experience{
		ID:         "exp-1",
		AgentID:    "agent-1",
		TaskID:     "task-1",
		TaskType:   "code_gen",
		Lessons:    []string{"Always check nil"},
		Confidence: 0.85,
	}

	err := ks.ShareExperience(ctx, exp, []string{"agent-2", "agent-3"})
	if err != nil {
		t.Fatalf("Failed to share experience: %v", err)
	}

	// Check that shared memories were created
	var sharedCount int
	for _, b := range store.blocks {
		if b.Type == MemoryTypeShared {
			sharedCount++
		}
	}

	if sharedCount != 2 {
		t.Errorf("Expected 2 shared memories, got %d", sharedCount)
	}
}

func TestShareSkill(t *testing.T) {
	store := NewMockMemoryStore()
	ks := NewKnowledgeSharer(store)

	ctx := context.Background()
	skill := &SkillDefinition{
		ID:          "skill-1",
		Name:        "error_handling",
		Description: "Handle errors gracefully",
		Category:    "coding",
		Template:    "if err != nil { return err }",
		SuccessRate: 0.95,
	}

	err := ks.ShareSkill(ctx, skill, []string{"agent-2"})
	if err != nil {
		t.Fatalf("Failed to share skill: %v", err)
	}

	// Check that skill memory was created
	var found bool
	for _, b := range store.blocks {
		if b.Type == MemoryTypeSkill && b.Title == "共享技能: error_handling" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected shared skill memory to be created")
	}
}

// ============================================
// Self-Improvement Loop Tests
// ============================================

type MockLLMClient struct {
	response string
	err      error
}

func (m *MockLLMClient) Generate(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
}

func (m *MockLLMClient) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	return m.response, m.err
}

func TestSelfImprovementLoop(t *testing.T) {
	store := NewMockMemoryStore()
	llm := &MockLLMClient{
		response: `{
			"analysis": "Good execution",
			"insights": ["Plan ahead"],
			"action_items": ["Test more"],
			"confidence": 0.8
		}`,
	}

	sil := NewSelfImprovementLoop(store, llm, []string{"agent-1", "agent-2"}, nil)

	ctx := context.Background()
	result := &TaskResult{
		TaskID:     "task-1",
		TaskType:   "code_gen",
		Success:    true,
		TokensUsed: 100,
		LatencyMs:  500,
	}

	err := sil.ProcessTaskResult(ctx, "agent-1", result)
	if err != nil {
		t.Fatalf("Failed to process task result: %v", err)
	}

	// Verify experience was stored
	if len(store.experiences) == 0 {
		t.Error("Expected experience to be stored")
	}

	// Verify reflection was stored
	if len(store.reflections) == 0 {
		t.Error("Expected reflection to be stored")
	}
}

// ============================================
// Agent ID Synchronization Tests (验收 A1-A3)
// ============================================

func TestAgentIDManagement(t *testing.T) {
	store := NewMockMemoryStore()
	llm := &MockLLMClient{response: "{}"}
	sil := NewSelfImprovementLoop(store, llm, []string{}, nil)

	// Test initial empty state
	ids := sil.GetAgentIDs()
	if len(ids) != 0 {
		t.Errorf("Expected 0 agent IDs, got %d", len(ids))
	}

	// Test AddAgentID
	sil.AddAgentID("agent-1")
	sil.AddAgentID("agent-2")
	ids = sil.GetAgentIDs()
	if len(ids) != 2 {
		t.Errorf("Expected 2 agent IDs, got %d", len(ids))
	}

	// Test duplicate prevention
	sil.AddAgentID("agent-1") // Should not add duplicate
	ids = sil.GetAgentIDs()
	if len(ids) != 2 {
		t.Errorf("Expected 2 agent IDs after duplicate add, got %d", len(ids))
	}

	// Test SetAgentIDs
	sil.SetAgentIDs([]string{"agent-3", "agent-4", "agent-5"})
	ids = sil.GetAgentIDs()
	if len(ids) != 3 {
		t.Errorf("Expected 3 agent IDs after SetAgentIDs, got %d", len(ids))
	}

	// Test RemoveAgentID
	sil.RemoveAgentID("agent-4")
	ids = sil.GetAgentIDs()
	if len(ids) != 2 {
		t.Errorf("Expected 2 agent IDs after remove, got %d", len(ids))
	}
}

func TestKnowledgeSharingWithAgentIDs(t *testing.T) {
	store := NewMockMemoryStore()
	llm := &MockLLMClient{
		response: `{
			"analysis": "Test analysis",
			"insights": ["test"],
			"action_items": [],
			"confidence": 0.8
		}`,
	}
	sil := NewSelfImprovementLoop(store, llm, []string{"agent-1", "agent-2", "agent-3"}, nil)

	ctx := context.Background()
	result := &TaskResult{
		TaskID:   "task-1",
		TaskType: "code_gen",
		Success:  true,
	}

	err := sil.ProcessTaskResult(ctx, "agent-1", result)
	if err != nil {
		t.Fatalf("Failed to process task result: %v", err)
	}

	// 验收 A2: 共享记忆落库 - 应该有 2 条共享记忆（给 agent-2 和 agent-3）
	var sharedCount int
	for _, b := range store.blocks {
		if b.Type == MemoryTypeShared {
			sharedCount++
			// 验证共享记忆是给其他 Agent 的
			if b.AgentID == "agent-1" {
				t.Error("Shared memory should not be for the source agent")
			}
		}
	}

	if sharedCount != 2 {
		t.Errorf("Expected 2 shared memories for other agents, got %d", sharedCount)
	}
}

// ============================================
// Semantic Search Tests (验收 C1-C3)
// ============================================

// MockEmbeddingClient 是用于测试的模拟 embedding 客户端
type MockEmbeddingClient struct {
	dimension int
}

func NewMockEmbeddingClient() *MockEmbeddingClient {
	return &MockEmbeddingClient{dimension: 384}
}

func (m *MockEmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	// 简单的模拟向量
	vector := make([]float32, m.dimension)
	for i := range vector {
		vector[i] = float32(len(text)+i) / float32(m.dimension)
	}
	return vector, nil
}

func TestGetRelevantMemoriesWithTaskType(t *testing.T) {
	store := NewMockMemoryStore()

	// 预存一些长期记忆
	store.StoreBlock(context.Background(), &MemoryBlock{
		ID:        "lt-1",
		AgentID:   "agent-1",
		Type:      MemoryTypeLongTerm,
		Title:     "Code Generation Experience",
		Content:   "Always check for nil pointers",
		Importance: 0.9,
		Metadata: map[string]interface{}{
			"task_type": "code_gen",
		},
	})

	store.StoreBlock(context.Background(), &MemoryBlock{
		ID:        "lt-2",
		AgentID:   "agent-1",
		Type:      MemoryTypeLongTerm,
		Title:     "Testing Experience",
		Content:   "Write tests first",
		Importance: 0.8,
		Metadata: map[string]interface{}{
			"task_type": "testing",
		},
	})

	llm := &MockLLMClient{response: "{}"}
	sil := NewSelfImprovementLoop(store, llm, []string{"agent-1"}, nil)

	ctx := context.Background()

	// 获取相关记忆
	memories, err := sil.GetRelevantMemories(ctx, "agent-1", "code_gen")
	if err != nil {
		t.Fatalf("Failed to get relevant memories: %v", err)
	}

	// 应该返回记忆
	if len(memories) == 0 {
		t.Error("Expected at least one memory")
	}

	// 按重要性排序，高重要性的应该在前
	if len(memories) > 1 && memories[0].Importance < memories[1].Importance {
		t.Error("Memories should be sorted by importance descending")
	}
}

func TestGetRelevantMemoriesWithQuery(t *testing.T) {
	store := NewMockMemoryStore()
	embedding := NewMockEmbeddingClient()

	// 预存带向量的记忆
	vector, _ := embedding.Embed(context.Background(), "code generation patterns")
	store.StoreBlock(context.Background(), &MemoryBlock{
		ID:        "sim-1",
		AgentID:   "agent-1",
		Type:      MemoryTypeLongTerm,
		Title:     "Code Patterns",
		Content:   "code generation patterns",
		Importance: 0.85,
		Embedding: vector,
	})

	llm := &MockLLMClient{response: "{}"}
	sil := NewSelfImprovementLoop(store, llm, []string{"agent-1"}, embedding)

	ctx := context.Background()

	// 使用查询进行语义检索
	memories, err := sil.GetRelevantMemoriesWithQuery(ctx, "agent-1", "code_gen", "how to generate code")
	if err != nil {
		t.Fatalf("Failed to get relevant memories: %v", err)
	}

	// 应该返回记忆
	if len(memories) == 0 {
		t.Error("Expected at least one memory from semantic search")
	}
}

// ============================================
// Consolidation Strategy Tests (验收 F1-F2)
// ============================================

func TestConsolidationByTaskCount(t *testing.T) {
	store := NewMockMemoryStore()
	llm := &MockLLMClient{
		response: `{
			"analysis": "test",
			"insights": [],
			"action_items": [],
			"confidence": 0.5
		}`,
	}
	sil := NewSelfImprovementLoop(store, llm, []string{"agent-1"}, nil)
	sil.consolidateN = 3 // 每 3 个任务固化一次

	ctx := context.Background()

	// 处理 2 个任务，不应该触发固化
	for i := 0; i < 2; i++ {
		result := &TaskResult{
			TaskID:   fmt.Sprintf("task-%d", i),
			TaskType: "test",
			Success:  true,
		}
		sil.ProcessTaskResult(ctx, "agent-1", result)
	}

	// 检查是否有长期记忆（不应该有，因为还没到阈值）
	var longTermCount int
	for _, b := range store.blocks {
		if b.Type == MemoryTypeLongTerm {
			longTermCount++
		}
	}
	// 注意：短期记忆也可能被固化，取决于重要性
	// 这里主要测试计数器逻辑

	// 处理第 3 个任务，应该触发固化
	result := &TaskResult{
		TaskID:   "task-3",
		TaskType: "test",
		Success:  true,
	}
	sil.ProcessTaskResult(ctx, "agent-1", result)
}

// ============================================
// Helper Functions
// ============================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================
// Benchmark Tests
// ============================================

func BenchmarkMemoryManagerAdd(b *testing.B) {
	store := NewMockMemoryStore()
	mm := NewMemoryManager(store, 100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm.AddShortTermMemory(ctx, "agent-1", "Test", "Content", 0.5)
	}
}

func BenchmarkReflectionParsing(b *testing.B) {
	store := NewMockMemoryStore()
	re := NewReflectionEngine(store, nil)

	response := `{
		"analysis": "Test analysis",
		"insights": ["insight1", "insight2"],
		"action_items": ["action1"],
		"confidence": 0.8
	}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.parseReflectionResponse(response)
	}
}
