# AI Corp 技术亮点速查卡

> 配合 Bilibili 视频使用，快速回顾每个技术点的核心信息。

---

## 1. Docker 任务沙箱隔离

| 维度 | 实现 |
|------|------|
| 参考架构 | E2B (Firecracker) / Daytona (Docker) |
| 安全措施 | `--cap-drop=ALL`, seccomp, `--no-new-privileges`, `--network none` |
| 资源限制 | 内存(256MB-1GB)、CPU(0.5-2核)、进程数、硬超时 |
| 沙箱模板 | 代码执行、网页抓取、数据处理 |
| 代码位置 | `pkg/sandbox/docker_sandbox.go` |

---

## 2. AI 自我迭代（仿 MemGPT）

| 维度 | 实现 |
|------|------|
| 参考架构 | MemGPT/Letta Memory Blocks + Reflexion |
| 记忆层次 | 短期(内存TTL=1h) → 长期(DB) → 共享(跨Agent广播) |
| 学习闭环 | 经验提取 → 反思分析 → 技能学习 → 知识共享 |
| 语义检索 | pgvector `<=>` 余弦距离 Top5 |
| 记忆注入 | `GetRelevantMemoriesWithQuery()` → system prompt |
| 关键技术 | `sync.RWMutex` 保护 Agent ID、`sync/atomic` 固化计数 |
| 代码位置 | `pkg/memory/self_improvement.go` |

---

## 3. PostgreSQL + pgvector

| 维度 | 实现 |
|------|------|
| 版本 | PostgreSQL 16 + pgvector 0.8.0（源码编译） |
| 向量维度 | 1536 维 |
| 索引类型 | IVFFlat (vector_cosine_ops) |
| 自管理 | `lists = sqrt(rows)`, 增长4倍自动重建 |
| 部署 | 一键脚本 `deploy/postgresql/setup-pgvector.sh` |
| 代码位置 | `pkg/database/vector_index.go` |

---

## 4. DAG 工作流引擎

| 维度 | 实现 |
|------|------|
| 调度算法 | 拓扑排序 → 层次并行执行 |
| 特性 | 条件分支、重试(指数退避)、超时、上下文传播 |
| 模板 | 外包项目交付(6步骤，前后端并行) |
| REST API | `POST /api/v1/workflows`, `POST /api/v1/workflows/:id/run` |
| 代码位置 | `pkg/workflow/engine.go` |

---

## 5. 安全体系

| 组件 | 实现 | 代码位置 |
|------|------|---------|
| JWT 认证 | HS256, 可选认证模式 | `pkg/security/middleware.go` |
| Token 配额 | 分钟/小时/天三级限制 | `pkg/security/quota.go` |
| PII 脱敏 | 手机/身份证/银行卡/邮箱 | `pkg/security/pii.go` |
| 审计日志 | HTTP 中间件 + DB 持久化 | `pkg/security/audit_middleware.go` |
| 速率限制 | 滑动窗口算法 | `pkg/security/middleware.go` |
| 输入过滤 | XSS/SQL 注入检测 | `pkg/security/middleware.go` |

---

## 6. LLM 双模式

| 维度 | 实现 |
|------|------|
| 云端 | DeepSeek API / OpenAI / Claude |
| 本地 | Ollama (原生 `/api/chat` 接口) |
| 策略 | Auto: 本地优先 → 云端回退 |
| 代码位置 | `pkg/llm/dual_mode.go` |

---

## 7. A2A 协议

| 维度 | 实现 |
|------|------|
| P2P 通信 | `SendDirect()` 点对点消息 |
| 协作类型 | 代码审查、请求协助、任务交接、技术咨询、广播 |
| Agent 发现 | 按能力/类型查找在线 Agent |
| 会话管理 | pending → active → completed |
| 代码位置 | `pkg/message/a2a_protocol.go` |

---

## 8. 监控体系

| 维度 | 实现 |
|------|------|
| 指标采集 | Prometheus client_golang, 24+ 维度 |
| 系统资源 | gopsutil 每 5 秒采集 CPU/内存/网络 |
| 可视化 | Grafana 18 面板 |
| 端点 | `GET /metrics` (Prometheus), `GET /api/v1/metrics` (JSON) |

---

## 项目统计

```
代码量:      8000+ 行 Go 代码
数据库表:    10 张
API 端点:    20+ 个
测试用例:    71 个（全部通过）
Prometheus:  24+ 指标
Grafana:     18 面板
```
