package rag

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-corp/pkg/database"
)

// PgVectorStore 基于 PostgreSQL + pgvector 的 VectorStore 实现
// 实现 VectorStore 接口，对接 KnowledgeBaseRepo
type PgVectorStore struct {
	kb *database.KnowledgeBaseRepo
}

// NewPgVectorStore 创建 PostgreSQL VectorStore
func NewPgVectorStore(kb *database.KnowledgeBaseRepo) *PgVectorStore {
	return &PgVectorStore{kb: kb}
}

// Insert 插入向量条目
func (s *PgVectorStore) Insert(ctx context.Context, entry *VectorEntry) error {
	// Metadata 转 map
	meta := entry.Metadata
	if meta == nil {
		meta = make(map[string]interface{})
	}

	// 从 metadata 提取标准字段，其余存 JSONB
	title := getStr(meta, "title", entry.ID)
	contentType := getStr(meta, "content_type", "text")
	source := getStr(meta, "source", "")
	language := getStr(meta, "language", "zh")
	content := getStr(meta, "content", "")
	if content == "" {
		// 序列化整个 metadata 作为 content 兜底
		b, _ := json.Marshal(meta)
		content = string(b)
	}

	kb := &database.KnowledgeEntry{
		Title:       title,
		Content:     content,
		ContentType: contentType,
		Source:      source,
		Language:    language,
		Embedding:   entry.Vector,
		Metadata:    meta,
		ChunkIndex:  0,
		ChunkTotal:  1,
	}

	id, err := s.kb.Insert(ctx, kb)
	if err != nil {
		return fmt.Errorf("PgVectorStore.Insert: %w", err)
	}

	// 回写 DB 生成的 ID 到 entry（方便上层追踪）
	entry.Metadata["_pg_id"] = id
	return nil
}

// InsertBatch 批量插入
func (s *PgVectorStore) InsertBatch(ctx context.Context, entries []*VectorEntry) error {
	for _, e := range entries {
		if err := s.Insert(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// Search 向量相似度搜索
func (s *PgVectorStore) Search(ctx context.Context, vector []float32, topK int) ([]*SearchResult, error) {
	results, err := s.kb.SearchSimilar(ctx, vector, topK, "")
	if err != nil {
		return nil, fmt.Errorf("PgVectorStore.Search: %w", err)
	}

	out := make([]*SearchResult, 0, len(results))
	for _, r := range results {
		sim := 0.0
		if v, ok := r.Metadata["_similarity"]; ok {
			if f, ok := v.(float64); ok {
				sim = f
			}
		}
		out = append(out, &SearchResult{
			ID:       r.ID,
			Score:    sim,
			Metadata: r.Metadata,
		})
	}
	return out, nil
}

// Delete 删除条目（按 pgvector ID 或 source 匹配）
func (s *PgVectorStore) Delete(ctx context.Context, id string) error {
	return s.kb.Delete(ctx, id)
}

// Get 按 ID 获取条目
func (s *PgVectorStore) Get(ctx context.Context, id string) (*VectorEntry, error) {
	entry, err := s.kb.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("PgVectorStore.Get: %w", err)
	}
	return &VectorEntry{
		ID:       entry.ID,
		Vector:   entry.Embedding,
		Metadata: entry.Metadata,
	}, nil
}

// Count 返回知识库条目总数
func (s *PgVectorStore) Count(ctx context.Context) (int, error) {
	return s.kb.Count(ctx)
}

// --- helpers ---

func getStr(m map[string]interface{}, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}
