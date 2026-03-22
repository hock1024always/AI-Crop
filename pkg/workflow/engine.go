// Package workflow 提供 DAG 工作流引擎
// 支持：步骤定义、条件分支、并行执行、重试、超时、回调
package workflow

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StepStatus 步骤状态
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// StepFunc 步骤执行函数
type StepFunc func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)

// Step 工作流步骤
type Step struct {
	ID           string
	Name         string
	Description  string
	AgentType    string            // 由哪类 Agent 执行
	Func         StepFunc
	DependsOn    []string          // 前置步骤 ID
	Condition    func(ctx map[string]interface{}) bool // nil = 无条件执行
	RetryLimit   int
	Timeout      time.Duration
	// 运行时状态
	Status       StepStatus
	Output       map[string]interface{}
	Error        error
	StartedAt    time.Time
	CompletedAt  time.Time
	Attempts     int
}

// Workflow DAG 工作流
type Workflow struct {
	ID          string
	Name        string
	Description string
	Steps       map[string]*Step
	Status      StepStatus
	Context     map[string]interface{} // 全局共享上下文
	CreatedAt   time.Time
	mu          sync.Mutex
}

// NewWorkflow 创建新工作流
func NewWorkflow(id, name string) *Workflow {
	return &Workflow{
		ID:        id,
		Name:      name,
		Steps:     make(map[string]*Step),
		Context:   make(map[string]interface{}),
		Status:    StepPending,
		CreatedAt: time.Now(),
	}
}

// AddStep 添加步骤
func (w *Workflow) AddStep(step *Step) *Workflow {
	if step.RetryLimit == 0 {
		step.RetryLimit = 1
	}
	if step.Timeout == 0 {
		step.Timeout = 60 * time.Second
	}
	step.Status = StepPending
	w.Steps[step.ID] = step
	return w
}

// Run 执行工作流
func (w *Workflow) Run(ctx context.Context) error {
	w.mu.Lock()
	w.Status = StepRunning
	w.mu.Unlock()

	// 拓扑排序
	order, err := w.topologicalSort()
	if err != nil {
		w.Status = StepFailed
		return err
	}

	// 按层次并行执行
	for _, layer := range order {
		var wg sync.WaitGroup
		errCh := make(chan error, len(layer))

		for _, stepID := range layer {
			step := w.Steps[stepID]
			wg.Add(1)
			go func(s *Step) {
				defer wg.Done()
				if err := w.runStep(ctx, s); err != nil {
					errCh <- fmt.Errorf("step %s: %w", s.ID, err)
				}
			}(step)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			if err != nil {
				w.Status = StepFailed
				return err
			}
		}
	}

	w.Status = StepCompleted
	return nil
}

// runStep 执行单个步骤（含重试）
func (w *Workflow) runStep(ctx context.Context, step *Step) error {
	// 条件检查
	if step.Condition != nil {
		w.mu.Lock()
		pass := step.Condition(w.Context)
		w.mu.Unlock()
		if !pass {
			step.Status = StepSkipped
			return nil
		}
	}

	step.Status = StepRunning
	step.StartedAt = time.Now()

	// 收集前置输出作为输入
	input := make(map[string]interface{})
	w.mu.Lock()
	for k, v := range w.Context {
		input[k] = v
	}
	w.mu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= step.RetryLimit; attempt++ {
		step.Attempts = attempt

		tctx, cancel := context.WithTimeout(ctx, step.Timeout)
		output, err := step.Func(tctx, input)
		cancel()

		if err == nil {
			step.Output = output
			step.Status = StepCompleted
			step.CompletedAt = time.Now()
			// 合并输出到全局上下文
			w.mu.Lock()
			for k, v := range output {
				w.Context[k] = v
			}
			w.mu.Unlock()
			return nil
		}

		lastErr = err
		if attempt < step.RetryLimit {
			time.Sleep(time.Duration(attempt) * 2 * time.Second) // 指数退避
		}
	}

	step.Status = StepFailed
	step.Error = lastErr
	return lastErr
}

// topologicalSort 拓扑排序，返回可并行执行的层次
func (w *Workflow) topologicalSort() ([][]string, error) {
	inDegree := make(map[string]int)
	for id := range w.Steps {
		inDegree[id] = 0
	}
	for id, step := range w.Steps {
		_ = id
		for _, dep := range step.DependsOn {
			if _, ok := w.Steps[dep]; !ok {
				return nil, fmt.Errorf("unknown dependency: %s", dep)
			}
			inDegree[step.ID]++
		}
	}

	var layers [][]string
	for len(inDegree) > 0 {
		var layer []string
		for id, deg := range inDegree {
			if deg == 0 {
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			return nil, fmt.Errorf("circular dependency detected")
		}
		layers = append(layers, layer)
		for _, id := range layer {
			delete(inDegree, id)
			// 减少依赖此步骤的步骤的 in-degree
			for oid, step := range w.Steps {
				if _, done := inDegree[oid]; !done {
					continue
				}
				for _, dep := range step.DependsOn {
					if dep == id {
						inDegree[oid]--
					}
				}
			}
		}
	}
	return layers, nil
}

// Summary 返回工作流摘要
func (w *Workflow) Summary() map[string]interface{} {
	steps := make([]map[string]interface{}, 0)
	for _, s := range w.Steps {
		steps = append(steps, map[string]interface{}{
			"id":       s.ID,
			"name":     s.Name,
			"status":   s.Status,
			"attempts": s.Attempts,
			"duration": s.CompletedAt.Sub(s.StartedAt).Milliseconds(),
			"error":    fmt.Sprintf("%v", s.Error),
		})
	}
	return map[string]interface{}{
		"id":      w.ID,
		"name":    w.Name,
		"status":  w.Status,
		"steps":   steps,
		"context": w.Context,
	}
}

// ---- 预置工作流模板 ----

// NewOutsourcingWorkflow 外包项目交付工作流
func NewOutsourcingWorkflow(projectName string) *Workflow {
	wf := NewWorkflow("outsource-"+projectName, "外包项目: "+projectName)
	wf.AddStep(&Step{
		ID:          "requirements",
		Name:        "需求分析",
		AgentType:   "pm",
		Description: "分析项目需求，输出需求文档",
		DependsOn:   []string{},
		RetryLimit:  2,
		Timeout:     120 * time.Second,
	})
	wf.AddStep(&Step{
		ID:          "architecture",
		Name:        "架构设计",
		AgentType:   "architect",
		Description: "设计系统架构，输出技术方案",
		DependsOn:   []string{"requirements"},
		RetryLimit:  2,
		Timeout:     120 * time.Second,
	})
	wf.AddStep(&Step{
		ID:          "frontend_dev",
		Name:        "前端开发",
		AgentType:   "frontend",
		Description: "实现前端页面",
		DependsOn:   []string{"architecture"},
		RetryLimit:  3,
		Timeout:     300 * time.Second,
	})
	wf.AddStep(&Step{
		ID:          "backend_dev",
		Name:        "后端开发",
		AgentType:   "backend",
		Description: "实现后端接口",
		DependsOn:   []string{"architecture"},
		RetryLimit:  3,
		Timeout:     300 * time.Second,
	})
	wf.AddStep(&Step{
		ID:          "testing",
		Name:        "集成测试",
		AgentType:   "tester",
		Description: "执行测试用例",
		DependsOn:   []string{"frontend_dev", "backend_dev"},
		RetryLimit:  2,
		Timeout:     180 * time.Second,
	})
	wf.AddStep(&Step{
		ID:          "deploy",
		Name:        "生产部署",
		AgentType:   "devops",
		Description: "部署到生产环境",
		DependsOn:   []string{"testing"},
		RetryLimit:  2,
		Timeout:     120 * time.Second,
	})
	return wf
}
