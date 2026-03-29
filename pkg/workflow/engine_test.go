package workflow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewWorkflow(t *testing.T) {
	wf := NewWorkflow("test-1", "Test Workflow")
	if wf.ID != "test-1" {
		t.Errorf("ID: got %q, want %q", wf.ID, "test-1")
	}
	if wf.Status != StepPending {
		t.Errorf("status: got %q, want %q", wf.Status, StepPending)
	}
}

func TestAddStep(t *testing.T) {
	wf := NewWorkflow("test-2", "Test")
	wf.AddStep(&Step{
		ID:   "step1",
		Name: "Step 1",
	})
	if len(wf.Steps) != 1 {
		t.Errorf("steps: got %d, want 1", len(wf.Steps))
	}
	if wf.Steps["step1"].RetryLimit != 1 {
		t.Error("default retry limit should be 1")
	}
	if wf.Steps["step1"].Timeout != 60*time.Second {
		t.Error("default timeout should be 60s")
	}
}

func TestLinearWorkflow(t *testing.T) {
	wf := NewWorkflow("linear-1", "Linear")

	var order []string
	makeFunc := func(name string) StepFunc {
		return func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			order = append(order, name)
			return map[string]interface{}{name: "done"}, nil
		}
	}

	wf.AddStep(&Step{ID: "a", Name: "A", Func: makeFunc("a")})
	wf.AddStep(&Step{ID: "b", Name: "B", Func: makeFunc("b"), DependsOn: []string{"a"}})
	wf.AddStep(&Step{ID: "c", Name: "C", Func: makeFunc("c"), DependsOn: []string{"b"}})

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if wf.Status != StepCompleted {
		t.Errorf("status: got %q, want %q", wf.Status, StepCompleted)
	}

	// 验证顺序
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("execution order: got %v, want [a b c]", order)
	}
}

func TestParallelWorkflow(t *testing.T) {
	wf := NewWorkflow("parallel-1", "Parallel")

	var counter int64
	makeFunc := func() StepFunc {
		return func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			atomic.AddInt64(&counter, 1)
			time.Sleep(50 * time.Millisecond) // 模拟耗时
			return map[string]interface{}{"done": true}, nil
		}
	}

	// a → (b, c) → d
	wf.AddStep(&Step{ID: "a", Name: "A", Func: makeFunc()})
	wf.AddStep(&Step{ID: "b", Name: "B", Func: makeFunc(), DependsOn: []string{"a"}})
	wf.AddStep(&Step{ID: "c", Name: "C", Func: makeFunc(), DependsOn: []string{"a"}})
	wf.AddStep(&Step{ID: "d", Name: "D", Func: makeFunc(), DependsOn: []string{"b", "c"}})

	start := time.Now()
	err := wf.Run(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if counter != 4 {
		t.Errorf("counter: got %d, want 4", counter)
	}
	// b 和 c 应该并行执行，所以总时间应该 < 200ms (4*50ms)
	if elapsed > 300*time.Millisecond {
		t.Errorf("parallel execution took too long: %v", elapsed)
	}
}

func TestConditionalStep(t *testing.T) {
	wf := NewWorkflow("cond-1", "Conditional")

	executed := make(map[string]bool)
	makeFunc := func(name string) StepFunc {
		return func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			executed[name] = true
			return map[string]interface{}{name: "done"}, nil
		}
	}

	wf.AddStep(&Step{
		ID: "a", Name: "A", Func: makeFunc("a"),
	})
	wf.AddStep(&Step{
		ID: "b", Name: "B (skip)", Func: makeFunc("b"),
		DependsOn: []string{"a"},
		Condition: func(ctx map[string]interface{}) bool { return false }, // 跳过
	})
	wf.AddStep(&Step{
		ID: "c", Name: "C", Func: makeFunc("c"),
		DependsOn: []string{"a"},
		Condition: func(ctx map[string]interface{}) bool { return true }, // 执行
	})

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !executed["a"] || !executed["c"] {
		t.Error("a and c should be executed")
	}
	if executed["b"] {
		t.Error("b should be skipped")
	}
	if wf.Steps["b"].Status != StepSkipped {
		t.Errorf("step b status: got %q, want %q", wf.Steps["b"].Status, StepSkipped)
	}
}

func TestStepRetry(t *testing.T) {
	wf := NewWorkflow("retry-1", "Retry")

	var attempts int
	wf.AddStep(&Step{
		ID:         "flaky",
		Name:       "Flaky Step",
		RetryLimit: 3,
		Timeout:    5 * time.Second,
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			attempts++
			if attempts < 3 {
				return nil, fmt.Errorf("transient error (attempt %d)", attempts)
			}
			return map[string]interface{}{"success": true}, nil
		},
	})

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts: got %d, want 3", attempts)
	}
	if wf.Steps["flaky"].Status != StepCompleted {
		t.Errorf("step status: got %q, want %q", wf.Steps["flaky"].Status, StepCompleted)
	}
}

func TestStepFailure(t *testing.T) {
	wf := NewWorkflow("fail-1", "Failure")

	wf.AddStep(&Step{
		ID:         "broken",
		Name:       "Broken Step",
		RetryLimit: 2,
		Timeout:    5 * time.Second,
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			return nil, fmt.Errorf("permanent error")
		},
	})

	err := wf.Run(context.Background())
	if err == nil {
		t.Error("workflow should fail")
	}
	if wf.Status != StepFailed {
		t.Errorf("status: got %q, want %q", wf.Status, StepFailed)
	}
	if wf.Steps["broken"].Attempts != 2 {
		t.Errorf("attempts: got %d, want 2", wf.Steps["broken"].Attempts)
	}
}

func TestCircularDependency(t *testing.T) {
	wf := NewWorkflow("circular-1", "Circular")

	wf.AddStep(&Step{ID: "a", Name: "A", DependsOn: []string{"c"},
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) { return nil, nil }})
	wf.AddStep(&Step{ID: "b", Name: "B", DependsOn: []string{"a"},
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) { return nil, nil }})
	wf.AddStep(&Step{ID: "c", Name: "C", DependsOn: []string{"b"},
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) { return nil, nil }})

	err := wf.Run(context.Background())
	if err == nil {
		t.Error("should detect circular dependency")
	}
}

func TestContextPropagation(t *testing.T) {
	wf := NewWorkflow("ctx-1", "Context")

	wf.AddStep(&Step{
		ID: "producer", Name: "Producer",
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"key": "value123"}, nil
		},
	})
	wf.AddStep(&Step{
		ID: "consumer", Name: "Consumer", DependsOn: []string{"producer"},
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			if input["key"] != "value123" {
				return nil, fmt.Errorf("context not propagated: got %v", input["key"])
			}
			return map[string]interface{}{"verified": true}, nil
		},
	})

	err := wf.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestOutsourcingWorkflowTemplate(t *testing.T) {
	wf := NewOutsourcingWorkflow("TestProject")

	if len(wf.Steps) != 6 {
		t.Errorf("outsourcing workflow should have 6 steps, got %d", len(wf.Steps))
	}

	expectedSteps := []string{"requirements", "architecture", "frontend_dev", "backend_dev", "testing", "deploy"}
	for _, id := range expectedSteps {
		if _, exists := wf.Steps[id]; !exists {
			t.Errorf("missing step: %s", id)
		}
	}

	// frontend_dev 和 backend_dev 应该同层（都依赖 architecture）
	if wf.Steps["frontend_dev"].DependsOn[0] != "architecture" {
		t.Error("frontend_dev should depend on architecture")
	}
	if wf.Steps["backend_dev"].DependsOn[0] != "architecture" {
		t.Error("backend_dev should depend on architecture")
	}
}

func TestWorkflowSummary(t *testing.T) {
	wf := NewWorkflow("summary-1", "Summary Test")
	wf.AddStep(&Step{
		ID: "s1", Name: "Step 1",
		Func: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
			return nil, nil
		},
	})

	summary := wf.Summary()
	if summary["id"] != "summary-1" {
		t.Errorf("id: got %v, want %q", summary["id"], "summary-1")
	}
	if summary["name"] != "Summary Test" {
		t.Errorf("name: got %v, want %q", summary["name"], "Summary Test")
	}
}
