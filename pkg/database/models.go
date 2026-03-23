package database

import (
	"time"
)

// Agent maps to the agents table.
type Agent struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Role          string                 `json:"role"`
	Status        string                 `json:"status"`
	Model         string                 `json:"model"`
	SystemPrompt  string                 `json:"system_prompt,omitempty"`
	Config        map[string]interface{} `json:"config"`
	Skills        []string               `json:"skills"`
	MaxConcurrent int                    `json:"max_concurrent"`
	TotalTasks    int64                  `json:"total_tasks"`
	SuccessTasks  int64                  `json:"success_tasks"`
	AvgLatencyMs  float64                `json:"avg_latency_ms"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// Task maps to the tasks table.
type Task struct {
	ID           string                 `json:"id"`
	AgentID      *string                `json:"agent_id,omitempty"`
	WorkflowID   *string                `json:"workflow_id,omitempty"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description,omitempty"`
	TaskType     string                 `json:"task_type"`
	Status       string                 `json:"status"`
	Priority     int                    `json:"priority"`
	InputData    map[string]interface{} `json:"input_data"`
	OutputData   map[string]interface{} `json:"output_data,omitempty"`
	ErrorMessage *string                `json:"error_message,omitempty"`
	TokensUsed   int                    `json:"tokens_used"`
	LatencyMs    int                    `json:"latency_ms"`
	RetryCount   int                    `json:"retry_count"`
	MaxRetries   int                    `json:"max_retries"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// KnowledgeEntry maps to the knowledge_base table.
type KnowledgeEntry struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	ContentType string                 `json:"content_type"`
	Source      string                 `json:"source,omitempty"`
	Metadata    map[string]interface{} `json:"metadata"`
	Embedding   []float32              `json:"embedding,omitempty"`
	ChunkIndex  int                    `json:"chunk_index"`
	ChunkTotal  int                    `json:"chunk_total"`
	Language    string                 `json:"language"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// InferenceMetric maps to the inference_metrics table.
type InferenceMetric struct {
	ID               int64     `json:"id"`
	RequestID        string    `json:"request_id"`
	AgentID          *string   `json:"agent_id,omitempty"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	TTFTMs           int       `json:"ttft_ms"`
	TPS              float64   `json:"tps"`
	CacheHit         bool      `json:"cache_hit"`
	Status           string    `json:"status"`
	ErrorCode        *string   `json:"error_code,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// WorkflowRun maps to the workflow_runs table.
type WorkflowRun struct {
	ID             string                 `json:"id"`
	WorkflowName   string                 `json:"workflow_name"`
	Status         string                 `json:"status"`
	DAGDefinition  map[string]interface{} `json:"dag_definition"`
	StepResults    []interface{}          `json:"step_results"`
	TotalSteps     int                    `json:"total_steps"`
	CompletedSteps int                    `json:"completed_steps"`
	TriggeredBy    string                 `json:"triggered_by,omitempty"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

// ModelEntry maps to the model_registry table.
type ModelEntry struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	Provider        string                 `json:"provider"`
	ModelType       string                 `json:"model_type"`
	Quantization    *string                `json:"quantization,omitempty"`
	SizeGB          *float64               `json:"size_gb,omitempty"`
	Parameters      string                 `json:"parameters,omitempty"`
	Config          map[string]interface{} `json:"config"`
	EndpointURL     string                 `json:"endpoint_url,omitempty"`
	IsActive        bool                   `json:"is_active"`
	HealthStatus    string                 `json:"health_status"`
	LastHealthCheck *time.Time             `json:"last_health_check,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// AuditEntry maps to the audit_log table.
type AuditEntry struct {
	ID           int64                  `json:"id"`
	UserID       string                 `json:"user_id,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	Details      map[string]interface{} `json:"details,omitempty"`
	IPAddress    string                 `json:"ip_address,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

// InferenceStats is the result of aggregated metrics.
type InferenceStats struct {
	TotalRequests int64   `json:"total_requests"`
	SuccessCount  int64   `json:"success_count"`
	ErrorCount    int64   `json:"error_count"`
	AvgLatency    float64 `json:"avg_latency"`
	P95Latency    float64 `json:"p95_latency"`
	TotalTokens   int64   `json:"total_tokens"`
	AvgTPS        float64 `json:"avg_tps"`
	CacheHitRate  float64 `json:"cache_hit_rate"`
}
