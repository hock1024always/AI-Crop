-- ============================================================
-- AI Corp - Phase 1 Database Schema
-- PostgreSQL 16 + pgvector Extension
-- ============================================================

-- Enable extensions (gen_random_uuid() is built-in since PG13, no uuid-ossp needed)
CREATE EXTENSION IF NOT EXISTS "vector";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";  -- Trigram similarity search

-- ============================================================
-- 1. AI Agents Table - Core agent configuration
-- ============================================================
CREATE TABLE IF NOT EXISTS agents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(128) NOT NULL,
    role            VARCHAR(64) NOT NULL,           -- frontend, backend, devops, pm, qa
    status          VARCHAR(32) NOT NULL DEFAULT 'idle',  -- idle, busy, offline, error
    model           VARCHAR(128) NOT NULL DEFAULT 'deepseek-chat',
    system_prompt   TEXT,
    config          JSONB NOT NULL DEFAULT '{}',     -- flexible agent config
    skills          JSONB NOT NULL DEFAULT '[]',     -- array of skill names
    max_concurrent  INTEGER NOT NULL DEFAULT 1,
    total_tasks     BIGINT NOT NULL DEFAULT 0,
    success_tasks   BIGINT NOT NULL DEFAULT 0,
    avg_latency_ms  DOUBLE PRECISION DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agents_role ON agents(role);
CREATE INDEX idx_agents_status ON agents(status);
CREATE INDEX idx_agents_config ON agents USING GIN(config);

-- ============================================================
-- 2. Tasks Table - Work unit tracking
-- ============================================================
CREATE TABLE IF NOT EXISTS tasks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID REFERENCES agents(id) ON DELETE SET NULL,
    workflow_id     UUID,                            -- link to workflow runs
    title           VARCHAR(256) NOT NULL,
    description     TEXT,
    task_type       VARCHAR(64) NOT NULL,            -- code_gen, review, test, deploy, chat
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending, running, completed, failed, cancelled
    priority        INTEGER NOT NULL DEFAULT 5,      -- 1 (highest) to 10 (lowest)
    input_data      JSONB NOT NULL DEFAULT '{}',     -- task input parameters
    output_data     JSONB DEFAULT '{}',              -- task output/results
    error_message   TEXT,
    tokens_used     INTEGER DEFAULT 0,
    latency_ms      INTEGER DEFAULT 0,
    retry_count     INTEGER DEFAULT 0,
    max_retries     INTEGER DEFAULT 3,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tasks_agent ON tasks(agent_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_type ON tasks(task_type);
CREATE INDEX idx_tasks_workflow ON tasks(workflow_id);
CREATE INDEX idx_tasks_created ON tasks(created_at DESC);
CREATE INDEX idx_tasks_input ON tasks USING GIN(input_data);

-- ============================================================
-- 3. Knowledge Base - RAG vector storage
-- ============================================================
CREATE TABLE IF NOT EXISTS knowledge_base (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title           VARCHAR(256) NOT NULL,
    content         TEXT NOT NULL,
    content_type    VARCHAR(64) NOT NULL DEFAULT 'text',  -- text, code, doc, api_spec
    source          VARCHAR(512),                    -- file path, URL, etc.
    metadata        JSONB NOT NULL DEFAULT '{}',
    embedding       vector(1536),                    -- OpenAI/DeepSeek embedding dimension
    chunk_index     INTEGER DEFAULT 0,               -- position within source document
    chunk_total     INTEGER DEFAULT 1,
    language        VARCHAR(32) DEFAULT 'zh',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- IVFFlat index for approximate nearest neighbor search
-- Need to create after inserting some data: CREATE INDEX ON knowledge_base USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
CREATE INDEX idx_kb_content_type ON knowledge_base(content_type);
CREATE INDEX idx_kb_source ON knowledge_base(source);
CREATE INDEX idx_kb_metadata ON knowledge_base USING GIN(metadata);
CREATE INDEX idx_kb_content_trgm ON knowledge_base USING GIN(content gin_trgm_ops);

-- ============================================================
-- 4. Inference Metrics - Request-level monitoring
-- ============================================================
CREATE TABLE IF NOT EXISTS inference_metrics (
    id              BIGSERIAL PRIMARY KEY,
    request_id      UUID NOT NULL DEFAULT gen_random_uuid(),
    agent_id        UUID REFERENCES agents(id) ON DELETE SET NULL,
    model           VARCHAR(128) NOT NULL,
    prompt_tokens   INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    latency_ms      INTEGER NOT NULL DEFAULT 0,
    ttft_ms         INTEGER DEFAULT 0,               -- Time To First Token
    tps             DOUBLE PRECISION DEFAULT 0,      -- Tokens Per Second
    cache_hit       BOOLEAN DEFAULT FALSE,
    status          VARCHAR(32) NOT NULL DEFAULT 'success',  -- success, error, timeout
    error_code      VARCHAR(64),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_metrics_agent ON inference_metrics(agent_id);
CREATE INDEX idx_metrics_model ON inference_metrics(model);
CREATE INDEX idx_metrics_created ON inference_metrics(created_at DESC);
CREATE INDEX idx_metrics_status ON inference_metrics(status);

-- Partitioning hint: In production, partition by created_at monthly
-- CREATE TABLE inference_metrics_2024_01 PARTITION OF inference_metrics
--     FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

-- ============================================================
-- 5. Workflow Runs - DAG execution history
-- ============================================================
CREATE TABLE IF NOT EXISTS workflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_name   VARCHAR(256) NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'pending',  -- pending, running, completed, failed
    dag_definition  JSONB NOT NULL DEFAULT '{}',     -- DAG structure
    step_results    JSONB NOT NULL DEFAULT '[]',     -- execution results per step
    total_steps     INTEGER DEFAULT 0,
    completed_steps INTEGER DEFAULT 0,
    triggered_by    VARCHAR(128),                    -- user, schedule, webhook
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_wf_status ON workflow_runs(status);
CREATE INDEX idx_wf_name ON workflow_runs(workflow_name);
CREATE INDEX idx_wf_created ON workflow_runs(created_at DESC);

-- ============================================================
-- 6. Model Registry - Model version management
-- ============================================================
CREATE TABLE IF NOT EXISTS model_registry (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(128) NOT NULL,
    version         VARCHAR(64) NOT NULL,
    provider        VARCHAR(64) NOT NULL,            -- deepseek, local, huggingface
    model_type      VARCHAR(64) NOT NULL,            -- chat, code, embedding
    quantization    VARCHAR(32),                     -- fp16, int8, q4_k_m, awq, gptq
    size_gb         DOUBLE PRECISION,
    parameters      VARCHAR(32),                     -- 1.3B, 6.7B, 33B, 67B
    config          JSONB NOT NULL DEFAULT '{}',
    endpoint_url    VARCHAR(512),
    is_active       BOOLEAN DEFAULT TRUE,
    health_status   VARCHAR(32) DEFAULT 'unknown',
    last_health_check TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(name, version)
);

CREATE INDEX idx_model_active ON model_registry(is_active);
CREATE INDEX idx_model_type ON model_registry(model_type);

-- ============================================================
-- 7. Audit Log - Security and operations tracking
-- ============================================================
CREATE TABLE IF NOT EXISTS audit_log (
    id              BIGSERIAL PRIMARY KEY,
    user_id         VARCHAR(128),
    action          VARCHAR(64) NOT NULL,            -- create, update, delete, login, api_call
    resource_type   VARCHAR(64) NOT NULL,            -- agent, task, model, workflow
    resource_id     VARCHAR(128),
    details         JSONB DEFAULT '{}',
    ip_address      INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_user ON audit_log(user_id);
CREATE INDEX idx_audit_action ON audit_log(action);
CREATE INDEX idx_audit_resource ON audit_log(resource_type, resource_id);
CREATE INDEX idx_audit_created ON audit_log(created_at DESC);

-- ============================================================
-- 8. System Config - Dynamic configuration store
-- ============================================================
CREATE TABLE IF NOT EXISTS system_config (
    key             VARCHAR(256) PRIMARY KEY,
    value           JSONB NOT NULL,
    description     TEXT,
    updated_by      VARCHAR(128),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Helper Functions
-- ============================================================

-- Auto-update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tr_agents_updated_at
    BEFORE UPDATE ON agents FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER tr_tasks_updated_at
    BEFORE UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER tr_knowledge_base_updated_at
    BEFORE UPDATE ON knowledge_base FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER tr_model_registry_updated_at
    BEFORE UPDATE ON model_registry FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Vector similarity search function
CREATE OR REPLACE FUNCTION search_knowledge(
    query_embedding vector(1536),
    match_count INTEGER DEFAULT 5,
    filter_type VARCHAR DEFAULT NULL
)
RETURNS TABLE (
    id UUID,
    title VARCHAR,
    content TEXT,
    similarity DOUBLE PRECISION,
    metadata JSONB
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        kb.id,
        kb.title,
        kb.content,
        1 - (kb.embedding <=> query_embedding) AS similarity,
        kb.metadata
    FROM knowledge_base kb
    WHERE (filter_type IS NULL OR kb.content_type = filter_type)
    ORDER BY kb.embedding <=> query_embedding
    LIMIT match_count;
END;
$$ LANGUAGE plpgsql;

-- Aggregate inference metrics by time window
CREATE OR REPLACE FUNCTION get_inference_stats(
    window_hours INTEGER DEFAULT 24
)
RETURNS TABLE (
    total_requests BIGINT,
    success_count BIGINT,
    error_count BIGINT,
    avg_latency DOUBLE PRECISION,
    p95_latency DOUBLE PRECISION,
    total_tokens BIGINT,
    avg_tps DOUBLE PRECISION,
    cache_hit_rate DOUBLE PRECISION
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        COUNT(*)::BIGINT AS total_requests,
        COUNT(*) FILTER (WHERE im.status = 'success')::BIGINT AS success_count,
        COUNT(*) FILTER (WHERE im.status = 'error')::BIGINT AS error_count,
        AVG(im.latency_ms)::DOUBLE PRECISION AS avg_latency,
        PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY im.latency_ms)::DOUBLE PRECISION AS p95_latency,
        SUM(im.total_tokens)::BIGINT AS total_tokens,
        AVG(im.tps)::DOUBLE PRECISION AS avg_tps,
        (COUNT(*) FILTER (WHERE im.cache_hit))::DOUBLE PRECISION / GREATEST(COUNT(*), 1) AS cache_hit_rate
    FROM inference_metrics im
    WHERE im.created_at >= NOW() - (window_hours || ' hours')::INTERVAL;
END;
$$ LANGUAGE plpgsql;

-- ============================================================
-- Seed Data - Default agents
-- ============================================================
INSERT INTO agents (name, role, system_prompt, config, skills) VALUES
('Phoenix', 'pm', 'You are Phoenix, an AI project manager. Break down requirements, plan tasks, and coordinate team members.', '{"temperature": 0.7, "max_tokens": 2048}', '["requirement_analysis", "task_planning", "risk_assessment"]'),
('Atlas', 'backend', 'You are Atlas, a senior backend developer. Write clean, efficient Go code with proper error handling.', '{"temperature": 0.3, "max_tokens": 4096}', '["go", "api_design", "database", "microservices"]'),
('Pixel', 'frontend', 'You are Pixel, a creative frontend developer. Build beautiful, responsive UIs with modern web technologies.', '{"temperature": 0.5, "max_tokens": 4096}', '["react", "vue", "css", "ux_design"]'),
('Guardian', 'devops', 'You are Guardian, a DevOps engineer. Manage deployments, monitoring, and infrastructure.', '{"temperature": 0.2, "max_tokens": 2048}', '["docker", "kubernetes", "prometheus", "ci_cd"]'),
('Sentinel', 'qa', 'You are Sentinel, a QA engineer. Write comprehensive tests and ensure code quality.', '{"temperature": 0.3, "max_tokens": 4096}', '["testing", "code_review", "security_audit"]')
ON CONFLICT DO NOTHING;

-- Default model registry entries
INSERT INTO model_registry (name, version, provider, model_type, quantization, parameters, config, endpoint_url) VALUES
('deepseek-chat', 'v2', 'deepseek', 'chat', NULL, '67B', '{"api_base": "https://api.deepseek.com/v1"}', 'https://api.deepseek.com/v1/chat/completions'),
('deepseek-coder', 'v2', 'deepseek', 'code', NULL, '33B', '{"api_base": "https://api.deepseek.com/v1"}', 'https://api.deepseek.com/v1/chat/completions'),
('deepseek-coder-1.3b', 'v1', 'local', 'code', 'q4_k_m', '1.3B', '{"base_url": "http://localhost:11434"}', 'http://localhost:11434/v1/chat/completions'),
('deepseek-coder-6.7b', 'v1', 'local', 'code', 'awq', '6.7B', '{"base_url": "http://localhost:8000"}', 'http://localhost:8000/v1/chat/completions')
ON CONFLICT DO NOTHING;

-- Default system config
INSERT INTO system_config (key, value, description) VALUES
('inference.default_model', '"deepseek-chat"', 'Default model for inference'),
('inference.max_tokens', '4096', 'Maximum tokens per request'),
('inference.temperature', '0.7', 'Default temperature'),
('rate_limit.requests_per_minute', '100', 'Global rate limit'),
('rate_limit.tokens_per_minute', '100000', 'Token rate limit'),
('monitoring.metrics_retention_days', '30', 'Days to retain metrics data')
ON CONFLICT (key) DO NOTHING;
