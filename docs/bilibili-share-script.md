# AI Corp 项目分享 - Bilibili 视频讲解稿

> 适用于 10-15 分钟的技术分享视频，可直接作为讲解提纲使用。

---

## 一、开场（1 分钟）

大家好，今天给大家分享一个我用 Go 语言开发的多智能体协作平台 -- AI Corp。

这个项目的核心思路是：**把 AI 系统建模成一家公司**。公司里有不同职能的 AI 员工（Agent），他们接受任务、在隔离的沙箱环境中执行，完成任务后自动学习，并把经验分享给同事。

项目涵盖了这些技术点：
- Docker 容器沙箱隔离
- 仿 MemGPT 架构的 AI 自我迭代系统
- PostgreSQL + pgvector 向量检索
- DAG 工作流引擎
- JWT 认证、Token 配额、PII 脱敏等安全体系
- 全链路 Prometheus + Grafana 监控

下面我按模块来讲。

---

## 二、整体架构（2 分钟）

**【建议配图：架构图】**

```
前端 (像素风 UI)
      |  WebSocket / REST
      v
Orchestrator（总控服务）
  |-- Agent Manager       注册/心跳/路由
  |-- Task Scheduler       创建/分配/重试
  |-- Self-Improvement     记忆注入 / 反思 / 共享
  |-- InferenceService     LLM 调用 + DB 记录
  |-- Workflow Engine      DAG 工作流引擎
  |-- Security Layer       JWT + 配额 + PII 脱敏 + 审计
  |
Agent Runtime
  +-- Docker Sandbox (seccomp + 无网络 + 资源限制)

PostgreSQL 16 + pgvector
  |-- agents / tasks / inference_metrics / audit_log
  |-- knowledge_base (IVFFlat 向量检索)
  +-- agent_memory / agent_experiences / agent_reflections / agent_skills
```

技术栈：Go 1.21 + PostgreSQL 16 + pgvector 0.8.0 + Prometheus + Grafana + Docker

讲解要点：
- Orchestrator 是中心节点，所有请求经过它路由
- Agent 在独立 Docker 容器中执行任务
- 数据层基于 PostgreSQL + pgvector，支持传统 CRUD 和向量语义检索
- 前端是一个像素风格的 Web 界面，通过 WebSocket 实时通信

---

## 三、核心亮点讲解（8 分钟）

### 亮点 1：Docker 任务沙箱隔离（1.5 分钟）

**【建议配图：沙箱架构图】**

讲解要点：
- 参考 E2B（用 Firecracker 微虚拟机做隔离）和 Daytona（用 Docker 容器做隔离）的思路
- 每个任务在独立容器中运行，安全措施包括：
  - `--cap-drop=ALL`：移除所有 Linux 能力
  - `--security-opt=no-new-privileges`：禁止提权
  - `--network none`：默认断网
  - 内存/CPU 硬限制
  - 硬超时强制杀容器
- 三种预定义沙箱模板：代码执行（256MB/0.5CPU）、网页抓取（512MB/1CPU+白名单网络）、数据处理（1GB/2CPU）
- `cleanupOnce` 保证资源清理幂等，不会泄露容器

可以展示的代码：`pkg/sandbox/docker_sandbox.go` 中的 `buildDockerRunArgs()` 方法

### 亮点 2：AI 自我迭代机制（2.5 分钟）-- 最核心的亮点

**【建议配图：MemGPT 对照表 + 三层闭环流程图】**

讲解要点：

1）**参考 MemGPT/Letta 架构**：

| MemGPT 概念 | AI Corp 实现 |
|-------------|-------------|
| Core Memory | short_term 记忆（内存缓存，1小时TTL） |
| Archival Memory | long_term 记忆（PostgreSQL 持久化 + pgvector 向量） |
| Recall Memory | agent_experiences + agent_reflections 表 |
| Memory Sharing（扩展）| KnowledgeSharer 跨 Agent 广播 |

2）**三层学习闭环**：任务完成后自动触发
- 经验提取：LLM 分析任务结果，提取教训和模式
- 反思分析：LLM 深度分析根本原因和改进方向
- 技能学习：识别可复用技能，维护 use_count 和 success_rate

3）**下一次对话自动利用历史经验**：
- `GetRelevantMemoriesWithQuery()` 四维检索：短期 + 向量语义 Top5 + 长期 + 共享
- 检索结果注入 system prompt，LLM 自动参考历史经验

4）**知识共享**：一个 Agent 学到的经验自动广播给所有其他 Agent

可以展示的代码：`pkg/memory/self_improvement.go` 中的 `ProcessTaskResult()` 和 `GetRelevantMemoriesWithQuery()`

### 亮点 3：PostgreSQL + pgvector 向量检索（1.5 分钟）

**【建议配图：向量检索流程图】**

讲解要点：
- PostgreSQL 16 + pgvector 0.8.0 源码编译部署
- 1536 维向量存储，IVFFlat 索引做余弦相似度近似检索
- 应用层自动管理索引生命周期：
  - 启动时检测数据量，`lists = sqrt(行数)` 自适应
  - 数据增长超 4 倍自动重建索引
- 检索 SQL：`SELECT ... 1 - (embedding <=> $1::vector) AS similarity`

可以展示的代码：`pkg/database/vector_index.go` 中的 `EnsureVectorIndexes()`

### 亮点 4：DAG 工作流引擎（1 分钟）

讲解要点：
- 拓扑排序确定执行层次，同层步骤并行执行
- 支持条件分支（`step.Condition`）、重试（指数退避）、超时
- 预置模板：外包项目交付流程（需求分析 → 架构设计 → 前后端并行 → 测试 → 部署）
- 每个步骤绑定到对应角色的 Agent，通过 LLM 执行

可以展示的代码：`pkg/workflow/engine.go`

### 亮点 5：安全体系（1 分钟）

讲解要点：
- JWT 认证：HS256 签名，可选认证模式（有 Token 验证，无 Token 匿名）
- Token 配额管理：分钟/小时/天三级限制，防止滥用
- PII 脱敏：正则检测手机号/身份证/银行卡/邮箱，LLM 输入输出双向过滤
- 审计日志：所有 HTTP 请求自动记录到数据库
- 速率限制：滑动窗口算法
- 输入过滤：XSS/SQL 注入检测

### 亮点 6：LLM 双模式（0.5 分钟）

讲解要点：
- 统一接口支持云端 API（DeepSeek/OpenAI）和本地 Ollama
- Auto 模式自动优先本地推理、失败回退云端
- Ollama 直接调用原生 `/api/chat` 接口
- 支持 DeepSeek 蒸馏等任意本地模型

---

## 四、测试覆盖（1 分钟）

**71 个单元测试**全部通过，覆盖：

| 模块 | 测试数 | 关键测试点 |
|------|--------|-----------|
| 自我迭代系统 | 25 | Agent ID 管理、知识广播、语义搜索、固化策略 |
| 安全体系 | 21 | PII 脱敏、Token 配额、JWT、速率限制、XSS/SQL 注入 |
| 工作流引擎 | 11 | 线性/并行/条件执行、重试、循环依赖检测 |
| A2A 协议 | 8 | Agent 注册/发现、P2P 通信、协作会话 |
| Docker 沙箱 | 5 | 配置生成、安全参数、沙箱模板 |

---

## 五、项目数据（0.5 分钟）

- Go 代码量：约 8000+ 行
- 数据库表：10 张（agents, tasks, knowledge_base, inference_metrics, audit_log 等）
- Prometheus 指标：24+ 维度
- Grafana 面板：18 个
- API 端点：20+ 个
- 测试用例：71 个

---

## 六、总结和未来规划（1 分钟）

项目当前是 v1.2 版本，核心功能已经完备。未来的方向：

- **可视化编排**：React Flow 拖拽式工作流设计
- **链路追踪**：OpenTelemetry 分布式追踪
- **更多 LLM**：Claude 集成、模型能力评估
- **高可用部署**：Orchestrator 集群化

感谢观看，代码已开源，欢迎 Star 和 PR。

---

# 关键代码展示清单

录视频时建议在以下代码位置停留讲解：

1. `pkg/sandbox/docker_sandbox.go` - `buildDockerRunArgs()` 展示安全参数拼接
2. `pkg/memory/self_improvement.go` - `ProcessTaskResult()` 展示三层闭环触发
3. `pkg/memory/self_improvement.go` - `GetRelevantMemoriesWithQuery()` 展示四维记忆检索
4. `pkg/database/vector_index.go` - `EnsureVectorIndexes()` 展示索引自管理
5. `pkg/workflow/engine.go` - `Run()` + `topologicalSort()` 展示 DAG 并行执行
6. `pkg/security/pii.go` - `Sanitize()` 展示 PII 脱敏
7. `pkg/security/quota.go` - `CheckQuota()` 展示配额检查
8. `pkg/message/a2a_protocol.go` - `RequestCollab()` 展示协作会话
9. `cmd/orchestrator/main.go` - `chat()` 展示完整请求链路（配额检查 → PII → 记忆注入 → LLM → 脱敏 → 响应）
