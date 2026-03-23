package database

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestDBIntegration tests the full database layer against a running PostgreSQL.
// Run with: go test -v -run TestDBIntegration ./pkg/database/ -tags integration
func TestDBIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := DefaultConfig()
	db, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer db.Close()

	// 1. Health check
	health, err := db.HealthCheck(ctx)
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	t.Logf("Health: %+v", health)

	// 2. List agents (seeded)
	agents, err := db.Agents.List(ctx, "")
	if err != nil {
		t.Fatalf("List agents failed: %v", err)
	}
	if len(agents) < 5 {
		t.Fatalf("Expected at least 5 seeded agents, got %d", len(agents))
	}
	t.Logf("Found %d agents", len(agents))
	for _, a := range agents {
		t.Logf("  Agent: %s (role=%s, status=%s)", a.Name, a.Role, a.Status)
	}

	// 3. Create a task
	agentID := agents[0].ID
	task := &Task{
		AgentID:     &agentID,
		Title:       "Test code generation",
		Description: "Generate a hello world function",
		TaskType:    "code_gen",
		Priority:    3,
		InputData:   map[string]interface{}{"language": "go", "prompt": "hello world"},
		MaxRetries:  3,
	}
	taskID, err := db.Tasks.Create(ctx, task)
	if err != nil {
		t.Fatalf("Create task failed: %v", err)
	}
	t.Logf("Created task: %s", taskID)

	// 4. Update task status
	err = db.Tasks.UpdateStatus(ctx, taskID, "running", nil, nil)
	if err != nil {
		t.Fatalf("Update task status failed: %v", err)
	}

	output := map[string]interface{}{"code": "func hello() { fmt.Println(\"hello\") }"}
	err = db.Tasks.UpdateStatus(ctx, taskID, "completed", output, nil)
	if err != nil {
		t.Fatalf("Complete task failed: %v", err)
	}
	t.Log("Task completed successfully")

	// 5. Record inference metrics
	metric := &InferenceMetric{
		AgentID:          &agentID,
		Model:            "deepseek-chat",
		PromptTokens:     150,
		CompletionTokens: 200,
		TotalTokens:      350,
		LatencyMs:        1200,
		TTFTMs:           180,
		TPS:              166.7,
		CacheHit:         false,
		Status:           "success",
	}
	if err := db.Metrics.Record(ctx, metric); err != nil {
		t.Fatalf("Record metric failed: %v", err)
	}
	t.Log("Metric recorded")

	// 6. Get inference stats
	stats, err := db.Metrics.GetStats(ctx, 24)
	if err != nil {
		t.Fatalf("Get stats failed: %v", err)
	}
	t.Logf("Stats: requests=%d, avg_latency=%.1fms, tokens=%d",
		stats.TotalRequests, stats.AvgLatency, stats.TotalTokens)

	// 7. Insert knowledge entry (without embedding for now)
	kbEntry := &KnowledgeEntry{
		Title:       "Go concurrency patterns",
		Content:     "Goroutines and channels are the building blocks of Go concurrency.",
		ContentType: "text",
		Source:      "docs/go-patterns.md",
		Metadata:    map[string]interface{}{"category": "golang"},
		Language:    "en",
	}
	kbID, err := db.KB.Insert(ctx, kbEntry)
	if err != nil {
		t.Fatalf("Insert KB entry failed: %v", err)
	}
	t.Logf("Knowledge entry created: %s", kbID)

	// 8. List active models
	models, err := db.Models.ListActive(ctx)
	if err != nil {
		t.Fatalf("List models failed: %v", err)
	}
	t.Logf("Found %d active models", len(models))
	for _, m := range models {
		t.Logf("  Model: %s v%s (provider=%s, type=%s)", m.Name, m.Version, m.Provider, m.ModelType)
	}

	// 9. Audit log
	audit := &AuditEntry{
		UserID:       "system",
		Action:       "create",
		ResourceType: "task",
		ResourceID:   taskID,
		Details:      map[string]interface{}{"test": true},
	}
	if err := db.Audit.Log(ctx, audit); err != nil {
		t.Fatalf("Audit log failed: %v", err)
	}

	recent, err := db.Audit.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent audit failed: %v", err)
	}
	t.Logf("Audit entries: %d", len(recent))

	// 10. Update agent stats
	if err := db.Agents.IncrementTaskStats(ctx, agentID, true, 1200); err != nil {
		t.Fatalf("Increment stats failed: %v", err)
	}

	updated, err := db.Agents.GetByID(ctx, agentID)
	if err != nil {
		t.Fatalf("Get updated agent failed: %v", err)
	}
	t.Logf("Agent %s: total_tasks=%d, success_tasks=%d, avg_latency=%.1fms",
		updated.Name, updated.TotalTasks, updated.SuccessTasks, updated.AvgLatencyMs)

	fmt.Println("\n=== All database integration tests passed! ===")
}
