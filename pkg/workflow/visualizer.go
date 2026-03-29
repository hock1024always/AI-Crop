// Package workflow - DAG 可视化支持
// 提供工作流执行状态的实时追踪、历史记录和可视化数据
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// WorkflowExecution 执行记录（用于持久化和可视化）
type WorkflowExecution struct {
	ID          string                 `json:"id"`
	WorkflowID  string                 `json:"workflow_id"`
	Name        string                 `json:"name"`
	Status      StepStatus             `json:"status"`
	StepStates  map[string]*StepState  `json:"step_states"` // step_id -> state
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt time.Time              `json:"completed_at,omitempty"`
	Context     map[string]interface{} `json:"context"`
	Error       string                 `json:"error,omitempty"`
	Duration    int64                  `json:"duration_ms"`
}

// StepState 步骤执行状态
type StepState struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	AgentType   string                 `json:"agent_type"`
	Status      StepStatus             `json:"status"`
	StartedAt   time.Time              `json:"started_at,omitempty"`
	CompletedAt time.Time              `json:"completed_at,omitempty"`
	Duration    int64                  `json:"duration_ms"`
	Attempts    int                    `json:"attempts"`
	Error       string                 `json:"error,omitempty"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Progress    int                    `json:"progress"` // 0-100
}

// WorkflowVisualizer 工作流可视化器
type WorkflowVisualizer struct {
	executions map[string]*WorkflowExecution // execution_id -> execution
	active     map[string]*WorkflowExecution // workflow_id -> active execution
	mu         sync.RWMutex
	subscribers []chan *WorkflowEvent
	subMu      sync.RWMutex
}

// WorkflowEvent 工作流事件（用于实时推送）
type WorkflowEvent struct {
	Type        string      `json:"type"` // started, step_started, step_completed, completed, failed
	ExecutionID string      `json:"execution_id"`
	WorkflowID  string      `json:"workflow_id"`
	StepID      string      `json:"step_id,omitempty"`
	StepState   *StepState  `json:"step_state,omitempty"`
	Timestamp   time.Time   `json:"timestamp"`
	Status      StepStatus  `json:"status"`
	Error       string      `json:"error,omitempty"`
}

// NewWorkflowVisualizer 创建可视化器
func NewWorkflowVisualizer() *WorkflowVisualizer {
	return &WorkflowVisualizer{
		executions: make(map[string]*WorkflowExecution),
		active:     make(map[string]*WorkflowExecution),
	}
}

// StartExecution 开始执行追踪
func (v *WorkflowVisualizer) StartExecution(wf *Workflow) string {
	execID := fmt.Sprintf("exec-%d", time.Now().UnixNano())

	exec := &WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Name:       wf.Name,
		Status:     StepRunning,
		StepStates: make(map[string]*StepState),
		StartedAt:  time.Now(),
		Context:    wf.Context,
	}

	// 初始化所有步骤状态
	for stepID, step := range wf.Steps {
		exec.StepStates[stepID] = &StepState{
			ID:        step.ID,
			Name:      step.Name,
			AgentType: step.AgentType,
			Status:    StepPending,
		}
	}

	v.mu.Lock()
	v.executions[execID] = exec
	v.active[wf.ID] = exec
	v.mu.Unlock()

	// 发送开始事件
	v.emit(&WorkflowEvent{
		Type:        "started",
		ExecutionID: execID,
		WorkflowID:  wf.ID,
		Timestamp:   time.Now(),
		Status:      StepRunning,
	})

	log.Printf("[WorkflowVisualizer] Execution started: %s (workflow: %s)", execID, wf.ID)
	return execID
}

// StepStarted 记录步骤开始
func (v *WorkflowVisualizer) StepStarted(wfID, execID, stepID string, step *Step) {
	v.mu.RLock()
	exec, exists := v.executions[execID]
	v.mu.RUnlock()

	if !exists {
		return
	}

	state := &StepState{
		ID:        step.ID,
		Name:      step.Name,
		AgentType: step.AgentType,
		Status:    StepRunning,
		StartedAt: time.Now(),
		Progress:  0,
	}

	v.mu.Lock()
	exec.StepStates[stepID] = state
	v.mu.Unlock()

	v.emit(&WorkflowEvent{
		Type:        "step_started",
		ExecutionID: execID,
		WorkflowID:  wfID,
		StepID:      stepID,
		StepState:   state,
		Timestamp:   time.Now(),
		Status:      StepRunning,
	})
}

// StepProgress 更新步骤进度
func (v *WorkflowVisualizer) StepProgress(wfID, execID, stepID string, progress int) {
	v.mu.Lock()
	defer v.mu.Unlock()

	exec, exists := v.executions[execID]
	if !exists {
		return
	}

	if state, ok := exec.StepStates[stepID]; ok {
		state.Progress = progress
	}
}

// StepCompleted 记录步骤完成
func (v *WorkflowVisualizer) StepCompleted(wfID, execID, stepID string, step *Step) {
	v.mu.RLock()
	exec, exists := v.executions[execID]
	v.mu.RUnlock()

	if !exists {
		return
	}

	now := time.Now()
	var duration int64
	var startedAt time.Time

	v.mu.Lock()
	if state, ok := exec.StepStates[stepID]; ok {
		startedAt = state.StartedAt
		if !startedAt.IsZero() {
			duration = now.Sub(startedAt).Milliseconds()
		}
		state.Status = step.Status
		state.CompletedAt = now
		state.Duration = duration
		state.Attempts = step.Attempts
		state.Output = step.Output
		state.Progress = 100
		if step.Error != nil {
			state.Error = step.Error.Error()
		}
	}
	v.mu.Unlock()

	event := &WorkflowEvent{
		Type:        "step_completed",
		ExecutionID: execID,
		WorkflowID:  wfID,
		StepID:      stepID,
		Timestamp:   now,
		Status:      step.Status,
	}

	v.mu.RLock()
	event.StepState = exec.StepStates[stepID]
	v.mu.RUnlock()

	v.emit(event)
}

// ExecutionCompleted 记录执行完成
func (v *WorkflowVisualizer) ExecutionCompleted(wfID, execID string, status StepStatus, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	exec, exists := v.executions[execID]
	if !exists {
		return
	}

	exec.Status = status
	exec.CompletedAt = time.Now()
	exec.Duration = exec.CompletedAt.Sub(exec.StartedAt).Milliseconds()
	if err != nil {
		exec.Error = err.Error()
	}

	delete(v.active, wfID)

	v.emit(&WorkflowEvent{
		Type:        "completed",
		ExecutionID: execID,
		WorkflowID:  wfID,
		Timestamp:   exec.CompletedAt,
		Status:      status,
		Error:       exec.Error,
	})

	log.Printf("[WorkflowVisualizer] Execution completed: %s (status: %s, duration: %dms)", execID, status, exec.Duration)
}

// Subscribe 订阅工作流事件
func (v *WorkflowVisualizer) Subscribe() chan *WorkflowEvent {
	ch := make(chan *WorkflowEvent, 100)
	v.subMu.Lock()
	v.subscribers = append(v.subscribers, ch)
	v.subMu.Unlock()
	return ch
}

// Unsubscribe 取消订阅
func (v *WorkflowVisualizer) Unsubscribe(ch chan *WorkflowEvent) {
	v.subMu.Lock()
	defer v.subMu.Unlock()

	for i, sub := range v.subscribers {
		if sub == ch {
			v.subscribers = append(v.subscribers[:i], v.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// emit 发送事件
func (v *WorkflowVisualizer) emit(event *WorkflowEvent) {
	v.subMu.RLock()
	defer v.subMu.RUnlock()

	for _, ch := range v.subscribers {
		select {
		case ch <- event:
		default:
			// channel full, skip
		}
	}
}

// GetExecution 获取执行记录
func (v *WorkflowVisualizer) GetExecution(execID string) *WorkflowExecution {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.executions[execID]
}

// GetActiveExecution 获取工作流的活跃执行
func (v *WorkflowVisualizer) GetActiveExecution(wfID string) *WorkflowExecution {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.active[wfID]
}

// ListExecutions 列出执行历史
func (v *WorkflowVisualizer) ListExecutions(limit int) []*WorkflowExecution {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if limit <= 0 || limit > len(v.executions) {
		limit = len(v.executions)
	}

	result := make([]*WorkflowExecution, 0, limit)
	for _, exec := range v.executions {
		result = append(result, exec)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// ToDAGGraph 生成 DAG 图结构（用于前端可视化）
func (exec *WorkflowExecution) ToDAGGraph() *DAGGraph {
	graph := &DAGGraph{
		Nodes: make([]*DAGNode, 0, len(exec.StepStates)),
		Edges: make([]*DAGEdge, 0),
	}

	for _, state := range exec.StepStates {
		graph.Nodes = append(graph.Nodes, &DAGNode{
			ID:     state.ID,
			Name:   state.Name,
			Type:   state.AgentType,
			Status: string(state.Status),
			Progress: state.Progress,
			Duration: state.Duration,
			Error:   state.Error,
		})
	}

	return graph
}

// DAGGraph DAG 图结构
type DAGGraph struct {
	Nodes []*DAGNode `json:"nodes"`
	Edges []*DAGEdge `json:"edges"`
}

// DAGNode DAG 节点
type DAGNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Duration int64  `json:"duration_ms"`
	Error    string `json:"error,omitempty"`
}

// DAGEdge DAG 边
type DAGEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // sequential, parallel, conditional
}

// ToJSON 转换为 JSON
func (exec *WorkflowExecution) ToJSON() ([]byte, error) {
	return json.MarshalIndent(exec, "", "  ")
}

// ToJSON 实现 Workflow 的 DAG 图生成
func (w *Workflow) ToDAGGraph() *DAGGraph {
	graph := &DAGGraph{
		Nodes: make([]*DAGNode, 0, len(w.Steps)),
		Edges: make([]*DAGEdge, 0),
	}

	for _, step := range w.Steps {
		graph.Nodes = append(graph.Nodes, &DAGNode{
			ID:     step.ID,
			Name:   step.Name,
			Type:   step.AgentType,
			Status: string(step.Status),
		})

		// 生成边
		for _, dep := range step.DependsOn {
			graph.Edges = append(graph.Edges, &DAGEdge{
				From: dep,
				To:   step.ID,
				Type: "sequential",
			})
		}
	}

	return graph
}

// TrackedWorkflow 带追踪的工作流包装
type TrackedWorkflow struct {
	*Workflow
	Visualizer *WorkflowVisualizer
	execID     string
}

// NewTrackedWorkflow 创建带追踪的工作流
func NewTrackedWorkflow(wf *Workflow, viz *WorkflowVisualizer) *TrackedWorkflow {
	return &TrackedWorkflow{
		Workflow:   wf,
		Visualizer: viz,
	}
}

// Run 执行工作流并追踪状态
func (tw *TrackedWorkflow) Run(ctx context.Context) error {
	tw.execID = tw.Visualizer.StartExecution(tw.Workflow)

	// 包装步骤函数以追踪状态
	originalSteps := make(map[string]*Step)
	for id, step := range tw.Steps {
		originalSteps[id] = &Step{
			ID:          step.ID,
			Name:        step.Name,
			Description: step.Description,
			AgentType:   step.AgentType,
			DependsOn:   step.DependsOn,
			Condition:   step.Condition,
			RetryLimit:  step.RetryLimit,
			Timeout:     step.Timeout,
		}
		originalSteps[id].Func = tw.wrapStepFunc(step)
	}

	// 替换步骤
	tw.Steps = originalSteps

	err := tw.Workflow.Run(ctx)

	status := StepCompleted
	if err != nil {
		status = StepFailed
	}
	tw.Visualizer.ExecutionCompleted(tw.ID, tw.execID, status, err)

	return err
}

func (tw *TrackedWorkflow) wrapStepFunc(step *Step) StepFunc {
	originalFunc := step.Func
	if originalFunc == nil {
		originalFunc = func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"simulated": true}, nil
		}
	}

	return func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
		tw.Visualizer.StepStarted(tw.ID, tw.execID, step.ID, step)

		output, err := originalFunc(ctx, input)

		// 更新步骤状态
		if err != nil {
			step.Status = StepFailed
			step.Error = err
		} else {
			step.Status = StepCompleted
			step.Output = output
		}

		tw.Visualizer.StepCompleted(tw.ID, tw.execID, step.ID, step)

		return output, err
	}
}
