package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

// KnowledgeBaseRepo provides operations for the RAG knowledge base.
type KnowledgeBaseRepo struct {
	pool *pgxpool.Pool
}

// Insert adds a knowledge entry with optional embedding vector.
func (r *KnowledgeBaseRepo) Insert(ctx context.Context, entry *KnowledgeEntry) (string, error) {
	metaJSON, _ := json.Marshal(entry.Metadata)

	var embedding *pgvector.Vector
	if len(entry.Embedding) > 0 {
		v := pgvector.NewVector(entry.Embedding)
		embedding = &v
	}

	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO knowledge_base (title, content, content_type, source, metadata, embedding, chunk_index, chunk_total, language)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		entry.Title, entry.Content, entry.ContentType, entry.Source,
		metaJSON, embedding, entry.ChunkIndex, entry.ChunkTotal, entry.Language,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert knowledge: %w", err)
	}
	return id, nil
}

// SearchSimilar performs vector similarity search using pgvector's cosine distance.
func (r *KnowledgeBaseRepo) SearchSimilar(ctx context.Context, queryEmbedding []float32, limit int, contentType string) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 5
	}

	qv := pgvector.NewVector(queryEmbedding)

	query := `SELECT id, title, content, content_type, source, metadata,
		1 - (embedding <=> $1::vector) AS similarity
		FROM knowledge_base WHERE embedding IS NOT NULL`
	args := []interface{}{qv}

	argIdx := 2
	if contentType != "" {
		query += fmt.Sprintf(" AND content_type = $%d", argIdx)
		args = append(args, contentType)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY embedding <=> $1::vector LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search similar: %w", err)
	}
	defer rows.Close()

	var results []KnowledgeEntry
	for rows.Next() {
		var e KnowledgeEntry
		var metaJSON []byte
		var similarity float64
		if err := rows.Scan(&e.ID, &e.Title, &e.Content, &e.ContentType, &e.Source, &metaJSON, &similarity); err != nil {
			return nil, err
		}
		json.Unmarshal(metaJSON, &e.Metadata)
		if e.Metadata == nil {
			e.Metadata = map[string]interface{}{}
		}
		e.Metadata["_similarity"] = similarity
		results = append(results, e)
	}
	return results, rows.Err()
}

// SearchText performs full-text trigram search on content.
func (r *KnowledgeBaseRepo) SearchText(ctx context.Context, keyword string, limit int) ([]KnowledgeEntry, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, title, content, content_type, source, metadata,
			similarity(content, $1) AS sim
		FROM knowledge_base
		WHERE content % $1
		ORDER BY sim DESC LIMIT $2`,
		keyword, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search text: %w", err)
	}
	defer rows.Close()

	var results []KnowledgeEntry
	for rows.Next() {
		var e KnowledgeEntry
		var metaJSON []byte
		var sim float64
		if err := rows.Scan(&e.ID, &e.Title, &e.Content, &e.ContentType, &e.Source, &metaJSON, &sim); err != nil {
			return nil, err
		}
		json.Unmarshal(metaJSON, &e.Metadata)
		if e.Metadata == nil {
			e.Metadata = map[string]interface{}{}
		}
		e.Metadata["_text_similarity"] = sim
		results = append(results, e)
	}
	return results, rows.Err()
}

// MetricsRepo provides operations for inference metrics.
type MetricsRepo struct {
	pool *pgxpool.Pool
}

// Record inserts a new inference metric entry.
func (r *MetricsRepo) Record(ctx context.Context, m *InferenceMetric) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO inference_metrics
			(agent_id, model, prompt_tokens, completion_tokens, total_tokens,
			 latency_ms, ttft_ms, tps, cache_hit, status, error_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		m.AgentID, m.Model, m.PromptTokens, m.CompletionTokens, m.TotalTokens,
		m.LatencyMs, m.TTFTMs, m.TPS, m.CacheHit, m.Status, m.ErrorCode,
	)
	return err
}

// GetStats returns aggregated inference stats for the given time window (hours).
func (r *MetricsRepo) GetStats(ctx context.Context, windowHours int) (*InferenceStats, error) {
	if windowHours <= 0 {
		windowHours = 24
	}

	var s InferenceStats
	err := r.pool.QueryRow(ctx,
		`SELECT * FROM get_inference_stats($1)`, windowHours,
	).Scan(
		&s.TotalRequests, &s.SuccessCount, &s.ErrorCount,
		&s.AvgLatency, &s.P95Latency, &s.TotalTokens, &s.AvgTPS, &s.CacheHitRate,
	)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}
	return &s, nil
}

// ModelRegistryRepo provides operations for the model registry.
type ModelRegistryRepo struct {
	pool *pgxpool.Pool
}

// ListActive returns all active model entries.
func (r *ModelRegistryRepo) ListActive(ctx context.Context) ([]ModelEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, version, provider, model_type, quantization, size_gb,
			parameters, config, endpoint_url, is_active, health_status, last_health_check,
			created_at, updated_at
		FROM model_registry WHERE is_active = true ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []ModelEntry
	for rows.Next() {
		var m ModelEntry
		var configJSON []byte
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Version, &m.Provider, &m.ModelType,
			&m.Quantization, &m.SizeGB, &m.Parameters, &configJSON,
			&m.EndpointURL, &m.IsActive, &m.HealthStatus, &m.LastHealthCheck,
			&m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		json.Unmarshal(configJSON, &m.Config)
		models = append(models, m)
	}
	return models, rows.Err()
}

// UpdateHealth updates the health status of a model.
func (r *ModelRegistryRepo) UpdateHealth(ctx context.Context, id, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE model_registry SET health_status = $1, last_health_check = NOW() WHERE id = $2`,
		status, id,
	)
	return err
}

// AuditRepo provides operations for the audit log.
type AuditRepo struct {
	pool *pgxpool.Pool
}

// Log records an audit event.
func (r *AuditRepo) Log(ctx context.Context, entry *AuditEntry) error {
	detailsJSON, _ := json.Marshal(entry.Details)
	_, err := r.pool.Exec(ctx,
		`INSERT INTO audit_log (user_id, action, resource_type, resource_id, details, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6::inet, $7)`,
		entry.UserID, entry.Action, entry.ResourceType, entry.ResourceID,
		detailsJSON, nilIfEmpty(entry.IPAddress), entry.UserAgent,
	)
	return err
}

// Recent returns the most recent audit entries.
func (r *AuditRepo) Recent(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, action, resource_type, resource_id, details, ip_address, created_at
		FROM audit_log ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var detailsJSON []byte
		var ipAddr *string
		if err := rows.Scan(&e.ID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID, &detailsJSON, &ipAddr, &e.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(detailsJSON, &e.Details)
		if ipAddr != nil {
			e.IPAddress = *ipAddr
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
