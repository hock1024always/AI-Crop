package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"ai-corp/pkg/rag"
)

// RAGToolRegistry RAG 工具注册表
type RAGToolRegistry struct {
	ragService *rag.RAGService
}

// NewRAGToolRegistry 创建 RAG 工具注册表
func NewRAGToolRegistry(ragService *rag.RAGService) *RAGToolRegistry {
	return &RAGToolRegistry{
		ragService: ragService,
	}
}

// GetTools 获取所有 RAG 工具
func (r *RAGToolRegistry) GetTools() []Tool {
	return []Tool{
		{
			Name:        "search_similar_problems",
			Description: "搜索相似题目，用于找到与当前问题类似的已解决问题。输入题目描述或关键词，返回最相似的问题列表。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "题目描述或关键词，如：数组两数之和、链表反转、二叉树遍历等",
					},
					"top_k": map[string]interface{}{
						"type":        "integer",
						"description": "返回数量，默认5",
						"default":     5,
					},
				},
				"required": []string{"query"},
			},
			Handler: r.handleSearchSimilarProblems,
		},
		{
			Name:        "get_hint",
			Description: "获取解题提示，根据难度级别返回不同程度的提示。级别1最简单，级别3最详细（包含代码示例）。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"problem_id": map[string]interface{}{
						"type":        "string",
						"description": "题目ID，如：001、002、003等",
					},
					"level": map[string]interface{}{
						"type":        "integer",
						"description": "提示级别 1-3，1最简单，3最详细",
						"minimum":     1,
						"maximum":     3,
					},
				},
				"required": []string{"problem_id", "level"},
			},
			Handler: r.handleGetHint,
		},
		{
			Name:        "explain_pattern",
			Description: "解释解题模式的原理和应用场景。支持：哈希表一次遍历、滑动窗口、双指针、二分查找、动态规划等。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "解题模式名称，如：哈希表一次遍历、滑动窗口、双指针等",
					},
				},
				"required": []string{"pattern"},
			},
			Handler: r.handleExplainPattern,
		},
		{
			Name:        "get_problem_detail",
			Description: "获取题目详细信息，包括描述、示例、约束条件等。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"problem_id": map[string]interface{}{
						"type":        "string",
						"description": "题目ID",
					},
				},
				"required": []string{"problem_id"},
			},
			Handler: r.handleGetProblemDetail,
		},
		{
			Name:        "list_problems",
			Description: "列出所有题目或按条件筛选题目。",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"difficulty": map[string]interface{}{
						"type":        "string",
						"description": "难度筛选：easy、medium、hard",
						"enum":        []string{"easy", "medium", "hard"},
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "标签筛选，如：array、linked-list、tree等",
					},
				},
			},
			Handler: r.handleListProblems,
		},
		{
			Name:        "get_stats",
			Description: "获取题库统计信息，包括题目总数、各难度数量、标签分布等。",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Handler: r.handleGetStats,
		},
	}
}

// 处理函数

func (r *RAGToolRegistry) handleSearchSimilarProblems(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("query 参数必须是字符串")
	}

	topK := 5
	if v, ok := args["top_k"].(float64); ok {
		topK = int(v)
	}

	results, err := r.ragService.SearchSimilarProblems(ctx, query, topK)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "未找到相似题目", nil
	}

	var output string
	output = fmt.Sprintf("找到 %d 道相似题目：\n\n", len(results))
	for i, result := range results {
		title := "未知题目"
		if t, ok := result.Metadata["title"].(string); ok {
			title = t
		}
		difficulty := "unknown"
		if d, ok := result.Metadata["difficulty"].(string); ok {
			difficulty = d
		}

		output += fmt.Sprintf("%d. [%s] %s (ID: %s, 相似度: %.2f)\n",
			i+1, difficulty, title, result.ID, result.Score)
	}

	return output, nil
}

func (r *RAGToolRegistry) handleGetHint(ctx context.Context, args map[string]interface{}) (string, error) {
	problemID, ok := args["problem_id"].(string)
	if !ok {
		return "", fmt.Errorf("problem_id 参数必须是字符串")
	}

	level := 1
	if v, ok := args["level"].(float64); ok {
		level = int(v)
	}

	hint, err := r.ragService.GetHint(problemID, level)
	if err != nil {
		return "", err
	}

	return hint, nil
}

func (r *RAGToolRegistry) handleExplainPattern(ctx context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern 参数必须是字符串")
	}

	explanation := r.ragService.ExplainPattern(pattern)
	return explanation, nil
}

func (r *RAGToolRegistry) handleGetProblemDetail(ctx context.Context, args map[string]interface{}) (string, error) {
	problemID, ok := args["problem_id"].(string)
	if !ok {
		return "", fmt.Errorf("problem_id 参数必须是字符串")
	}

	problem := r.ragService.GetProblem(problemID)
	if problem == nil {
		return "", fmt.Errorf("题目不存在: %s", problemID)
	}

	output := fmt.Sprintf("题目 %s: %s\n", problem.ID, problem.Title)
	output += fmt.Sprintf("难度: %s\n", problem.Difficulty)
	output += fmt.Sprintf("标签: %v\n\n", problem.Tags)
	output += fmt.Sprintf("描述:\n%s\n\n", problem.Description)

	if len(problem.Examples) > 0 {
		output += "示例:\n"
		for i, ex := range problem.Examples {
			output += fmt.Sprintf("  示例 %d:\n", i+1)
			output += fmt.Sprintf("    输入: %s\n", ex.Input)
			output += fmt.Sprintf("    输出: %s\n", ex.Output)
			if ex.Explanation != "" {
				output += fmt.Sprintf("    解释: %s\n", ex.Explanation)
			}
		}
	}

	if len(problem.Constraints) > 0 {
		output += "\n约束条件:\n"
		for _, c := range problem.Constraints {
			output += fmt.Sprintf("  - %s\n", c)
		}
	}

	return output, nil
}

func (r *RAGToolRegistry) handleListProblems(ctx context.Context, args map[string]interface{}) (string, error) {
	stats := r.ragService.GetStats()

	output := "题库统计信息:\n\n"

	if total, ok := stats["total"].(int); ok {
		output += fmt.Sprintf("题目总数: %d\n\n", total)
	}

	if byDifficulty, ok := stats["by_difficulty"].(map[string]int); ok {
		output += "按难度分布:\n"
		output += fmt.Sprintf("  简单: %d\n", byDifficulty["easy"])
		output += fmt.Sprintf("  中等: %d\n", byDifficulty["medium"])
		output += fmt.Sprintf("  困难: %d\n", byDifficulty["hard"])
	}

	if byTag, ok := stats["by_tag"].(map[string]int); ok {
		output += "\n按标签分布:\n"
		for tag, count := range byTag {
			output += fmt.Sprintf("  %s: %d\n", tag, count)
		}
	}

	return output, nil
}

func (r *RAGToolRegistry) handleGetStats(ctx context.Context, args map[string]interface{}) (string, error) {
	stats := r.ragService.GetStats()
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
