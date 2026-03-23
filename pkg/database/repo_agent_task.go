package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentRepo provides CRUD operations for agents.
type AgentRepo struct {
	pool *pgxpool.Pool
}

// List returns all agents, optionally filtered by role.
func (r *AgentRepo) List(ctx context.Context, role string) ([]Agent, error) {
	query := `SELECT id, name, role, status, model, system_prompt, config, skills,
		max_concurrent, total_tasks, success_tasks, avg_latency_ms, created_at, updated_at
		FROM agents`
	args := []interface{}{}

	if role != "" {
		query += " WHERE role = $1"
		args = append(args, role)
	}
	query += " ORDER BY name"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		var configJSON, skillsJSON []byte
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Role, &a.Status, &a.Model, &a.SystemPrompt,
			&configJSON, &skillsJSON,
			&a.MaxConcurrent, &a.TotalTasks, &a.SuccessTasks, &a.AvgLatencyMs,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		json.Unmarshal(configJSON, &a.Config)
		json.Unmarshal(skillsJSON, &a.Skills)
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// GetByID returns a single agent by UUID.
func (r *AgentRepo) GetByID(ctx context.Context, id string) (*Agent, error) {
	var a Agent
	var configJSON, skillsJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, role, status, model, system_prompt, config, skills,
		max_concurrent, total_tasks, success_tasks, avg_latency_ms, created_at, updated_at
		FROM agents WHERE id = $1`, id,
	).Scan(
		&a.ID, &a.Name, &a.Role, &a.Status, &a.Model, &a.SystemPrompt,
		&configJSON, &skillsJSON,
		&a.MaxConcurrent, &a.TotalTasks, &a.SuccessTasks, &a.AvgLatencyMs,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	json.Unmarshal(configJSON, &a.Config)
	json.Unmarshal(skillsJSON, &a.Skills)
	return &a, nil
}

// Create inserts a new agent and returns its ID.
func (r *AgentRepo) Create(ctx context.Context, a *Agent) (string, error) {
	configJSON, _ := json.Marshal(a.Config)
	skillsJSON, _ := json.Marshal(a.Skills)

	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO agents (name, role, model, system_prompt, config, skills, max_concurrent)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		a.Name, a.Role, a.Model, a.SystemPrompt, configJSON, skillsJSON, a.MaxConcurrent,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create agent: %w", err)
	}
	return id, nil
}

// UpdateStatus atomically updates an agent's status.
func (r *AgentRepo) UpdateStatus(ctx context.Context, id, status string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE agents SET status = $1 WHERE id = $2`, status, id)
	if err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent not found: %s", id)
	}
	return nil
}

// IncrementTaskStats updates task counters after a task completes.
func (r *AgentRepo) IncrementTaskStats(ctx context.Context, id string, success bool, latencyMs int) error {
	successInc := 0
	if success {
		successInc = 1
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE agents SET
			total_tasks = total_tasks + 1,
			success_tasks = success_tasks + $1,
			avg_latency_ms = (avg_latency_ms * total_tasks + $2) / (total_tasks + 1)
		WHERE id = $3`,
		successInc, latencyMs, id,
	)
	return err
}

// TaskRepo provides CRUD operations for tasks.
type TaskRepo struct {
	pool *pgxpool.Pool
}

// Create inserts a new task and returns its ID.
func (r *TaskRepo) Create(ctx context.Context, t *Task) (string, error) {
	inputJSON, _ := json.Marshal(t.InputData)

	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO tasks (agent_id, workflow_id, title, description, task_type, priority, input_data, max_retries)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		t.AgentID, t.WorkflowID, t.Title, t.Description, t.TaskType, t.Priority, inputJSON, t.MaxRetries,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}
	return id, nil
}

// UpdateStatus transitions a task to a new status with optional output.
func (r *TaskRepo) UpdateStatus(ctx context.Context, id, status string, output map[string]interface{}, errMsg *string) error {
	outputJSON, _ := json.Marshal(output)
	now := time.Now()

	var startedAt, completedAt *time.Time
	switch status {
	case "running":
		startedAt = &now
	case "completed", "failed":
		completedAt = &now
	}

	_, err := r.pool.Exec(ctx,
		`UPDATE tasks SET status = $1, output_data = $2, error_message = $3,
			started_at = COALESCE($4, started_at), completed_at = COALESCE($5, completed_at)
		WHERE id = $6`,
		status, outputJSON, errMsg, startedAt, completedAt, id,
	)
	return err
}

// ListByAgent returns recent tasks for a given agent.
func (r *TaskRepo) ListByAgent(ctx context.Context, agentID string, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, workflow_id, title, description, task_type, status,
			priority, tokens_used, latency_ms, retry_count, created_at
		FROM tasks WHERE agent_id = $1 ORDER BY created_at DESC LIMIT $2`,
		agentID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID, &t.AgentID, &t.WorkflowID, &t.Title, &t.Description, &t.TaskType,
			&t.Status, &t.Priority, &t.TokensUsed, &t.LatencyMs, &t.RetryCount, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListPending returns tasks in pending status, ordered by priority.
func (r *TaskRepo) ListPending(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, title, task_type, priority, created_at
		FROM tasks WHERE status = 'pending' ORDER BY priority ASC, created_at ASC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.AgentID, &t.Title, &t.TaskType, &t.Priority, &t.CreatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// RecordCompletion finalizes a task with token/latency stats.
func (r *TaskRepo) RecordCompletion(ctx context.Context, id string, tokensUsed, latencyMs int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE tasks SET tokens_used = $1, latency_ms = $2 WHERE id = $3`,
		tokensUsed, latencyMs, id,
	)
	return err
}
