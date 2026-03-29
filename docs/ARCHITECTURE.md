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

参考 **MemGPT/Letta** 的分层记忆架构和 **Reflexion** 框架，实现 Agent 在任务完成后自动触发三层学习闭环，持续积累和共享知识。

#### 与 MemGPT 架构的对应关系

| MemGPT 概念 | AI Corp 实现 | 说明 |
|-------------|-------------|------|
| Core Memory (persona/human) | `short_term` MemoryBlock | 当前对话上下文，1 小时 TTL，超限按 Importance 淘汰 |
| Archival Memory | `long_term` MemoryBlock + pgvector | 持久化经验，支持向量语义检索（IVFFlat 索引） |
| Recall Memory | `agent_experiences` + `agent_reflections` | 历史任务记录，可按 task_type 过滤 |
| Memory Consolidation | `ConsolidateToLongTerm()` | 每 N 个任务自动将高重要性短期记忆固化为长期记忆 |
| Memory Sharing (扩展) | `KnowledgeSharer` | MemGPT 原生不支持，AI Corp 扩展为多 Agent 经验/技能广播 |

#### 分层记忆模型

```
┌─────────────────────────────────────────────────────────┐
│                   Memory Layer                           │
│                                                          │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐ │
│  │  Short-Term  │   │   Long-Term  │   │    Shared    │ │
│  │  (内存缓存)   │   │  (PostgreSQL) │   │  (跨Agent)   │ │
│  │  TTL=1h      │   │  向量+文本    │   │  广播写入     │ │
│  │  Importance↓  │   │  IVFFlat 索引 │   │              │ │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘ │
│         │                  │                   │         │
│         └────────┬─────────┴───────────────────┘         │
│                  ▼                                        │
│         GetRelevantMemoriesWithQuery()                    │
│         短期 + 语义Top5 + 长期 + 共享 → 去重 → Importance排序│
└─────────────────────────────────────────────────────────┘
```

#### 三层学习闭环

```
task_complete / task_fail
         │
         ▼
  ExperienceExtractor            → LLM 提取 lessons / patterns / suggestions
         │                         存入 agent_experiences + short_term
         │                         生成 Embedding 向量写入 pgvector
         ▼
  ReflectionEngine               → LLM 深度分析根本原因、洞察、行动项
         │                         存入 agent_reflections
         │                         反思内容同步生成 Embedding 向量
         ▼
  SkillLearner                   → 从反思中识别新技能
         │                         存入 agent_skills (use_count / success_rate)
         ▼
  KnowledgeSharer                → 将经验/技能广播给其他 Agent
                                   存为 type=shared 的 agent_memory
                                   共享记忆同样携带 Embedding 向量
```

#### 关键实现细节

**Agent ID 动态管理**：`SelfImprovementLoop` 使用 `sync.RWMutex` 保护 `agentIDs` 列表，Orchestrator 在 Agent 创建/删除/注册时实时同步，确保知识共享覆盖所有在线 Agent。

**固化策略**：基于 `sync/atomic` 原子计数器实现每 Agent 独立的任务计数，默认每 10 个任务触发一次 `ConsolidateToLongTerm()`。高重要性（>0.7）或高访问次数（>3）的短期记忆自动升级为长期记忆。

**语义检索增强**：`GetRelevantMemoriesWithQuery()` 支持双路检索——当配置了 `EmbeddingClient` 时，先对查询文本生成向量，通过 pgvector `<=>` 余弦距离算子检索 Top5 相似记忆，再合并传统的按 agent/type 过滤结果，最终按 Importance 降序排列，上限 15 条。

**记忆类型**：

| 类型 | 生命周期 | 用途 |
|------|----------|------|
| `short_term` | 1 小时 TTL，超限按 Importance 淘汰 | 当前工作上下文 |
| `long_term` | 永久，由 `ConsolidateToLongTerm` 触发 | 重要经验固化 |
| `reflection` | 永久 | LLM 自我分析记录 |
| `skill` | 永久，`use_count` + `success_rate` 动态更新 | 可复用技能模板 |
| `shared` | 永久 | 跨 Agent 知识传递 |

**chat 记忆注入**：
```
POST /api/v1/chat { agent_type, message }
  → GetRelevantMemoriesWithQuery(agentID, taskType, message)
     1. short_term (内存缓存，高优先级)
     2. 语义相似度 Top5 (pgvector <=> 余弦距离)
     3. long_term (DB 按 agent + type 查询)
     4. shared (DB 跨 Agent 共享记忆)
  → 去重 + Importance 降序排列 (上限 15 条)
  → 拼接到 system prompt header
  → InferenceService.ChatWithSystem
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

**IVFFlat 索引自管理**（`pkg/database/vector_index.go`）：

应用启动时自动调用 `EnsureVectorIndexes()`，对 `knowledge_base` 和 `agent_memory` 两张向量表执行：

1. 检测表是否存在及有向量数据的行数
2. 计算最优 `lists` 参数：`sqrt(行数)`，范围 `[1, 1000]`
3. 首次创建 IVFFlat 索引（`vector_cosine_ops`）
4. 数据量增长超过 4 倍时自动 `DROP + CREATE` 重建索引
5. 支持运行时调整 `ivfflat.probes` 参数（精确度/性能权衡）

```sql
-- 自动创建的索引示例
CREATE INDEX idx_kb_embedding_ivfflat
  ON knowledge_base USING ivfflat (embedding vector_cosine_ops)
  WITH (lists = 100);
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

支持云端 API 和本地 Ollama 两种推理模式，运行时统一接口、自动 Fallback。

**架构**：
```
DualModeClient
  ├── OllamaClient (本地)   → POST /api/chat (非流式)
  │     120s 超时，ollamaChatRequest/Response 完整解析
  │     支持 IsAvailable() 健康检查
  │
  └── CloudClients (云端)   → DeepSeek / OpenAI / Claude
        优先级: deepseek > openai > claude
        失败自动切换下一个 provider
```

**模式选择策略**：
```go
ModelTypeLocal  → 仅使用 Ollama 本地模型
ModelTypeCloud  → 仅使用云端 API（按优先级 Fallback）
ModelTypeAuto   → 优先本地 → 失败回退云端
```

**Ollama 集成**：
- 直接调用 Ollama 原生 `/api/chat` 接口（非 OpenAI 兼容层）
- 支持任意 Ollama 模型（DeepSeek 蒸馏、Qwen、Llama 等）
- 请求/响应完整类型定义，含 `prompt_eval_count`、`eval_count` 等统计字段

`InferenceService` 在 `Client` 上增加：
- 调用前后计时，计算 TPS = completion_tokens / latency_s
- 写入 `inference_metrics` 表
- 更新 Prometheus 指标（延迟直方图、Token 计数器）

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

**向量索引自管理**（`vector_index.go`）：

| 功能 | 实现 |
|------|------|
| 自动创建 | 启动时检测向量数据行数，`lists = sqrt(行数)` |
| 自动重建 | 数据量增长超 4 倍时 `DROP + CREATE` |
| 参数调优 | 运行时 `SET ivfflat.probes = N`（精确度/性能权衡） |
| 覆盖表 | `knowledge_base`、`agent_memory` |

**一键部署脚本**（`deploy/postgresql/setup-pgvector.sh`）：
PostgreSQL 16 + pgvector 0.8.0 源码编译、initdb、启用扩展、导入 Schema，一条命令完成。

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
