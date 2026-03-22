package problem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Problem 题目结构
type Problem struct {
	ID              string            `yaml:"id"`
	Title           string            `yaml:"title"`
	Difficulty      string            `yaml:"difficulty"` // easy, medium, hard
	Tags            []string          `yaml:"tags"`
	Description     string            `yaml:"description"`
	Examples        []Example         `yaml:"examples"`
	Constraints     []string          `yaml:"constraints"`
	Keywords        []string          `yaml:"keywords"`
	RelatedProblems []string          `yaml:"related_problems"`
	SolutionPatterns []SolutionPattern `yaml:"solution_patterns"`
	MCPTools        []MCPToolMeta     `yaml:"mcp_tools"`
}

// Example 示例
type Example struct {
	Input       string `yaml:"input"`
	Output      string `yaml:"output"`
	Explanation string `yaml:"explanation"`
}

// SolutionPattern 解题模式
type SolutionPattern struct {
	Pattern      string `yaml:"pattern"`
	Hint         string `yaml:"hint"`
	Complexity   string `yaml:"complexity"`
	CodeExample  string `yaml:"code_example"`
}

// MCPToolMeta MCP 工具元数据
type MCPToolMeta struct {
	Name string   `yaml:"name"`
	Args []string `yaml:"args"`
}

// ParseYAML 解析 YAML 文件
func ParseYAML(path string) (*Problem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var problem Problem
	if err := yaml.Unmarshal(data, &problem); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}

	if err := problem.Validate(); err != nil {
		return nil, fmt.Errorf("验证失败: %w", err)
	}

	return &problem, nil
}

// Validate 验证题目格式
func (p *Problem) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("题目 ID 不能为空")
	}
	if p.Title == "" {
		return fmt.Errorf("题目标题不能为空")
	}
	if p.Description == "" {
		return fmt.Errorf("题目描述不能为空")
	}
	if p.Difficulty != "easy" && p.Difficulty != "medium" && p.Difficulty != "hard" {
		return fmt.Errorf("难度必须是 easy/medium/hard")
	}
	return nil
}

// ToSearchableText 生成用于向量检索的文本
func (p *Problem) ToSearchableText() string {
	parts := []string{
		p.Title,
		p.Description,
		strings.Join(p.Keywords, " "),
		strings.Join(p.Tags, " "),
	}
	return strings.Join(parts, " ")
}

// ToMetadata 生成向量库元数据
func (p *Problem) ToMetadata() map[string]interface{} {
	return map[string]interface{}{
		"id":               p.ID,
		"title":            p.Title,
		"difficulty":       p.Difficulty,
		"tags":             p.Tags,
		"keywords":         p.Keywords,
		"related_problems": p.RelatedProblems,
	}
}

// GetHint 根据级别获取提示
func (p *Problem) GetHint(level int) string {
	if len(p.SolutionPatterns) == 0 {
		return "暂无提示"
	}

	// level 1: 最简单的提示
	// level 2: 中等提示
	// level 3: 详细提示（包含代码示例）
	if level < 1 {
		level = 1
	}
	if level > 3 {
		level = 3
	}

	pattern := p.SolutionPatterns[0]
	switch level {
	case 1:
		return fmt.Sprintf("解题思路: %s", pattern.Pattern)
	case 2:
		return fmt.Sprintf("解题思路: %s\n\n提示: %s\n\n复杂度: %s",
			pattern.Pattern, pattern.Hint, pattern.Complexity)
	case 3:
		return fmt.Sprintf("解题思路: %s\n\n提示: %s\n\n复杂度: %s\n\n代码示例:\n%s",
			pattern.Pattern, pattern.Hint, pattern.Complexity, pattern.CodeExample)
	default:
		return pattern.Hint
	}
}

// LoadAllProblems 加载所有题目
func LoadAllProblems(dir string) ([]*Problem, error) {
	var problems []*Problem

	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("查找文件失败: %w", err)
	}

	for _, file := range files {
		problem, err := ParseYAML(file)
		if err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", file, err)
		}
		problems = append(problems, problem)
	}

	return problems, nil
}

// ProblemStore 题目存储
type ProblemStore struct {
	problems map[string]*Problem
	byTag    map[string][]string
}

// NewProblemStore 创建题目存储
func NewProblemStore() *ProblemStore {
	return &ProblemStore{
		problems: make(map[string]*Problem),
		byTag:    make(map[string][]string),
	}
}

// Add 添加题目
func (s *ProblemStore) Add(p *Problem) {
	s.problems[p.ID] = p
	for _, tag := range p.Tags {
		s.byTag[tag] = append(s.byTag[tag], p.ID)
	}
}

// Get 获取题目
func (s *ProblemStore) Get(id string) *Problem {
	return s.problems[id]
}

// GetByTag 按标签获取题目
func (s *ProblemStore) GetByTag(tag string) []*Problem {
	ids := s.byTag[tag]
	var result []*Problem
	for _, id := range ids {
		if p := s.problems[id]; p != nil {
			result = append(result, p)
		}
	}
	return result
}

// GetAll 获取所有题目
func (s *ProblemStore) GetAll() []*Problem {
	var result []*Problem
	for _, p := range s.problems {
		result = append(result, p)
	}
	return result
}

// LoadFromDir 从目录加载所有题目
func (s *ProblemStore) LoadFromDir(dir string) error {
	problems, err := LoadAllProblems(dir)
	if err != nil {
		return err
	}
	for _, p := range problems {
		s.Add(p)
	}
	return nil
}
