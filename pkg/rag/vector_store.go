package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
)

// VectorEntry 向量条目
type VectorEntry struct {
	ID       string                 `json:"id"`
	Vector   []float32              `json:"vector"`
	Metadata map[string]interface{} `json:"metadata"`
}

// SearchResult 搜索结果
type SearchResult struct {
	ID       string                 `json:"id"`
	Score    float64                `json:"score"`
	Metadata map[string]interface{} `json:"metadata"`
}

// VectorStore 向量存储接口
type VectorStore interface {
	Insert(ctx context.Context, entry *VectorEntry) error
	InsertBatch(ctx context.Context, entries []*VectorEntry) error
	Search(ctx context.Context, vector []float32, topK int) ([]*SearchResult, error)
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*VectorEntry, error)
	Count(ctx context.Context) (int, error)
}

// MemoryVectorStore 内存向量存储（用于开发和测试）
type MemoryVectorStore struct {
	mu      sync.RWMutex
	entries map[string]*VectorEntry
	vectors [][]float32
	ids     []string
}

// NewMemoryVectorStore 创建内存向量存储
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{
		entries: make(map[string]*VectorEntry),
		vectors: make([][]float32, 0),
		ids:     make([]string, 0),
	}
}

// Insert 插入向量
func (s *MemoryVectorStore) Insert(ctx context.Context, entry *VectorEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[entry.ID] = entry
	s.vectors = append(s.vectors, entry.Vector)
	s.ids = append(s.ids, entry.ID)

	return nil
}

// InsertBatch 批量插入向量
func (s *MemoryVectorStore) InsertBatch(ctx context.Context, entries []*VectorEntry) error {
	for _, entry := range entries {
		if err := s.Insert(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

// Search 搜索相似向量
func (s *MemoryVectorStore) Search(ctx context.Context, query []float32, topK int) ([]*SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.vectors) == 0 {
		return []*SearchResult{}, nil
	}

	// 计算所有向量的相似度
	type scoredEntry struct {
		id    string
		score float64
	}

	scores := make([]scoredEntry, len(s.vectors))
	for i, vec := range s.vectors {
		score := cosineSimilarity(query, vec)
		scores[i] = scoredEntry{id: s.ids[i], score: score}
	}

	// 按相似度排序
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// 返回 topK 结果
	if topK > len(scores) {
		topK = len(scores)
	}

	results := make([]*SearchResult, topK)
	for i := 0; i < topK; i++ {
		entry := s.entries[scores[i].id]
		results[i] = &SearchResult{
			ID:       scores[i].id,
			Score:    scores[i].score,
			Metadata: entry.Metadata,
		}
	}

	return results, nil
}

// Delete 删除向量
func (s *MemoryVectorStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entries, id)

	// 重建索引
	s.vectors = make([][]float32, 0)
	s.ids = make([]string, 0)
	for id, entry := range s.entries {
		s.vectors = append(s.vectors, entry.Vector)
		s.ids = append(s.ids, id)
	}

	return nil
}

// Get 获取向量
func (s *MemoryVectorStore) Get(ctx context.Context, id string) (*VectorEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[id]
	if !ok {
		return nil, fmt.Errorf("向量不存在: %s", id)
	}
	return entry, nil
}

// Count 获取向量数量
func (s *MemoryVectorStore) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries), nil
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Export 导出为 JSON
func (s *MemoryVectorStore) Export() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]*VectorEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}

	return json.MarshalIndent(entries, "", "  ")
}

// Import 从 JSON 导入
func (s *MemoryVectorStore) Import(data []byte) error {
	var entries []*VectorEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	for _, entry := range entries {
		s.entries[entry.ID] = entry
		s.vectors = append(s.vectors, entry.Vector)
		s.ids = append(s.ids, entry.ID)
	}

	return nil
}
