package memory

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

// ============================================
// PostgreSQL Memory Store - 记忆持久化存储
// ============================================

// PostgresMemoryStore PostgreSQL 记忆存储实现
type PostgresMemoryStore struct {
	pool *pgxpool.Pool
}

// NewPostgresMemoryStore 创建 PostgreSQL 记忆存储
func NewPostgresMemoryStore(pool *pgxpool.Pool) *PostgresMemoryStore {
	return &PostgresMemoryStore{pool: pool}
}

// StoreBlock 存储记忆块
func (s *PostgresMemoryStore) StoreBlock(ctx context.Context, block *MemoryBlock) error {
	metadataJSON, _ := json.Marshal(block.Metadata)

	var embedding *pgvector.Vector
	if len(block.Embedding) > 0 {
		v := pgvector.NewVector(block.Embedding)
		embedding = &v
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_memory (id, agent_id, type, title, content, importance, access_count, metadata, embedding, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			importance = EXCLUDED.importance,
			access_count = EXCLUDED.access_count,
			last_access = NOW(),
			metadata = EXCLUDED.metadata
	`, block.ID, block.AgentID, block.Type, block.Title, block.Content,
		block.Importance, block.AccessCount, metadataJSON, embedding, block.ExpiresAt)

	return err
}

// GetBlock 获取记忆块
func (s *PostgresMemoryStore) GetBlock(ctx context.Context, id string) (*MemoryBlock, error) {
	var block MemoryBlock
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, type, title, content, importance, access_count, metadata, created_at, last_access, expires_at
		FROM agent_memory WHERE id = $1
	`, id).Scan(&block.ID, &block.AgentID, &block.Type, &block.Title, &block.Content,
		&block.Importance, &block.AccessCount, &metadataJSON, &block.CreatedAt, &block.LastAccess, &block.ExpiresAt)

	if err != nil {
		return nil, err
	}

	json.Unmarshal(metadataJSON, &block.Metadata)
	if block.Metadata == nil {
		block.Metadata = make(map[string]interface{})
	}

	return &block, nil
}

// QueryBlocks 查询记忆块
func (s *PostgresMemoryStore) QueryBlocks(ctx context.Context, agentID string, memType MemoryType, limit int) ([]*MemoryBlock, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, type, title, content, importance, access_count, metadata, created_at, last_access
		FROM agent_memory
		WHERE agent_id = $1 AND type = $2 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY importance DESC, created_at DESC
		LIMIT $3
	`, agentID, memType, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []*MemoryBlock
	for rows.Next() {
		var block MemoryBlock
		var metadataJSON []byte
		if err := rows.Scan(&block.ID, &block.AgentID, &block.Type, &block.Title, &block.Content,
			&block.Importance, &block.AccessCount, &metadataJSON, &block.CreatedAt, &block.LastAccess); err != nil {
			return nil, err
		}
		json.Unmarshal(metadataJSON, &block.Metadata)
		if block.Metadata == nil {
			block.Metadata = make(map[string]interface{})
		}
		blocks = append(blocks, &block)
	}

	return blocks, rows.Err()
}

// SearchSimilar 相似度搜索
func (s *PostgresMemoryStore) SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]*MemoryBlock, error) {
	if limit <= 0 {
		limit = 5
	}

	qv := pgvector.NewVector(embedding)

	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, type, title, content, importance, access_count, metadata, created_at, last_access,
			   1 - (embedding <=> $1::vector) AS similarity
		FROM agent_memory
		WHERE embedding IS NOT NULL AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`, qv, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []*MemoryBlock
	for rows.Next() {
		var block MemoryBlock
		var metadataJSON []byte
		var similarity float64
		if err := rows.Scan(&block.ID, &block.AgentID, &block.Type, &block.Title, &block.Content,
			&block.Importance, &block.AccessCount, &metadataJSON, &block.CreatedAt, &block.LastAccess, &similarity); err != nil {
			return nil, err
		}
		json.Unmarshal(metadataJSON, &block.Metadata)
		if block.Metadata == nil {
			block.Metadata = make(map[string]interface{})
		}
		block.Metadata["_similarity"] = similarity
		blocks = append(blocks, &block)
	}

	return blocks, rows.Err()
}

// StoreExperience 存储经验
func (s *PostgresMemoryStore) StoreExperience(ctx context.Context, exp *Experience) error {
	lessonsJSON, _ := json.Marshal(exp.Lessons)
	patternsJSON, _ := json.Marshal(exp.Patterns)
	suggestionsJSON, _ := json.Marshal(exp.Suggestions)
	metadataJSON, _ := json.Marshal(exp.Metadata)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_experiences (id, agent_id, task_id, task_type, input, output, success,
			lessons, patterns, suggestions, confidence, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, exp.ID, exp.AgentID, exp.TaskID, exp.TaskType, exp.Input, exp.Output, exp.Success,
		lessonsJSON, patternsJSON, suggestionsJSON, exp.Confidence, metadataJSON)

	return err
}

// GetExperiences 获取经验列表
func (s *PostgresMemoryStore) GetExperiences(ctx context.Context, agentID string, limit int) ([]*Experience, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, task_id, task_type, input, output, success, lessons, patterns, suggestions, confidence, created_at
		FROM agent_experiences
		WHERE agent_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, agentID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var experiences []*Experience
	for rows.Next() {
		var exp Experience
		var lessonsJSON, patternsJSON, suggestionsJSON []byte
		if err := rows.Scan(&exp.ID, &exp.AgentID, &exp.TaskID, &exp.TaskType, &exp.Input, &exp.Output,
			&exp.Success, &lessonsJSON, &patternsJSON, &suggestionsJSON, &exp.Confidence, &exp.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(lessonsJSON, &exp.Lessons)
		json.Unmarshal(patternsJSON, &exp.Patterns)
		json.Unmarshal(suggestionsJSON, &exp.Suggestions)
		experiences = append(experiences, &exp)
	}

	return experiences, rows.Err()
}

// StoreReflection 存储反思
func (s *PostgresMemoryStore) StoreReflection(ctx context.Context, ref *Reflection) error {
	insightsJSON, _ := json.Marshal(ref.Insights)
	actionItemsJSON, _ := json.Marshal(ref.ActionItems)
	metadataJSON, _ := json.Marshal(ref.Metadata)

	var skillJSON []byte
	if ref.SkillLearned != nil {
		skillJSON, _ = json.Marshal(ref.SkillLearned)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_reflections (id, agent_id, trigger_task_id, trigger_type, analysis,
			insights, action_items, skill_learned, confidence, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, ref.ID, ref.AgentID, ref.TriggerTaskID, ref.TriggerType, ref.Analysis,
		insightsJSON, actionItemsJSON, skillJSON, ref.Confidence, metadataJSON)

	return err
}

// GetReflections 获取反思列表
func (s *PostgresMemoryStore) GetReflections(ctx context.Context, agentID string, limit int) ([]*Reflection, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, agent_id, trigger_task_id, trigger_type, analysis, insights, action_items, skill_learned, confidence, created_at
		FROM agent_reflections
		WHERE agent_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, agentID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reflections []*Reflection
	for rows.Next() {
		var ref Reflection
		var insightsJSON, actionItemsJSON, skillJSON []byte
		if err := rows.Scan(&ref.ID, &ref.AgentID, &ref.TriggerTaskID, &ref.TriggerType, &ref.Analysis,
			&insightsJSON, &actionItemsJSON, &skillJSON, &ref.Confidence, &ref.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(insightsJSON, &ref.Insights)
		json.Unmarshal(actionItemsJSON, &ref.ActionItems)
		if len(skillJSON) > 0 {
			json.Unmarshal(skillJSON, &ref.SkillLearned)
		}
		reflections = append(reflections, &ref)
	}

	return reflections, rows.Err()
}

// StoreSkill 存储技能
func (s *PostgresMemoryStore) StoreSkill(ctx context.Context, skill *SkillDefinition) error {
	paramsJSON, _ := json.Marshal(skill.Parameters)
	examplesJSON, _ := json.Marshal(skill.Examples)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_skills (id, name, description, category, template, parameters, examples, success_rate, use_count, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			template = EXCLUDED.template,
			parameters = EXCLUDED.parameters,
			success_rate = EXCLUDED.success_rate,
			use_count = agent_skills.use_count + 1,
			updated_at = NOW()
	`, skill.ID, skill.Name, skill.Description, skill.Category, skill.Template,
		paramsJSON, examplesJSON, skill.SuccessRate, skill.UseCount, skill.Source)

	return err
}

// GetSkill 获取技能
func (s *PostgresMemoryStore) GetSkill(ctx context.Context, name string) (*SkillDefinition, error) {
	var skill SkillDefinition
	var paramsJSON, examplesJSON []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, category, template, parameters, examples, success_rate, use_count, source, created_at, updated_at
		FROM agent_skills WHERE name = $1
	`, name).Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Category, &skill.Template,
		&paramsJSON, &examplesJSON, &skill.SuccessRate, &skill.UseCount, &skill.Source,
		&skill.CreatedAt, &skill.UpdatedAt)

	if err != nil {
		return nil, err
	}

	json.Unmarshal(paramsJSON, &skill.Parameters)
	json.Unmarshal(examplesJSON, &skill.Examples)
	if skill.Parameters == nil {
		skill.Parameters = make(map[string]ParamDef)
	}

	return &skill, nil
}

// ListSkills 列出技能
func (s *PostgresMemoryStore) ListSkills(ctx context.Context, category string) ([]*SkillDefinition, error) {
	var rows pgx.Rows
	var err error

	if category != "" {
		rows, err = s.pool.Query(ctx, `
			SELECT id, name, description, category, template, success_rate, use_count, source
			FROM agent_skills WHERE category = $1 ORDER BY use_count DESC
		`, category)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id, name, description, category, template, success_rate, use_count, source
			FROM agent_skills ORDER BY use_count DESC
		`)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []*SkillDefinition
	for rows.Next() {
		var skill SkillDefinition
		if err := rows.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Category,
			&skill.Template, &skill.SuccessRate, &skill.UseCount, &skill.Source); err != nil {
			return nil, err
		}
		skills = append(skills, &skill)
	}

	return skills, rows.Err()
}

// CleanupExpiredMemories 清理过期记忆
func (s *PostgresMemoryStore) CleanupExpiredMemories(ctx context.Context) (int64, error) {
	result, err := s.pool.Exec(ctx, `
		DELETE FROM agent_memory WHERE expires_at IS NOT NULL AND expires_at < NOW()
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// GetMemoryStats 获取记忆统计
func (s *PostgresMemoryStore) GetMemoryStats(ctx context.Context, agentID string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// 各类型记忆数量
	rows, err := s.pool.Query(ctx, `
		SELECT type, COUNT(*) FROM agent_memory WHERE agent_id = $1 GROUP BY type
	`, agentID)
	if err == nil {
		defer rows.Close()
		counts := make(map[string]int)
		for rows.Next() {
			var memType string
			var count int
			if err := rows.Scan(&memType, &count); err == nil {
				counts[memType] = count
			}
		}
		stats["memory_counts"] = counts
	}

	// 经验数量
	var expCount int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_experiences WHERE agent_id = $1`, agentID).Scan(&expCount); err == nil {
		stats["experience_count"] = expCount
	}

	// 反思数量
	var refCount int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_reflections WHERE agent_id = $1`, agentID).Scan(&refCount); err == nil {
		stats["reflection_count"] = refCount
	}

	// 平均置信度
	var avgConfidence float64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(confidence), 0) FROM agent_experiences WHERE agent_id = $1
	`, agentID).Scan(&avgConfidence); err == nil {
		stats["avg_confidence"] = avgConfidence
	}

	return stats, nil
}

// GetTopInsights 获取最重要的洞察
func (s *PostgresMemoryStore) GetTopInsights(ctx context.Context, agentID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx, `
		SELECT jsonb_array_elements_text(insights) as insight, COUNT(*) as cnt
		FROM agent_reflections WHERE agent_id = $1
		GROUP BY insight
		ORDER BY cnt DESC
		LIMIT $2
	`, agentID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var insights []string
	for rows.Next() {
		var insight string
		var count int
		if err := rows.Scan(&insight, &count); err != nil {
			continue
		}
		insights = append(insights, insight)
	}

	return insights, rows.Err()
}

// GetSkillSuccessRate 获取技能成功率
func (s *PostgresMemoryStore) GetSkillSuccessRate(ctx context.Context, skillName string) (float64, error) {
	var successRate float64
	err := s.pool.QueryRow(ctx, `
		SELECT success_rate FROM agent_skills WHERE name = $1
	`, skillName).Scan(&successRate)
	return successRate, err
}

// UpdateSkillUsage 更新技能使用情况
func (s *PostgresMemoryStore) UpdateSkillUsage(ctx context.Context, skillName string, success bool) error {
	var successDelta float64
	if success {
		successDelta = 0.01 // 成功时增加成功率
	} else {
		successDelta = -0.02 // 失败时减少成功率
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE agent_skills
		SET use_count = use_count + 1,
			success_rate = GREATEST(0, LEAST(1, success_rate + $2)),
			updated_at = NOW()
		WHERE name = $1
	`, skillName, successDelta)

	return err
}

// GetSharedKnowledge 获取共享知识
func (s *PostgresMemoryStore) GetSharedKnowledge(ctx context.Context, agentID string, limit int) ([]*MemoryBlock, error) {
	if limit <= 0 {
		limit = 10
	}

	return s.QueryBlocks(ctx, agentID, MemoryTypeShared, limit)
}

// GetRecentLearnedSkills 获取最近学习的技能
func (s *PostgresMemoryStore) GetRecentLearnedSkills(ctx context.Context, limit int) ([]*SkillDefinition, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, category, template, success_rate, use_count, source, created_at
		FROM agent_skills WHERE source = 'learned'
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []*SkillDefinition
	for rows.Next() {
		var skill SkillDefinition
		if err := rows.Scan(&skill.ID, &skill.Name, &skill.Description, &skill.Category,
			&skill.Template, &skill.SuccessRate, &skill.UseCount, &skill.Source, &skill.CreatedAt); err != nil {
			continue
		}
		skills = append(skills, &skill)
	}

	return skills, rows.Err()
}

// PruneOldMemories 清理旧记忆（保留高重要性的）
func (s *PostgresMemoryStore) PruneOldMemories(ctx context.Context, agentID string, maxAge time.Duration, keepCount int) (int64, error) {
	cutoff := time.Now().Add(-maxAge)

	result, err := s.pool.Exec(ctx, `
		DELETE FROM agent_memory
		WHERE agent_id = $1 AND created_at < $2
		AND id NOT IN (
			SELECT id FROM agent_memory
			WHERE agent_id = $1
			ORDER BY importance DESC
			LIMIT $3
		)
	`, agentID, cutoff, keepCount)

	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}
