# AI Corp 技术架构文档

## 1. 系统概述

AI Corp 是一个多智能体协作平台。用"公司"隐喻建模 AI 系统：不同职能的 Agent（研发、测试、架构、运维）由 Orchestrator 统一调度，完成任务的自动分解、执行与学习闭环。

**核心设计目标**：
- 每个任务在隔离的 Docker 沙箱中执行，保证安全
- Agent 执行完任务后自动提取经验、反思并共享给其他 Agent
- 全链路指标（推理延迟、Token 消耗、CPU/内存）实时可视化

---

## 2. 整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                       前端 (WebSocket + REST)                     │
│          像素风 UI · 任务看板 · 实时监控面板 · Agent 状态画布       │
└───────────────────────────────┬──────────────────────────────────┘
                                │ WebSocket / REST API
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│                         Orchestrator                              │
│  ┌────────────────┐  ┌─────────────────┐  ┌────────────────────┐ │
│  │  Agent Manager │  │  Task Scheduler │  │  Self-Improvement  │ │
│  │  注册/心跳/路由 │  │  创建/分配/重试  │  │  记忆注入 / 反思   │ │
│  └────────────────┘  └─────────────────┘  └────────────────────┘ │
│  ┌────────────────┐  ┌─────────────────┐  ┌────────────────────┐ │
│  │  InferenceService│ │  Metrics Collector│ │  Audit Log        │ │
│  │  LLM + DB 记录  │  │  Prometheus 指标  │  │  操作审计          │ │
│  └────────────────┘  └─────────────────┘  └────────────────────┘ │
└───────────────┬──────────────────────────┬───────────────────────┘
                │                          │
     WebSocket / NATS               pgx connection pool
                │                          │
                ▼                          ▼
┌───────────────────────┐    ┌─────────────────────────────────────┐
│     Agent Runtime     │    │          PostgreSQL 16               │
│  Developer / Tester   │    │  agents · tasks · knowledge_base     │
│  Architect / DevOps   │    │  inference_metrics · workflow_runs   │
│                       │    │  model_registry · audit_log          │
│  ┌─────────────────┐  │    │  agent_memory · agent_experiences    │
│  │  Docker Sandbox │  │    │  agent_reflections · agent_skills    │
│  │  seccomp + 无网络│  │    │  sandbox_executions                 │
│  │  内存/CPU 限制   │  │    └─────────────────────────────────────┘
│  └─────────────────┘  │
└───────────────────────┘
```

---

## 3. 核心模块

### 3.1 Orchestrator (`cmd/orchestrator/`)

任务生命周期全流程管理。

**关键流程**：
```
POST /api/v1/tasks
  → taskQueue channel
  → taskScheduler goroutine 寻找空闲 Agent
  → WS broadcast task_assigned
  → Agent 执行
  → WS task_complete / task_fail
  → 异步触发 SelfImprovementLoop.ProcessTaskResult
```

**chat 接口记忆增强**：
```
POST /api/v1/chat { agent_type, message }
  → GetRelevantMemories(agent_type)   // 查短期+长期+共享记忆
  → 拼接到 system prompt
  → InferenceService.ChatWithSystem
  → 记录 inference_metrics
```

**API 端点**：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET/POST | `/api/v1/agents` | Agent CRUD |
| GET/POST | `/api/v1/tasks` | 任务 CRUD |
| POST | `/api/v1/tasks/:id/assign` | 手动分配 |
| POST | `/api/v1/chat` | LLM 对话（含记忆注入） |
| GET | `/api/v1/db/stats` | 推理统计（24h） |
| GET | `/metrics` | Prometheus scrape |
| GET | `/ws` | WebSocket 长连接 |

---

### 3.2 Docker Task Sandbox (`pkg/sandbox/`)

每个任务在独立容器中执行，参考 E2B / Daytona 架构。

**安全措施**：

| 维度 | 实现 |
|------|------|
| 内存隔离 | `--memory` + `--memory-swap`（禁用 swap） |
| CPU 隔离 | `--cpu-quota` + `--cpus` |
| 进程限制 | `--pids-limit 256` |
| 能力限制 | `--cap-drop=ALL` |
| 权限提升 | `--no-new-privileges` |
| 系统调用 | `--security-opt seccomp=default`（默认开启） |
| 网络隔离 | `--network none`（默认）/ `--network ai-corp-sandbox-net`（internal bridge，无外网出口） |

**预定义模板**：

```go
CodeExecutionSandbox()  // 1GB, 1 CPU, 无网络, 10min 超时
WebScraperSandbox()     // 512MB, 0.5 CPU, internal 网络 + 白名单, 5min
DataProcessingSandbox() // 2GB, 2 CPU, 无网络, 30min 超时
```

**执行流程**：
```
CreateSandbox(taskID, image, config)
  → 创建 /tmp/ai-corp-sandbox/{sandboxID} 工作目录
  → docker run -d [resource+security flags] --network none image sleep N
  → 返回 sandbox.ID

ExecuteInSandbox(sandboxID, command)
  → docker exec {containerID} command
  → 超时 → status=timeout，强制 stop

Cleanup()  → docker stop + rm workdir（cleanupOnce 保证幂等）
```

---

### 3.3 Self-Improvement Loop (`pkg/memory/`)

参考 MemGPT/Letta Memory Blocks + Reflexion 框架，任务完成后自动触发三层学习闭环。

**流程**：
```
task_complete / task_fail
         │
         ▼
  ExperienceExtractor          → 提取 lessons / patterns / suggestions
         │                       存入 agent_experiences
         ▼
  ReflectionEngine             → LLM 分析根本原因、洞察、行动项
         │                       存入 agent_reflections
         ▼
  SkillLearner                 → 识别新技能，存入 agent_skills
         │
         ▼
  KnowledgeSharer              → 将经验/技能广播给其他 Agent
                                 存为 type=shared 的 agent_memory
```

**记忆类型**：

| 类型 | 生命周期 | 用途 |
|------|----------|------|
| `short_term` | 1 小时，超过限制淘汰低重要性 | 当前上下文 |
| `long_term` | 永久，由 ConsolidateToLongTerm 触发 | 重要经验固化 |
| `reflection` | 永久 | 自我分析记录 |
| `skill` | 永久，use_count + success_rate 动态更新 | 可复用技能 |
| `shared` | 永久 | 跨 Agent 传递 |

**chat 记忆注入**：
```
GetRelevantMemories(agentID, taskType)
  → short_term (内存) + long_term (DB) + shared (DB)
  → 拼接到 system prompt header
  → 下一轮推理自动利用历史经验
```

---

### 3.4 RAG 知识库 (`pkg/rag/` + `pkg/database/`)

**存储**：PostgreSQL 16 + pgvector 0.8.0（源码编译）

**两层 VectorStore**：
```
MemoryVectorStore   → 开发/测试用，余弦相似度暴力搜索
PgVectorStore       → 生产用，对接 KnowledgeBaseRepo
                       INSERT → knowledge_base + embedding vector(1536)
                       SEARCH → pgvector IVFFlat 索引余弦搜索
                       结果带 _similarity float64
```

**检索 SQL**：
```sql
SELECT id, title, content, 1 - (embedding <=> $1::vector) AS similarity
FROM knowledge_base
WHERE embedding IS NOT NULL
ORDER BY embedding <=> $1::vector
LIMIT $2
```

---

### 3.5 监控体系 (`pkg/metrics/`)

**指标采集**：

| 来源 | 实现 | 指标示例 |
|------|------|---------|
| LLM 推理 | `InferenceService` 封装 | `ai_inference_latency_seconds`, `ai_tokens_generated` |
| 系统资源 | `gopsutil` 每 5s 采集 | `system_cpu_percent`, `system_memory_percent`, `system_network_bytes` |
| 任务状态 | Orchestrator 事件驱动 | `tasks_created_total`, `tasks_completed_total` |
| 知识库 | DB 查询 | `knowledge_entries_total` |

**采集路径**：
```
gopsutil → SystemCollector → prometheus.Gauge
InferenceService.ChatWithSystem
  → 记录 inference_metrics 表
  → promauto Counter/Histogram 更新
Grafana → /metrics → 18 个 Panel 展示
```

**Grafana Dashboard**：18 面板，覆盖推理请求速率、Token 速率、P95 延迟、CPU/内存趋势、网络 IO、任务完成率。

---

### 3.6 LLM 双模式 (`pkg/llm/`)

```go
// 统一接口，运行时切换
type Client struct { config Config }  // deepseek / openai / claude

// 云端 API 模式
NewClient(Config{Provider: "deepseek", APIKey: "...", Model: "deepseek-chat"})

// 本地 Ollama 模式
NewClient(Config{Provider: "openai", BaseURL: "http://localhost:11434/v1", Model: "deepseek-coder:6.7b"})
```

`InferenceService` 在 `Client` 上增加：
- 调用前后计时，计算 TPS = completion_tokens / latency_s
- 写入 `inference_metrics` 表
- 更新 Prometheus 指标

---

### 3.7 数据库层 (`pkg/database/`)

**PostgreSQL 16**：CentOS 7 源码编译，pgvector 0.8.0 扩展编译安装。

**连接池（pgx v5）**：
```go
MaxConns = 20, MinConns = 2
MaxConnLifetime = 30min, MaxConnIdleTime = 5min
```

**Repository 清单**：

| Repo | 表 | 核心方法 |
|------|----|---------|
| `AgentRepo` | `agents` | `Create`, `List`, `UpdateStatus` |
| `TaskRepo` | `tasks` | `Create`, `UpdateStatus`, `GetByAgent` |
| `KnowledgeBaseRepo` | `knowledge_base` | `Insert`, `SearchSimilar`, `GetByID`, `Delete`, `Count` |
| `MetricsRepo` | `inference_metrics` | `Record`, `GetStats` |
| `ModelRegistryRepo` | `model_registry` | `ListActive`, `UpdateHealth` |
| `AuditRepo` | `audit_log` | `Log`, `Recent` |

---

## 4. 数据库 Schema 总览

**Phase 1**（`deploy/postgresql/schema.sql`）：
```
agents · tasks · knowledge_base · inference_metrics
workflow_runs · model_registry · audit_log · system_config
```

**Phase 2**（`deploy/postgresql/memory_schema.sql`）：
```
agent_memory · agent_experiences · agent_reflections
agent_skills · sandbox_executions
```

**关键扩展**：
- `vector` extension：IVFFlat 索引，余弦相似度
- `pg_trgm` extension：全文模糊搜索
- 自动 `updated_at` trigger
- `get_inference_stats(hours)` 聚合函数
- `get_self_improvement_stats(agent_id)` 自我改进统计

---

## 5. 目录结构

```
ai-corp/
├── cmd/
│   ├── orchestrator/      # 主服务入口
│   └── agent-runtime/     # Agent 独立运行时
├── pkg/
│   ├── database/          # pgx 连接池 + Repository
│   ├── llm/               # LLM 客户端 + InferenceService
│   ├── memory/            # 自我迭代系统（记忆/反思/技能/共享）
│   ├── metrics/           # Prometheus 指标 + gopsutil 采集
│   ├── rag/               # RAG 服务 + PgVectorStore 适配器
│   ├── sandbox/           # Docker 任务沙箱
│   ├── agent/             # Agent Runtime
│   ├── message/           # 消息总线（内存 / NATS）
│   ├── skill/             # MCP Skill 系统
│   ├── compiler/          # 编译器插件（LLVM/GCC/Go）
│   └── workflow/          # DAG 工作流引擎
├── deploy/
│   ├── postgresql/        # Schema SQL
│   ├── grafana/           # Dashboard JSON（18 面板）
│   └── sandbox/           # seccomp profile
├── web/pixel/             # 前端（像素风 HTML/CSS/JS）
├── docs/                  # 技术文档
└── configs/               # 配置文件
```

---

## 6. 部署

### 快速启动

```bash
# 1. 启动 PostgreSQL（已源码编译安装）
pg_ctl -D /usr/local/pgsql/data start

# 2. 初始化 Schema
psql -U postgres -d aicorp -f deploy/postgresql/schema.sql
psql -U postgres -d aicorp -f deploy/postgresql/memory_schema.sql

# 3. 启动 Orchestrator
export LLM_API_KEY=your_key
export LLM_PROVIDER=deepseek
./orchestrator
```

### Docker Compose

```bash
docker-compose up -d
```

### Prometheus + Grafana

```yaml
# prometheus.yml
scrape_configs:
  - job_name: ai-corp
    static_configs:
      - targets: ['orchestrator:8080']
```

导入 `deploy/grafana/dashboard.json` 至 Grafana，18 个面板即刻可用。
