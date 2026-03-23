-- ============================================================
-- AI Corp - Memory System Schema Extension
-- 自我迭代与经验共享系统数据库扩展
-- ============================================================

-- ============================================================
-- 1. Agent Memory Table - 记忆块存储
-- ============================================================
CREATE TABLE IF NOT EXISTS agent_memory (
    id              VARCHAR(128) PRIMARY KEY,
    agent_id        VARCHAR(128) NOT NULL,
    type            VARCHAR(32) NOT NULL,           -- short_term, long_term, reflection, skill, shared
    title           VARCHAR(256) NOT NULL,
    content         TEXT NOT NULL,
    importance      DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    access_count    INTEGER NOT NULL DEFAULT 0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    embedding       vector(1536),                   -- 可选的向量嵌入
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_access     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ                     -- 过期时间（用于短期记忆）
);

CREATE INDEX IF NOT EXISTS idx_memory_agent ON agent_memory(agent_id);
CREATE INDEX IF NOT EXISTS idx_memory_type ON agent_memory(type);
CREATE INDEX IF NOT EXISTS idx_memory_importance ON agent_memory(importance DESC);
CREATE INDEX IF NOT EXISTS idx_memory_expires ON agent_memory(expires_at) WHERE expires_at IS NOT NULL;

-- IVFFlat index for vector similarity search (create after data insertion)
-- CREATE INDEX IF NOT EXISTS idx_memory_embedding ON agent_memory USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- ============================================================
-- 2. Agent Experiences Table - 经验记录
-- ============================================================
CREATE TABLE IF NOT EXISTS agent_experiences (
    id              VARCHAR(128) PRIMARY KEY,
    agent_id        VARCHAR(128) NOT NULL,
    task_id         VARCHAR(128) NOT NULL,
    task_type       VARCHAR(64) NOT NULL,
    input           TEXT NOT NULL,
    output          TEXT,
    success         BOOLEAN NOT NULL DEFAULT false,
    lessons         JSONB NOT NULL DEFAULT '[]',    -- 学到的教训
    patterns        JSONB NOT NULL DEFAULT '[]',    -- 识别的模式
    suggestions     JSONB NOT NULL DEFAULT '[]',    -- 改进建议
    confidence      DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_exp_agent ON agent_experiences(agent_id);
CREATE INDEX IF NOT EXISTS idx_exp_task ON agent_experiences(task_id);
CREATE INDEX IF NOT EXISTS idx_exp_type ON agent_experiences(task_type);
CREATE INDEX IF NOT EXISTS idx_exp_success ON agent_experiences(success);
CREATE INDEX IF NOT EXISTS idx_exp_created ON agent_experiences(created_at DESC);

-- ============================================================
-- 3. Agent Reflections Table - 反思记录
-- ============================================================
CREATE TABLE IF NOT EXISTS agent_reflections (
    id              VARCHAR(128) PRIMARY KEY,
    agent_id        VARCHAR(128) NOT NULL,
    trigger_task_id VARCHAR(128) NOT NULL,
    trigger_type    VARCHAR(32) NOT NULL,           -- success, failure, timeout
    analysis        TEXT NOT NULL,
    insights        JSONB NOT NULL DEFAULT '[]',    -- 洞察
    action_items    JSONB NOT NULL DEFAULT '[]',    -- 行动项
    skill_learned   JSONB,                          -- 学到的新技能
    confidence      DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ref_agent ON agent_reflections(agent_id);
CREATE INDEX IF NOT EXISTS idx_ref_trigger_task ON agent_reflections(trigger_task_id);
CREATE INDEX IF NOT EXISTS idx_ref_trigger_type ON agent_reflections(trigger_type);
CREATE INDEX IF NOT EXISTS idx_ref_created ON agent_reflections(created_at DESC);

-- ============================================================
-- 4. Agent Skills Table - 技能定义
-- ============================================================
CREATE TABLE IF NOT EXISTS agent_skills (
    id              VARCHAR(128) PRIMARY KEY,
    name            VARCHAR(128) NOT NULL UNIQUE,
    description     TEXT NOT NULL,
    category        VARCHAR(64) NOT NULL,           -- coding, analysis, communication, etc.
    template        TEXT,                           -- 执行模板或代码片段
    parameters      JSONB NOT NULL DEFAULT '{}',    -- 参数定义
    examples        JSONB NOT NULL DEFAULT '[]',    -- 使用示例
    success_rate    DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    use_count       INTEGER NOT NULL DEFAULT 0,
    source          VARCHAR(32) NOT NULL DEFAULT 'predefined', -- learned, predefined
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skills_category ON agent_skills(category);
CREATE INDEX IF NOT EXISTS idx_skills_source ON agent_skills(source);
CREATE INDEX IF NOT EXISTS idx_skills_success ON agent_skills(success_rate DESC);

-- ============================================================
-- 5. Sandbox Executions Table - 沙箱执行记录
-- ============================================================
CREATE TABLE IF NOT EXISTS sandbox_executions (
    id              VARCHAR(128) PRIMARY KEY,
    task_id         VARCHAR(128) NOT NULL,
    container_id    VARCHAR(128),
    image           VARCHAR(256) NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'creating', -- creating, running, completed, failed, timeout
    command         TEXT,
    output          TEXT,
    exit_code       INTEGER,
    memory_mb       INTEGER NOT NULL DEFAULT 512,
    cpu_quota       INTEGER NOT NULL DEFAULT 50000,
    timeout_seconds INTEGER NOT NULL DEFAULT 300,
    network_enabled BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    error_message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_sandbox_task ON sandbox_executions(task_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_status ON sandbox_executions(status);
CREATE INDEX IF NOT EXISTS idx_sandbox_created ON sandbox_executions(created_at DESC);

-- ============================================================
-- Helper Functions for Memory System
-- ============================================================

-- 获取 Agent 的记忆摘要
CREATE OR REPLACE FUNCTION get_agent_memory_summary(agent_uuid VARCHAR)
RETURNS TABLE (
    memory_type VARCHAR,
    count BIGINT,
    avg_importance DOUBLE PRECISION,
    total_access BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        am.type::VARCHAR,
        COUNT(*)::BIGINT,
        AVG(am.importance)::DOUBLE PRECISION,
        SUM(am.access_count)::BIGINT
    FROM agent_memory am
    WHERE am.agent_id = agent_uuid
    GROUP BY am.type;
END;
$$ LANGUAGE plpgsql;

-- 获取最近的经验教训
CREATE OR REPLACE FUNCTION get_recent_lessons(agent_uuid VARCHAR, limit_count INTEGER DEFAULT 10)
RETURNS TABLE (
    lesson TEXT,
    task_type VARCHAR,
    success BOOLEAN,
    created_at TIMESTAMPTZ
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        jsonb_array_elements_text(ae.lessons) AS lesson,
        ae.task_type,
        ae.success,
        ae.created_at
    FROM agent_experiences ae
    WHERE ae.agent_id = agent_uuid
    ORDER BY ae.created_at DESC
    LIMIT limit_count;
END;
$$ LANGUAGE plpgsql;

-- 获取最常用的技能
CREATE OR REPLACE FUNCTION get_top_skills(category_filter VARCHAR DEFAULT NULL, limit_count INTEGER DEFAULT 10)
RETURNS TABLE (
    name VARCHAR,
    category VARCHAR,
    success_rate DOUBLE PRECISION,
    use_count INTEGER,
    source VARCHAR
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        s.name::VARCHAR,
        s.category::VARCHAR,
        s.success_rate,
        s.use_count,
        s.source::VARCHAR
    FROM agent_skills s
    WHERE (category_filter IS NULL OR s.category = category_filter)
    ORDER BY s.use_count DESC, s.success_rate DESC
    LIMIT limit_count;
END;
$$ LANGUAGE plpgsql;

-- 清理过期的短期记忆
CREATE OR REPLACE FUNCTION cleanup_expired_memories()
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM agent_memory
    WHERE expires_at IS NOT NULL AND expires_at < NOW();

    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- 统计 Agent 的自我改进指标
CREATE OR REPLACE FUNCTION get_self_improvement_stats(agent_uuid VARCHAR)
RETURNS TABLE (
    total_experiences BIGINT,
    successful_experiences BIGINT,
    total_reflections BIGINT,
    skills_learned BIGINT,
    avg_confidence DOUBLE PRECISION,
    insights_count BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        (SELECT COUNT(*) FROM agent_experiences WHERE agent_id = agent_uuid)::BIGINT,
        (SELECT COUNT(*) FROM agent_experiences WHERE agent_id = agent_uuid AND success = true)::BIGINT,
        (SELECT COUNT(*) FROM agent_reflections WHERE agent_id = agent_uuid)::BIGINT,
        (SELECT COUNT(*) FROM agent_reflections WHERE agent_id = agent_uuid AND skill_learned IS NOT NULL)::BIGINT,
        COALESCE((SELECT AVG(confidence) FROM agent_experiences WHERE agent_id = agent_uuid), 0)::DOUBLE PRECISION,
        (SELECT COUNT(*) FROM agent_reflections, jsonb_array_elements(insights) WHERE agent_id = agent_uuid)::BIGINT;
END;
$$ LANGUAGE plpgsql;

-- ============================================================
-- Seed Data - Predefined Skills
-- ============================================================
INSERT INTO agent_skills (id, name, description, category, template, parameters, source) VALUES
('skill-001', 'code_review', 'Review code for bugs, style issues, and improvements', 'coding',
 'Review the following code:\n{{code}}\n\nFocus on: {{focus_areas}}',
 '{"code": {"type": "string", "required": true}, "focus_areas": {"type": "array", "default": ["bugs", "style", "performance"]}}',
 'predefined'),

('skill-002', 'test_generation', 'Generate unit tests for given code', 'testing',
 'Generate {{test_framework}} tests for:\n{{code}}\n\nCover: {{coverage_targets}}',
 '{"code": {"type": "string", "required": true}, "test_framework": {"type": "string", "default": "go test"}, "coverage_targets": {"type": "array", "default": ["edge_cases", "error_paths"]}}',
 'predefined'),

('skill-003', 'api_design', 'Design RESTful API endpoints', 'architecture',
 'Design API for: {{description}}\n\nRequirements:\n- Authentication: {{auth_type}}\n- Rate limiting: {{rate_limit}}',
 '{"description": {"type": "string", "required": true}, "auth_type": {"type": "string", "default": "JWT"}, "rate_limit": {"type": "string", "default": "100/hour"}}',
 'predefined'),

('skill-004', 'error_analysis', 'Analyze error logs and suggest fixes', 'debugging',
 'Analyze these error logs:\n{{logs}}\n\nContext: {{context}}',
 '{"logs": {"type": "string", "required": true}, "context": {"type": "string", "required": false}}',
 'predefined'),

('skill-005', 'documentation', 'Generate documentation for code or APIs', 'documentation',
 'Document: {{subject}}\n\nFormat: {{format}}\nAudience: {{audience}}',
 '{"subject": {"type": "string", "required": true}, "format": {"type": "string", "default": "markdown"}, "audience": {"type": "string", "default": "developers"}}',
 'predefined')
ON CONFLICT (name) DO NOTHING;

-- ============================================================
-- Triggers
-- ============================================================

-- Auto-update updated_at for skills
CREATE TRIGGER tr_skills_updated_at
    BEFORE UPDATE ON agent_skills FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();
