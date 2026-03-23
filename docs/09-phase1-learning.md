# Phase 1 学习文档：数据库 + 推理服务 + 可观测性

> AI Corp 项目 Phase 1 实现详解，涵盖 PostgreSQL + pgvector、Go 数据库层、
> DeepSeek API 集成、Prometheus 指标体系。每节均包含原理说明、代码解析和实操命令。

---

## 目录

1. [PostgreSQL 16 源码编译与 pgvector 扩展](#1-postgresql-16-源码编译与-pgvector-扩展)
2. [数据库 Schema 设计哲学](#2-数据库-schema-设计哲学)
3. [Go 数据库层：pgx 驱动深度解析](#3-go-数据库层pgx-驱动深度解析)
4. [向量检索与 RAG 知识库](#4-向量检索与-rag-知识库)
5. [DeepSeek API 集成与推理服务](#5-deepseek-api-集成与推理服务)
6. [Prometheus 指标体系设计](#6-prometheus-指标体系设计)
7. [Grafana 可视化监控](#7-grafana-可视化监控)
8. [系统架构总览](#8-系统架构总览)

---

## 1. PostgreSQL 16 源码编译与 pgvector 扩展

### 1.1 为什么选择 PostgreSQL

在 AI 时代，PostgreSQL 相比 MySQL 有几个决定性优势：

| 特性 | PostgreSQL | MySQL |
|------|-----------|-------|
| 向量搜索 | pgvector 原生支持 | 无原生支持 |
| JSONB | 二进制存储 + GIN 索引 | JSON 字符串存储 |
| CTE/Window 函数 | 完整支持 | 8.0 后部分支持 |
| 自定义类型 | 支持 composite/enum/range | 不支持 |
| 扩展机制 | Extension API | 无 |
| 并发控制 | MVCC (无锁读) | InnoDB MVCC |

**核心结论**：pgvector 让 PostgreSQL 成为 AI 应用的"全能数据库"——关系数据 + 向量搜索 + JSON 文档，一个库搞定。

### 1.2 源码编译流程

在 CentOS 7 等旧版系统中，PGDG 仓库可能不提供最新版本，需要从源码编译：

```bash
# 1. 安装编译依赖
yum install -y gcc make readline-devel zlib-devel openssl-devel

# 2. 下载 PostgreSQL 16.6 源码
curl -L -o postgresql-16.6.tar.bz2 \
  "https://ftp.postgresql.org/pub/source/v16.6/postgresql-16.6.tar.bz2"
tar xjf postgresql-16.6.tar.bz2

# 3. 配置 + 编译 + 安装
cd postgresql-16.6
./configure --prefix=/usr/local/pgsql16 --with-openssl
make -j$(nproc)
make install

# 4. 编译 contrib 模块（pg_trgm 等）
cd contrib && make -j$(nproc) && make install
```

**关键知识点**：
- `--prefix` 指定安装目录，避免与系统自带的旧版本冲突
- `--with-openssl` 启用 SSL 连接，生产环境必需
- contrib 模块包含 pg_trgm（模糊搜索）、pg_stat_statements（SQL 性能分析）等实用扩展

### 1.3 pgvector 编译安装

pgvector 是 PostgreSQL 的向量数据类型扩展，支持精确和近似最近邻搜索：

```bash
# 下载 pgvector 0.8.0
curl -L -o pgvector-0.8.0.tar.gz \
  "https://github.com/pgvector/pgvector/archive/refs/tags/v0.8.0.tar.gz"
tar xzf pgvector-0.8.0.tar.gz

# 编译安装（指向我们自编译的 PG）
cd pgvector-0.8.0
PG_CONFIG=/usr/local/pgsql16/bin/pg_config make
PG_CONFIG=/usr/local/pgsql16/bin/pg_config make install
```

**pgvector 核心概念**：

```
向量 (Vector)
  ↓
存储在 PostgreSQL 列中，类型为 vector(N)
  ↓
索引方式：
  ├── IVFFlat: 倒排文件索引，适合中等数据集（<100万）
  │     └── 原理：将向量空间划分为 K 个 Voronoi cell，查询时只搜索最近的几个 cell
  │     └── 参数：lists（分区数），probes（搜索分区数）
  └── HNSW: 层次可导航小世界图，适合大数据集
        └── 原理：构建多层跳跃图，从粗粒度层开始搜索逐步细化
        └── 参数：m（每层连接数），ef_construction（构建时搜索宽度）
```

**距离函数**：
- `<->` L2 距离（欧几里得）
- `<=>` 余弦距离（语义相似度首选）
- `<#>` 内积距离

### 1.4 数据库初始化

```bash
# 创建用户和数据目录
useradd -r -s /bin/bash -d /var/lib/pgsql postgres
mkdir -p /var/lib/pgsql/data && chown -R postgres:postgres /var/lib/pgsql

# 初始化数据库集群
su - postgres -c "/usr/local/pgsql16/bin/initdb -D /var/lib/pgsql/data -E UTF8"

# 启动
su - postgres -c "/usr/local/pgsql16/bin/pg_ctl -D /var/lib/pgsql/data -l logfile start"

# 创建项目数据库 + 启用扩展
su - postgres -c "/usr/local/pgsql16/bin/createdb aicorp"
su - postgres -c "/usr/local/pgsql16/bin/psql -d aicorp -c 'CREATE EXTENSION vector;'"
su - postgres -c "/usr/local/pgsql16/bin/psql -d aicorp -c 'CREATE EXTENSION pg_trgm;'"
```

---

## 2. 数据库 Schema 设计哲学

### 2.1 表结构总览

```
┌─────────────────────────────────────────────────────────┐
│                    AI Corp Schema                        │
├─────────────┬──────────────┬────────────────────────────┤
│   agents    │    tasks     │    knowledge_base          │
│  (5 agents) │ (工作单元)    │  (RAG 向量存储)            │
│             │  ↑ agent_id  │  embedding vector(1536)    │
├─────────────┼──────────────┼────────────────────────────┤
│ inference   │ workflow     │    model_registry          │
│ _metrics    │ _runs        │  (模型版本管理)             │
│ (请求级)    │ (DAG 执行)    │                            │
├─────────────┴──────────────┴────────────────────────────┤
│  audit_log (审计日志)  │  system_config (动态配置)       │
└─────────────────────────────────────────────────────────┘
```

### 2.2 设计原则

**1. UUID 主键**

```sql
id UUID PRIMARY KEY DEFAULT gen_random_uuid()
```

- `gen_random_uuid()` 是 PG13+ 内置函数，不需要 uuid-ossp 扩展
- UUID 在分布式环境中天然无冲突，方便未来分库分表
- 相比自增 ID，不泄露业务量信息

**2. JSONB 灵活字段**

```sql
config  JSONB NOT NULL DEFAULT '{}'   -- Agent 配置
skills  JSONB NOT NULL DEFAULT '[]'   -- 技能列表
```

JSONB vs JSON 的区别：
- JSONB 是二进制存储，解析一次后以二进制树形式保存
- 支持 GIN 索引，可以对 JSONB 内部字段建索引
- 查询速度比 JSON 类型快 5-10 倍

```sql
-- 示例：查询所有会 Go 的 Agent
SELECT * FROM agents WHERE skills @> '["go"]';

-- GIN 索引让上面的查询走索引扫描
CREATE INDEX idx_agents_config ON agents USING GIN(config);
```

**3. 时间戳与自动更新**

```sql
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

使用触发器自动维护 `updated_at`：

```sql
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tr_agents_updated_at
    BEFORE UPDATE ON agents FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();
```

**4. 向量列设计**

```sql
embedding vector(1536)  -- 匹配 OpenAI/DeepSeek embedding 维度
```

1536 是 OpenAI text-embedding-ada-002 的维度。如果用其他 embedding 模型：
- OpenAI text-embedding-3-small: 1536
- DeepSeek embedding: 1024
- Nomic embed text: 768
- BGE-M3: 1024

### 2.3 聚合函数：推理统计

```sql
CREATE OR REPLACE FUNCTION get_inference_stats(window_hours INTEGER DEFAULT 24)
RETURNS TABLE (...) AS $$
BEGIN
    RETURN QUERY
    SELECT
        COUNT(*)::BIGINT,
        COUNT(*) FILTER (WHERE im.status = 'success')::BIGINT,
        AVG(im.latency_ms)::DOUBLE PRECISION,
        -- P95 延迟：Prometheus 的 histogram_quantile 在数据库层的等价
        PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY im.latency_ms)::DOUBLE PRECISION,
        ...
    FROM inference_metrics im
    WHERE im.created_at >= NOW() - (window_hours || ' hours')::INTERVAL;
END;
$$ LANGUAGE plpgsql;
```

**知识点**：
- `FILTER (WHERE ...)` 是 PostgreSQL 的条件聚合，比 `CASE WHEN` 更简洁高效
- `PERCENTILE_CONT` 计算连续百分位，是监控系统中 P95/P99 延迟的标准方法
- 使用 INTERVAL 类型做时间窗口，PostgreSQL 原生支持

---

## 3. Go 数据库层：pgx 驱动深度解析

### 3.1 pgx vs database/sql

| 特性 | pgx | database/sql |
|------|-----|-------------|
| 性能 | 原生协议，零拷贝 | 通过驱动接口，有开销 |
| 连接池 | pgxpool 内置 | 需要额外配置 |
| PostgreSQL 特性 | 完整支持 (LISTEN/NOTIFY, COPY) | 仅标准 SQL |
| 类型映射 | 原生支持 JSONB/UUID/Array | 需要手动转换 |
| 批量操作 | pgx.Batch | 不支持 |

**选择 pgx 的原因**：直接使用 PostgreSQL 原生协议，性能比 database/sql + pq 驱动高 30-50%。

### 3.2 连接池配置

```go
// db.go 核心代码解析
func New(ctx context.Context, cfg Config) (*DB, error) {
    dsn := fmt.Sprintf(
        "postgres://%s:%s@%s:%d/%s?sslmode=%s",
        cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.SSLMode,
    )

    poolCfg, err := pgxpool.ParseConfig(dsn)
    // ...

    // 连接池参数
    poolCfg.MaxConns = 20          // 最大连接数
    poolCfg.MinConns = 2           // 最小空闲连接
    poolCfg.MaxConnLifetime = 30 * time.Minute  // 连接最大存活时间
    poolCfg.MaxConnIdleTime = 5 * time.Minute   // 空闲连接超时

    pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
    // ...
}
```

**连接池参数调优指南**：

```
MaxConns = CPU核数 * 2 + 有效磁盘数

原因：PostgreSQL 连接会独占一个后端进程，过多连接反而导致上下文切换开销。
例如：4核 CPU + 1块 SSD → MaxConns = 4*2+1 = 9（开发环境用 20 足够）

MaxConnLifetime = 30min
原因：避免使用已被防火墙/负载均衡器断开的"幽灵连接"

MinConns = 2
原因：保持少量预热连接，避免突发请求时的连接建立延迟
```

### 3.3 Repository 模式

我们使用 Repository 模式组织数据访问代码：

```
DB struct
  ├── Pool     *pgxpool.Pool    // 连接池（共享）
  ├── Agents   *AgentRepo       // Agent CRUD
  ├── Tasks    *TaskRepo        // 任务 CRUD
  ├── KB       *KnowledgeBaseRepo  // 知识库 + 向量搜索
  ├── Metrics  *MetricsRepo     // 推理指标
  ├── Models   *ModelRegistryRepo  // 模型注册表
  └── Audit    *AuditRepo       // 审计日志
```

每个 Repo 持有同一个 Pool 的引用，由 Pool 管理连接复用：

```go
type AgentRepo struct {
    pool *pgxpool.Pool  // 所有 Repo 共享同一个 Pool
}

func (r *AgentRepo) List(ctx context.Context, role string) ([]Agent, error) {
    query := `SELECT id, name, role, ... FROM agents`
    args := []interface{}{}

    if role != "" {
        query += " WHERE role = $1"
        args = append(args, role)
    }

    rows, err := r.pool.Query(ctx, query, args...)
    // ... scan rows
}
```

**重点**：使用 `$1, $2...` 参数占位符（不是 `?`），这是 PostgreSQL 的原生语法。pgx 会使用 Prepared Statement 协议，让数据库缓存执行计划。

### 3.4 JSONB 序列化

```go
func (r *AgentRepo) Create(ctx context.Context, a *Agent) (string, error) {
    configJSON, _ := json.Marshal(a.Config)   // map → []byte
    skillsJSON, _ := json.Marshal(a.Skills)   // []string → []byte

    var id string
    err := r.pool.QueryRow(ctx,
        `INSERT INTO agents (name, role, config, skills, ...)
         VALUES ($1, $2, $3, $4, ...) RETURNING id`,
        a.Name, a.Role, configJSON, skillsJSON, ...,
    ).Scan(&id)
    // ...
}
```

pgx 会自动将 `[]byte` 作为 JSONB 类型传递给 PostgreSQL。反序列化时也是先读取 `[]byte`，再 `json.Unmarshal`。

---

## 4. 向量检索与 RAG 知识库

### 4.1 RAG 架构

```
用户提问 → Embedding Model → 查询向量
                                 ↓
                          pgvector 余弦检索
                                 ↓
                          Top-K 相关文档
                                 ↓
                    拼接到 System Prompt 中
                                 ↓
                         LLM 生成回答
```

### 4.2 向量写入

```go
func (r *KnowledgeBaseRepo) Insert(ctx context.Context, entry *KnowledgeEntry) (string, error) {
    var embedding *pgvector.Vector
    if len(entry.Embedding) > 0 {
        v := pgvector.NewVector(entry.Embedding)  // []float32 → pgvector.Vector
        embedding = &v
    }

    var id string
    err := r.pool.QueryRow(ctx,
        `INSERT INTO knowledge_base (title, content, ..., embedding)
         VALUES ($1, $2, ..., $6) RETURNING id`,
        entry.Title, entry.Content, ..., embedding,
    ).Scan(&id)
    // ...
}
```

`pgvector.NewVector` 将 Go 的 `[]float32` 转换为 PostgreSQL 的 `vector` 类型。

### 4.3 相似度检索

```go
func (r *KnowledgeBaseRepo) SearchSimilar(ctx context.Context,
    queryEmbedding []float32, limit int, contentType string) ([]KnowledgeEntry, error) {

    qv := pgvector.NewVector(queryEmbedding)

    query := `SELECT id, title, content, content_type, source, metadata,
              1 - (embedding <=> $1::vector) AS similarity
              FROM knowledge_base WHERE embedding IS NOT NULL`
    // ...
    query += ` ORDER BY embedding <=> $1::vector LIMIT $N`

    rows, err := r.pool.Query(ctx, query, args...)
    // ...
}
```

**关键运算符**：`<=>` 计算余弦距离（0~2），`1 - 距离 = 相似度`。
ORDER BY 距离升序 = 相似度降序。

**索引优化**：数据量超过 10000 条后应创建 IVFFlat 或 HNSW 索引：

```sql
-- IVFFlat（推荐数据量 < 100万）
CREATE INDEX ON knowledge_base
  USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- HNSW（推荐数据量 > 100万）
CREATE INDEX ON knowledge_base
  USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
```

### 4.4 全文搜索补充

pgvector 的向量搜索适合语义相似，但有时需要关键词精确匹配。pg_trgm 扩展提供模糊匹配：

```sql
-- 三元组(trigram)相似度搜索
SELECT *, similarity(content, '容器编排') AS sim
FROM knowledge_base
WHERE content % '容器编排'   -- % 操作符：相似度 > 阈值
ORDER BY sim DESC LIMIT 10;
```

**混合检索策略**：先用向量搜索找语义相关，再用 trgm 搜索做关键词补充，合并去重取 Top-K。

---

## 5. DeepSeek API 集成与推理服务

### 5.1 架构分层

```
HTTP Request → Orchestrator → InferenceService → LLM Client → DeepSeek API
                                    ↓
                              Prometheus 指标
                                    ↓
                              PostgreSQL 指标表
```

### 5.2 InferenceService 设计

`InferenceService` 是 LLM 调用的中间层，职责：
1. 封装 LLM API 调用
2. 自动记录推理指标到 PostgreSQL
3. 自动更新 Prometheus 计数器
4. 计算 TPS（Tokens Per Second）

```go
type InferenceService struct {
    client *Client        // LLM HTTP 客户端
    db     *database.DB   // 数据库（可选，nil 则跳过记录）
}

func (s *InferenceService) Chat(ctx context.Context, messages []Message, agentID *string) (*ChatResult, error) {
    start := time.Now()

    // 1. 调用 DeepSeek API
    resp, err := s.client.ChatFull(ctx, req)
    latencyMs := int(time.Since(start).Milliseconds())

    // 2. 计算 TPS
    tps := float64(resp.Usage.CompletionTokens) / (float64(latencyMs) / 1000.0)

    // 3. 异步写入 PostgreSQL（不阻塞响应）
    go func() {
        _ = s.db.Metrics.Record(ctx, &database.InferenceMetric{...})
    }()

    // 4. 同步更新 Prometheus 指标
    metrics.RecordInference(model, provider, "success",
        float64(latencyMs)/1000.0, 0, tps, promptTokens, completionTokens, false)

    return result, nil
}
```

**设计决策**：
- DB 写入用 `go func()` 异步：避免数据库慢查询阻塞 API 响应
- Prometheus 更新用同步：promauto 的 Counter/Histogram 是原子操作，纳秒级

### 5.3 DeepSeek API 调用细节

```go
func (c *Client) ChatFull(ctx context.Context, req Request) (*Response, error) {
    reqBody, _ := json.Marshal(req)

    httpReq, _ := http.NewRequestWithContext(ctx, "POST",
        c.config.BaseURL+"/chat/completions",
        bytes.NewBuffer(reqBody))

    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

    resp, err := c.client.Do(httpReq)
    // ...
    var llmResp Response
    json.Unmarshal(body, &llmResp)
    return &llmResp, nil
}
```

**Response 结构**（OpenAI 兼容格式）：

```json
{
  "id": "chatcmpl-xxx",
  "model": "deepseek-chat",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "..."},
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 150,
    "completion_tokens": 200,
    "total_tokens": 350
  }
}
```

DeepSeek 的 API 完全兼容 OpenAI 格式，所以同一个 Client 可以切换到 OpenAI/Claude 等提供商。

### 5.4 API Key 安全管理

```yaml
# config.yaml 中用环境变量引用
llm:
  api_key: "${DEEPSEEK_API_KEY}"
```

```go
// main.go 中 os.ExpandEnv 展开
content := os.ExpandEnv(string(data))
```

`.env` 文件存储真实 key，加入 `.gitignore` 避免泄露。

---

## 6. Prometheus 指标体系设计

### 6.1 四种指标类型

```
Counter   (计数器)  → 只增不减。例：总请求数、总 Token 数
Gauge     (仪表盘)  → 可增可减。例：当前连接数、Goroutine 数
Histogram (直方图)  → 观测值分桶。例：延迟分布，可计算 P50/P95/P99
Summary   (摘要)    → 客户端计算百分位。例：GC 暂停时间
```

### 6.2 我们的指标体系

```go
// 命名规范：{namespace}_{subsystem}_{name}_{unit}
// 例如：aicorp_inference_latency_seconds

// === 推理层 ===
aicorp_inference_requests_total{model, provider, status}   // Counter
aicorp_inference_latency_seconds{model, provider}          // Histogram
aicorp_inference_tokens_total{model, direction}             // Counter
aicorp_inference_tokens_per_second{model}                   // Gauge
aicorp_inference_cache_hits_total{model}                    // Counter
aicorp_inference_cache_misses_total{model}                  // Counter
aicorp_inference_ttft_seconds{model}                        // Histogram (Time To First Token)

// === Agent 层 ===
aicorp_agent_tasks_total{agent_role, status}               // Counter
aicorp_agent_task_latency_seconds{agent_role, task_type}   // Histogram
aicorp_agent_active{role}                                   // Gauge

// === 数据库层 ===
aicorp_db_connections_active                                // Gauge
aicorp_db_connections_idle                                  // Gauge
aicorp_db_query_latency_seconds{operation}                 // Histogram

// === HTTP 层 ===
aicorp_http_requests_total{method, path, status}           // Counter
aicorp_http_request_duration_seconds{method, path}         // Histogram

// === 系统层 (gopsutil) ===
aicorp_system_cpu_usage_percent                             // Gauge (0-100)
aicorp_system_memory_used_bytes                             // Gauge
aicorp_system_memory_total_bytes                            // Gauge
aicorp_system_memory_usage_percent                          // Gauge (0-100)
aicorp_system_network_bytes_in_total                        // Gauge (累计)
aicorp_system_network_bytes_out_total                       // Gauge (累计)
aicorp_system_go_goroutines                                 // Gauge
aicorp_system_go_memory_alloc_bytes                         // Gauge
```

### 6.3 系统监控实现原理

使用 `gopsutil` 库跨平台采集系统指标：

```go
import (
    "github.com/shirou/gopsutil/v3/cpu"
    "github.com/shirou/gopsutil/v3/mem"
    "github.com/shirou/gopsutil/v3/net"
)

// CPU 使用率 - 需要采样间隔
cpuPct, _ := cpu.Percent(1*time.Second, false)
// 返回 []float64，每个元素是一个核心的使用率
// false = 总体使用率，true = 每核使用率

// 内存信息
vm, _ := mem.VirtualMemory()
// vm.Used       - 已用字节数
// vm.Total      - 总字节数
// vm.UsedPercent - 使用百分比
// vm.Available  - 可用字节数

// 网络 I/O（累计值，需要用 rate() 计算速率）
netIO, _ := net.IOCounters(false)
// netIO[0].BytesRecv - 接收字节数
// netIO[0].BytesSent - 发送字节数
```

**采集周期选择**：
- CPU 需要采样间隔（1秒），所以采集周期至少 1 秒
- 内存和网络可以即时读取
- 推荐采集周期：5 秒（平衡精度与开销）

### 6.4 Histogram Bucket 设计

```go
InferenceLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
    Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
}, []string{"model", "provider"})
```

**Bucket 选择逻辑**：
- LLM 推理的延迟范围通常在 0.5s ~ 30s
- 选择对数分布的 bucket 边界
- Prometheus 会自动创建 `_bucket`、`_sum`、`_count` 三个子指标
- 通过 `histogram_quantile(0.95, rate(...[5m]))` 计算 P95

### 6.4 promauto 自动注册

```go
import "github.com/prometheus/client_golang/prometheus/promauto"

var InferenceRequestsTotal = promauto.NewCounterVec(...)
```

`promauto` 自动将指标注册到默认 Registry，`promhttp.Handler()` 会暴露所有已注册指标。
不需要手动 `prometheus.MustRegister()`。

### 6.5 /metrics 端点

访问 `http://localhost:8080/metrics` 返回标准 Prometheus 文本格式：

```
# HELP aicorp_inference_requests_total Total number of LLM inference requests
# TYPE aicorp_inference_requests_total counter
aicorp_inference_requests_total{model="deepseek-chat",provider="deepseek",status="success"} 42

# HELP aicorp_inference_latency_seconds Inference request latency in seconds
# TYPE aicorp_inference_latency_seconds histogram
aicorp_inference_latency_seconds_bucket{model="deepseek-chat",provider="deepseek",le="0.5"} 5
aicorp_inference_latency_seconds_bucket{model="deepseek-chat",provider="deepseek",le="1"} 20
aicorp_inference_latency_seconds_bucket{model="deepseek-chat",provider="deepseek",le="+Inf"} 42
aicorp_inference_latency_seconds_sum{...} 63.5
aicorp_inference_latency_seconds_count{...} 42
```

---

## 7. Grafana 可视化监控

### 7.1 Dashboard 面板设计

我们的 Dashboard 包含 12 个面板，分为 4 行：

```
第 1 行：推理核心
  ┌──────────────┬───────────────────┬──────────────────┐
  │ Inference QPS │ Latency P50/P95/99 │ Tokens/sec Gauge │
  └──────────────┴───────────────────┴──────────────────┘

第 2 行：推理详情
  ┌────────────────────┬──────────────┬──────────────┐
  │ Token 吞吐量 (bar) │ Cache Hit %  │  Error Rate  │
  └────────────────────┴──────────────┴──────────────┘

第 3 行：Agent & DB
  ┌────────────────────┬──────────────┬──────────────┐
  │ Agent Task Rate    │  DB 连接数    │  DB 查询延迟  │
  └────────────────────┴──────────────┴──────────────┘

第 4 行：系统
  ┌──────────────┬──────────────┬──────────────────┐
  │ Goroutines   │ Memory       │ HTTP Request Rate │
  └──────────────┴──────────────┴──────────────────┘
```

### 7.2 关键 PromQL 查询

```promql
# QPS（每秒请求数）
rate(aicorp_inference_requests_total[5m])

# P95 延迟
histogram_quantile(0.95, rate(aicorp_inference_latency_seconds_bucket[5m]))

# 错误率
rate(aicorp_inference_requests_total{status="error"}[5m])
  / rate(aicorp_inference_requests_total[5m])

# 缓存命中率
rate(aicorp_inference_cache_hits_total[5m])
  / (rate(aicorp_inference_cache_hits_total[5m])
   + rate(aicorp_inference_cache_misses_total[5m]))

# Token 生成速率 (tokens/sec)
rate(aicorp_inference_tokens_total{direction="completion"}[5m])

# CPU 使用率
aicorp_system_cpu_usage_percent

# 内存使用率
aicorp_system_memory_usage_percent

# 网络吞吐量 (bytes/sec)
rate(aicorp_system_network_bytes_in_total[1m])   # 入站
rate(aicorp_system_network_bytes_out_total[1m])  # 出站
```

### 7.3 系统级监控实现

使用 `gopsutil` 库采集 CPU、内存、网络等系统级指标：

```go
import (
    "github.com/shirou/gopsutil/v3/cpu"
    "github.com/shirou/gopsutil/v3/mem"
    "github.com/shirou/gopsutil/v3/net"
)

// CPU 使用率（1秒采样）
cpuPct, _ := cpu.Percent(1*time.Second, false)

// 内存使用
vmStat, _ := mem.VirtualMemory()
// vmStat.Used, vmStat.Total, vmStat.UsedPercent

// 网络 I/O（累计值）
netIO, _ := net.IOCounters(false)
// netIO[0].BytesRecv, netIO[0].BytesSent
```

**采集频率**：每 5 秒采集一次，平衡精度与性能开销。

**指标列表**：

| 指标名 | 类型 | 说明 |
|-------|------|------|
| `aicorp_system_cpu_usage_percent` | Gauge | CPU 使用率 (0-100) |
| `aicorp_system_memory_used_bytes` | Gauge | 已用内存 |
| `aicorp_system_memory_total_bytes` | Gauge | 总内存 |
| `aicorp_system_memory_usage_percent` | Gauge | 内存使用率 |
| `aicorp_system_network_bytes_in_total` | Gauge | 网络接收字节数 |
| `aicorp_system_network_bytes_out_total` | Gauge | 网络发送字节数 |
| `aicorp_system_go_goroutines` | Gauge | Goroutine 数量 |
| `aicorp_system_go_memory_alloc_bytes` | Gauge | Go 运行时内存 |

### 7.4 Grafana Dashboard 面板（18 个）

```
第 1 行：推理核心
  ┌──────────────┬───────────────────┬──────────────────┐
  │ Inference QPS │ Latency P50/P95/99 │ Tokens/sec Gauge │
  └──────────────┴───────────────────┴──────────────────┘

第 2 行：推理详情
  ┌────────────────────┬──────────────┬──────────────┐
  │ Token 吞吐量 (bar) │ Cache Hit %  │  Error Rate  │
  └────────────────────┴──────────────┴──────────────┘

第 3 行：Agent & DB
  ┌────────────────────┬──────────────┬──────────────┐
  │ Agent Task Rate    │  DB 连接数    │  DB 查询延迟  │
  └────────────────────┴──────────────┴──────────────┘

第 4 行：系统监控 (新增)
  ┌──────────────┬──────────────┬──────────────────┐
  │ CPU Usage %  │ Memory Usage │   Network I/O    │
  └──────────────┴──────────────┴──────────────────┘

第 5 行：Token 监控 (新增)
  ┌────────────────────┬──────────────┬──────────────┐
  │ Token Gen Rate     │ Total Tokens │  Avg TPS     │
  └────────────────────┴──────────────┴──────────────┘
```

### 7.5 部署方式

```bash
# 一键部署脚本
bash deploy/monitoring-deploy.sh

# 手动验证
curl http://localhost:9090/api/v1/targets  # Prometheus 抓取目标
curl http://localhost:3000                  # Grafana UI (admin/admin)
```

---

## 8. 系统架构总览

### 8.1 Phase 1 完整架构

```
                    ┌─────────────────┐
                    │   浏览器 (前端)   │
                    │  Pixel Tavern UI │
                    └────────┬────────┘
                             │ HTTP/WS
                    ┌────────▼────────┐
                    │  Orchestrator   │ :8080
                    │  (Gin + WS)     │
                    │                 │──── /metrics ──→ Prometheus :9090
                    │  InferenceService│                       │
                    │  │  Prometheus   │               Grafana :3000
                    │  │  Metrics      │
                    └──┬──────┬───────┘
                       │      │
              ┌────────▼──┐  ┌▼──────────────┐
              │ DeepSeek  │  │  PostgreSQL 16 │ :5432
              │ Cloud API │  │  + pgvector    │
              │           │  │  + pg_trgm     │
              └───────────┘  │                │
                             │  8 tables:     │
                             │  agents        │
                             │  tasks         │
                             │  knowledge_base│
                             │  inference_    │
                             │   metrics      │
                             │  workflow_runs │
                             │  model_registry│
                             │  audit_log     │
                             │  system_config │
                             └────────────────┘
```

### 8.2 数据流

```
用户请求 → Orchestrator
  ├── /api/v1/chat → InferenceService
  │     ├── DeepSeek API 调用（同步）
  │     ├── Prometheus Counter/Histogram 更新（同步，纳秒级）
  │     └── PostgreSQL inference_metrics 写入（异步 goroutine）
  │
  ├── /api/v1/db/agents → AgentRepo → PostgreSQL
  ├── /api/v1/db/stats → MetricsRepo → get_inference_stats()
  ├── /metrics → promhttp.Handler() → Prometheus scrape
  └── /ws → WebSocket 广播
```

### 8.3 Phase 1 交付清单

| 组件 | 状态 | 文件位置 |
|------|------|---------|
| PostgreSQL 16.6 | 运行中 | /usr/local/pgsql16/ |
| pgvector 0.8.0 | 已安装 | extension in PG |
| 数据库 Schema (8表) | 已初始化 | deploy/postgresql/schema.sql |
| Go 数据库层 (pgx) | 测试通过 | pkg/database/*.go |
| DeepSeek API 集成 | 已连接 | pkg/llm/inference_service.go |
| Prometheus 指标 (30+) | 已暴露 | pkg/metrics/prometheus.go |
| 系统监控 (CPU/内存/网络) | 已实现 | pkg/metrics/system.go |
| Grafana Dashboard (18面板) | 配置就绪 | deploy/grafana/dashboard.json |
| Prometheus 配置 | 配置就绪 | deploy/prometheus/prometheus.yml |
| 部署脚本 | 就绪 | deploy/monitoring-deploy.sh |

### 8.4 监控指标总览

| 类别 | 指标数量 | 关键指标 |
|------|---------|---------|
| 推理层 | 8 | QPS、延迟(P50/P95/P99)、Token速率、TTFT、缓存命中率 |
| Agent层 | 3 | 任务数、任务延迟、活跃Agent数 |
| 数据库层 | 3 | 连接数、查询延迟 |
| HTTP层 | 2 | 请求数、请求延迟 |
| 系统层 | 8 | CPU%、内存、网络I/O、Goroutines |
| **总计** | **24+** | |

### 8.5 API 端点一览

```
GET  /health                → 服务健康检查
GET  /metrics               → Prometheus 标准指标
GET  /api/v1/llm/status     → LLM 连接状态
POST /api/v1/chat           → 与 AI Agent 对话
GET  /api/v1/db/health      → 数据库健康（连接池状态）
GET  /api/v1/db/agents      → 数据库中的 Agent 列表
GET  /api/v1/db/models      → 注册的模型列表
GET  /api/v1/db/stats       → 24h 推理统计（QPS/延迟/Token/缓存命中率）
GET  /api/v1/db/audit       → 最近审计日志
GET  /ws                    → WebSocket 实时通信
```

---

## 附录 A：常用运维命令

```bash
# PostgreSQL
su - postgres -c "/usr/local/pgsql16/bin/psql -d aicorp"   # 连接数据库
su - postgres -c "/usr/local/pgsql16/bin/pg_ctl -D /var/lib/pgsql/data status"  # 查看状态
su - postgres -c "/usr/local/pgsql16/bin/pg_ctl -D /var/lib/pgsql/data restart" # 重启

# 查看活跃连接
SELECT pid, usename, application_name, state, query_start
FROM pg_stat_activity WHERE datname = 'aicorp';

# 查看表大小
SELECT relname, pg_size_pretty(pg_total_relation_size(oid))
FROM pg_class WHERE relkind = 'r' ORDER BY pg_total_relation_size(oid) DESC;

# Orchestrator
export DEEPSEEK_API_KEY=sk-xxx
cd /home/haoqian.li/ai-corp && ./bin/orchestrator  # 前台启动
nohup ./bin/orchestrator > /tmp/orchestrator.log 2>&1 &  # 后台启动

# 快速验证所有指标
curl -s http://localhost:8080/metrics | grep -E "^aicorp_" | cut -d'{' -f1 | sort -u

# 验证系统指标
curl -s http://localhost:8080/metrics | grep -E "^aicorp_system_(cpu|memory|network)"

# 验证推理指标（需先调用 /api/v1/chat）
curl -s http://localhost:8080/metrics | grep -E "^aicorp_inference"
```

## 附录 B：Phase 2 预告

Phase 2 将实现：
- **可视化工作流编排**：ReactFlow 拖拽式 DAG 编辑器
- **实时监控面板**：集成到前端的 WebSocket 推送指标
- **知识库管理**：文档上传 → 分片 → Embedding → pgvector 存储
- **Agent 协作**：多 Agent 基于 DAG 的任务编排和并行执行
