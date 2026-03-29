# AI Corp

多智能体协作平台。将 AI 系统建模为一家公司：不同职能的 Agent 接受任务、在隔离沙箱中执行、完成后自动学习并将经验共享给同事。

---

## 核心亮点

**Docker 任务沙箱隔离（参考 E2B / Daytona）**
每个任务运行在独立容器内，`--cap-drop=ALL` + seccomp + `--network none` + 内存/CPU 硬限制，任务失败不影响宿主机和其他任务。支持代码执行、网页抓取、数据处理三种预定义沙箱模板。

**AI 自我迭代与经验共享（参考 MemGPT/Letta）**
任务完成后自动触发三层学习闭环：经验提取 -> LLM 反思分析 -> 技能抽象。记忆分为短期（内存 TTL 1h）、长期（DB 持久化）、共享（跨 Agent 广播）三层，参考 MemGPT 的 Core/Archival/Recall Memory 分层模型。关键记忆生成 Embedding 向量写入 pgvector，支持语义相似度检索。下一次执行前，历史经验自动注入 system prompt。

**全链路可视化监控**
24+ Prometheus 指标（推理延迟、Token 消耗、TPS、CPU/内存/网络），gopsutil 每 5 秒采集系统资源，Grafana 18 面板实时展示。

**PostgreSQL + pgvector RAG 知识库**
PostgreSQL 16 + pgvector 0.8.0 源码编译。`knowledge_base` 和 `agent_memory` 表存储 1536 维向量，IVFFlat 索引余弦相似度检索。应用层自动管理索引生命周期（`lists = sqrt(rows)`，数据增长 4 倍自动重建）。

**LLM 双模式（云端 + 本地 Ollama）**
统一接口支持 DeepSeek/OpenAI 云端 API 和 Ollama 本地部署，`Auto` 模式自动优先本地推理、失败回退云端。Ollama 直接调用原生 `/api/chat` 接口，支持 DeepSeek 蒸馏等任意模型。

---

## 架构

```
前端 (像素风 UI)
      |  WebSocket / REST
      v
Orchestrator
  |-- Agent Manager      注册/心跳/路由
  |-- Task Scheduler      创建/分配/重试
  |-- Self-Improvement    记忆注入 / 反思 / 共享
  |-- InferenceService    LLM 调用 + DB 记录 + Prometheus
  |-- Metrics Collector   gopsutil + Prometheus
  |
  |-- chat 接口记忆增强:
  |     GetRelevantMemoriesWithQuery()
  |     -> 短期(内存) + 语义Top5(pgvector) + 长期(DB) + 共享(DB)
  |     -> 拼接 system prompt -> InferenceService
  |
Agent Runtime
  +-- Docker Sandbox (seccomp + 无网络 + 资源限制)

PostgreSQL 16 + pgvector
  |-- agents / tasks / inference_metrics
  |-- knowledge_base (IVFFlat 向量检索)
  +-- agent_memory / agent_experiences / agent_reflections / agent_skills

Prometheus + Grafana (18 面板)
```

---

## 技术栈

| 层 | 技术 |
|----|------|
| 语言 | Go 1.21 |
| 数据库 | PostgreSQL 16 + pgvector 0.8.0（源码编译） |
| 连接池 | pgx v5 + Repository 模式 |
| LLM | DeepSeek API / OpenAI / Ollama（双模式，统一接口） |
| 向量检索 | pgvector IVFFlat 索引，余弦相似度，应用层自管理 |
| 监控 | Prometheus client_golang + gopsutil + Grafana |
| 容器 | Docker（沙箱隔离） |
| 前端 | 原生 HTML/CSS/JS，WebSocket 实时通信 |

---

## 快速开始

### 方式一：源码编译部署

```bash
# 1. 部署 PostgreSQL 16 + pgvector（一键脚本）
bash deploy/postgresql/setup-pgvector.sh

# 2. 初始化 Schema
psql -U postgres -d aicorp -f deploy/postgresql/schema.sql
psql -U postgres -d aicorp -f deploy/postgresql/memory_schema.sql

# 3. 编译并启动 Orchestrator
go build -o orchestrator ./cmd/orchestrator/
export LLM_API_KEY=your_deepseek_key
export LLM_PROVIDER=deepseek
./orchestrator

# 4. 访问
# 前端界面:     http://localhost:8080
# Prometheus:   http://localhost:8080/metrics
```

### 方式二：Docker Compose

```bash
docker-compose up -d
# 自动启动 PostgreSQL(pgvector) + Orchestrator
```

### 方式三：本地 Ollama 推理（无需 API Key）

```bash
# 1. 安装并启动 Ollama
ollama pull deepseek-r1:7b

# 2. 启动 Orchestrator（配置 Ollama 地址）
export OLLAMA_BASE_URL=http://localhost:11434
export OLLAMA_MODEL=deepseek-r1:7b
./orchestrator

# chat 接口将自动使用本地模型推理
```

---

## 自我迭代机制

参考 MemGPT/Letta 的分层记忆架构：

```
任务完成
    |
    v
+------------------+
| 经验提取          | --> agent_experiences + short_term 记忆
| LLM 提取教训/模式  |     生成 Embedding 向量
+--------+---------+
         |
         v
+------------------+
| 反思分析          | --> agent_reflections
| LLM 深度分析      |     反思内容生成 Embedding 向量
+--------+---------+
         |
         v
+------------------+
| 技能学习          | --> agent_skills (use_count + success_rate)
| 识别可复用技能    |
+--------+---------+
         |
         v
+------------------+
| 知识共享          | --> 广播给所有其他 Agent
| 经验 + 技能广播   |     存为 type=shared 的 agent_memory
+------------------+
```

下次对话时，`GetRelevantMemoriesWithQuery()` 自动从短期记忆、向量语义检索、长期记忆、共享记忆四个维度召回相关经验，注入 system prompt。

---

## 目录

```
ai-corp/
|-- cmd/
|   |-- orchestrator/      # 主服务入口
|   +-- agent-runtime/     # Agent 独立运行时
|-- pkg/
|   |-- database/          # pgx 连接池 + Repository + 向量索引管理
|   |-- llm/               # LLM 客户端 + InferenceService + Ollama 双模式
|   |-- memory/            # 自我迭代系统（记忆/反思/技能/共享）
|   |-- metrics/           # Prometheus 指标 + gopsutil 采集
|   |-- rag/               # RAG 服务 + PgVectorStore 适配器
|   |-- sandbox/           # Docker 任务沙箱（E2B/Daytona 架构）
|   |-- agent/             # Agent Runtime
|   |-- message/           # 消息总线（内存 / NATS）
|   |-- skill/             # MCP Skill 系统
|   |-- compiler/          # 编译器插件（LLVM/GCC/Go）
|   +-- workflow/          # DAG 工作流引擎
|-- deploy/
|   |-- postgresql/        # Schema SQL + pgvector 一键部署脚本
|   |-- grafana/           # Dashboard JSON（18 面板）
|   +-- sandbox/           # seccomp profile
|-- web/pixel/             # 前端（像素风 HTML/CSS/JS）
|-- docs/                  # 技术文档
+-- configs/               # 配置文件
```

---

## Agent 角色

| 角色 | 职责 |
|------|------|
| Phoenix（PM） | 需求分解、任务规划 |
| Atlas（Backend） | Go 代码开发、API 设计 |
| Pixel（Frontend） | UI 开发 |
| Guardian（DevOps） | 部署、监控、基础设施 |
| Sentinel（QA） | 测试、代码审查 |

---

## 文档

- [技术架构文档](docs/ARCHITECTURE.md) - 7 大核心模块详细设计
- [核心功能技术说明](docs/10-phase2-features.md) - Docker 沙箱 + 自我迭代详细实现
- [开发路线图](ROADMAP.md) - 版本规划与功能清单
