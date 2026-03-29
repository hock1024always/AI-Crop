package rag

import (
	"context"
	"fmt"
	"log"
	"strings"

	"ai-corp/pkg/problem"
)

// RAGService RAG 服务
type RAGService struct {
	store        VectorStore
	embedClient  EmbeddingClient
	problemStore *problem.ProblemStore
}

// NewRAGService 创建 RAG 服务
func NewRAGService(store VectorStore, embedClient EmbeddingClient) *RAGService {
	return &RAGService{
		store:        store,
		embedClient:  embedClient,
		problemStore: problem.NewProblemStore(),
	}
}

// IngestProblem 将题目入库
func (s *RAGService) IngestProblem(ctx context.Context, p *problem.Problem) error {
	// 1. 生成可搜索文本
	searchableText := p.ToSearchableText()

	// 2. 获取向量
	vector, err := s.embedClient.Embed(ctx, searchableText)
	if err != nil {
		return fmt.Errorf("获取向量失败: %w", err)
	}

	// 3. 存入向量库
	entry := &VectorEntry{
		ID:       p.ID,
		Vector:   vector,
		Metadata: p.ToMetadata(),
	}

	if err := s.store.Insert(ctx, entry); err != nil {
		return fmt.Errorf("存储向量失败: %w", err)
	}

	// 4. 存入题目存储
	s.problemStore.Add(p)

	log.Printf("题目入库成功: %s - %s", p.ID, p.Title)
	return nil
}

// IngestFromDir 从目录批量入库
func (s *RAGService) IngestFromDir(ctx context.Context, dir string) error {
	problems, err := problem.LoadAllProblems(dir)
	if err != nil {
		return fmt.Errorf("加载题目失败: %w", err)
	}

	for _, p := range problems {
		if err := s.IngestProblem(ctx, p); err != nil {
			log.Printf("入库失败 %s: %v", p.ID, err)
			continue
		}
	}

	count, _ := s.store.Count(ctx)
	log.Printf("入库完成，共 %d 道题目", count)
	return nil
}

// SearchSimilarProblems 搜索相似题目
func (s *RAGService) SearchSimilarProblems(ctx context.Context, query string, topK int) ([]*SearchResult, error) {
	// 1. 获取查询向量
	vector, err := s.embedClient.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("获取查询向量失败: %w", err)
	}

	// 2. 搜索相似向量
	results, err := s.store.Search(ctx, vector, topK)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}

	return results, nil
}

// GetProblem 获取题目详情
func (s *RAGService) GetProblem(id string) *problem.Problem {
	return s.problemStore.Get(id)
}

// GetEmbedding 获取文本的向量嵌入
func (s *RAGService) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	if s.embedClient == nil {
		return nil, fmt.Errorf("embedding client not configured")
	}
	return s.embedClient.Embed(ctx, text)
}

// LoadProblems 直接加载题目（不进行向量化）
func (s *RAGService) LoadProblems(problems []*problem.Problem) {
	for _, p := range problems {
		s.problemStore.Add(p)
	}
}

// GetHint 获取题目提示
func (s *RAGService) GetHint(problemID string, level int) (string, error) {
	p := s.problemStore.Get(problemID)
	if p == nil {
		return "", fmt.Errorf("题目不存在: %s", problemID)
	}
	return p.GetHint(level), nil
}

// ExplainPattern 解释解题模式
func (s *RAGService) ExplainPattern(pattern string) string {
	// 预定义的解题模式解释
	patterns := map[string]string{
		"哈希表一次遍历": "使用哈希表存储已遍历元素，实现 O(n) 时间复杂度的查找。适用于需要快速查找、去重、计数的场景。",
		"滑动窗口":    "使用双指针维护一个窗口，根据条件动态调整窗口大小。适用于子串/子数组问题。",
		"双指针":     "使用两个指针从不同方向遍历，减少时间复杂度。适用于有序数组、链表问题。",
		"二分查找":    "在有序数组中折半查找，时间复杂度 O(log n)。适用于有序数据查找。",
		"动态规划":    "将问题分解为子问题，存储子问题的解避免重复计算。适用于最优化问题。",
		"深度优先搜索":  "沿着一条路径深入探索，然后回溯。适用于树、图的遍历。",
		"广度优先搜索":  "逐层遍历，使用队列实现。适用于最短路径问题。",
		"模拟加法":    "按位模拟手工加法过程，处理进位。适用于大数运算。",
	}

	if explanation, ok := patterns[pattern]; ok {
		return explanation
	}
	return fmt.Sprintf("暂无 %s 模式的详细解释", pattern)
}

// SearchByKeywords 按关键词搜索
func (s *RAGService) SearchByKeywords(keywords []string) []*problem.Problem {
	var results []*problem.Problem
	for _, p := range s.problemStore.GetAll() {
		for _, kw := range keywords {
			if strings.Contains(p.Title, kw) || containsStr(p.Keywords, kw) || containsStr(p.Tags, kw) {
				results = append(results, p)
				break
			}
		}
	}
	return results
}

// GetStats 获取统计信息
func (s *RAGService) GetStats() map[string]interface{} {
	problems := s.problemStore.GetAll()

	difficultyCount := make(map[string]int)
	tagCount := make(map[string]int)

	for _, p := range problems {
		difficultyCount[p.Difficulty]++
		for _, tag := range p.Tags {
			tagCount[tag]++
		}
	}

	return map[string]interface{}{
		"total":     len(problems),
		"by_difficulty": difficultyCount,
		"by_tag":    tagCount,
	}
}

func containsStr(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
