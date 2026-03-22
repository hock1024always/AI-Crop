# AI Corp 技术架构文档

本文档详细描述 AI Corp 多智能体协作平台的技术架构、核心模块设计和实现细节。

---

## 1. 系统概述

AI Corp 是一个多智能体协作平台，采用"公司"隐喻，将 AI Agent 模拟为不同职能的员工（研发、测试、架构、运维），通过 Orchestrator 总控协调，实现任务的自动分解、分配和协作完成。

### 1.1 设计理念

- **角色分工**：不同类型的 Agent 具有不同的技能和职责
- **总控协调**：所有 Agent 通信必须经过 Orchestrator，便于监控和管理
- **沙箱隔离**：每个 Agent 在独立的环境中执行，保证安全性
- **可视化交互**：像素风 UI，直观展示 Agent 状态和任务进度

### 1.2 核心特性

| 特性 | 描述 |
|------|------|
| 多智能体协作 | Developer、Tester、Architect、DevOps 四种角色 |
| 实时通信 | WebSocket 双向通信，状态实时同步 |
| MCP 工具系统 | 可扩展的 Skill 工具集 |
| RAG 知识库 | 向量检索，智能提示 |
| 编译器插件 | 支持多语言编译，插件化架构 |
| 监控体系 | Prometheus 指标，实时监控 |

---

## 2. 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        前端可视化层                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │ Agent画布   │  │ 实时日志    │  │ Agent间通信可视化      │  │
│  │ (像素风UI)  │  │ (WebSocket) │  │ (任务看板/监控面板)    │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    │ WebSocket / REST API
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Orchestrator 总控层                           │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ Agent 管理       │ 任务调度       │ 消息路由             │    │
│  │ - 注册/注销      │ - 创建/分配    │ - WebSocket 广播     │    │
│  │ - 状态监控       │ - 进度跟踪     │ - NATS Pub/Sub       │    │
│  │ - 心跳检测       │ - 结果收集     │ - 事件分发           │    │
│  └─────────────────────────────────────────────────────────┘    │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ REST API        │ WebSocket      │ Metrics              │    │
│  │ :8080/api/v1/*  │ :8080/ws       │ :8080/metrics        │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    │ NATS / WebSocket
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Agent 运行时层                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐  │
│  │ Developer Agent  │  │ Tester Agent     │  │ Architect/    │  │
│  │ - 代码生成       │  │ - 测试生成       │  │ DevOps Agent  │  │
│  │ - 代码审查       │  │ - 质量检查       │  │ - 系统设计    │  │
│  │ - 调试修复       │  │ - 覆盖率分析     │  │ - 部署运维    │  │
│  └──────────────────┘  └──────────────────┘  └───────────────┘  │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ MCP Server (端口 8081+)                                  │    │
│  │ - Skill 注册与执行                                       │    │
│  │ - LLM 调用封装                                           │    │
│  │ - RAG 工具集成                                           │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────┐
│                      基础设施层                                  │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────┐  │
│  │ NATS       │  │ Vector     │  │ Compiler   │  │ Docker    │  │
│  │ 消息队列   │  │ Store      │  │ Plugins    │  │ Sandbox   │  │
│  │            │  │            │  │            │  │           │  │
│  │ JetStream  │  │ Chroma/    │  │ LLVM/GCC/  │  │ go-judge  │  │
│  │            │  │ Milvus     │  │ Go         │  │ 隔离执行  │  │
│  └────────────┘  └────────────┘  └────────────┘  └───────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. 核心模块设计

### 3.1 Orchestrator 总控服务

**职责**：
- Agent 生命周期管理（注册、心跳、注销）
- 任务创建、分配、进度跟踪
- WebSocket 连接管理和消息广播
- REST API 服务
- 监控指标暴露

**关键数据结构**：

```go
type Orchestrator struct {
    agents    map[string]*Agent      // 已注册的 Agent
    tasks     map[string]*Task       // 任务列表
    wsClients map[*websocket.Conn]bool // WebSocket 客户端
    broadcast chan WSMessage         // 广播通道
    taskQueue chan *Task             // 任务队列
}

type Agent struct {
    ID        string   `json:"id"`
    Name      string   `json:"name"`
    Type      string   `json:"type"`      // developer/tester/architect/devops
    Status    string   `json:"status"`    // idle/busy/offline
    Skills    []string `json:"skills"`
}

type Task struct {
    ID          string                 `json:"id"`
    Title       string                 `json:"title"`
    Status      string                 `json:"status"`    // pending/running/completed/failed
    AssignedTo  string                 `json:"assigned_to"`
    Result      map[string]interface{} `json:"result"`
}
```

**API 端点**：

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/agents` | 列出所有 Agent |
| POST | `/api/v1/agents` | 创建 Agent |
| GET | `/api/v1/tasks` | 列出所有任务 |
| POST | `/api/v1/tasks` | 创建任务 |
| POST | `/api/v1/tasks/:id/assign` | 分配任务 |
| GET | `/metrics` | Prometheus 指标 |
| GET | `/ws` | WebSocket 连接 |

### 3.2 Agent Runtime 运行时

**职责**：
- 连接 Orchestrator 并注册
- 接收任务并执行
- 报告进度和结果
- 暴露 MCP Server

**关键数据结构**：

```go
type Runtime struct {
    ID       string
    Name     string
    Type     string              // developer/tester/architect/devops

    bus      message.MessageBus  // 消息总线
    registry *skill.Registry     // Skill 注册表
    state    *State              // 运行状态
}

type State struct {
    Status      string // idle/busy
    CurrentTask string
    Stats       map[string]interface{}
}
```

**任务执行流程**：

```
1. 接收任务 (WebSocket/NATS)
   ↓
2. 解析任务类型
   ↓
3. 选择执行方式
   ├── skill: 调用 Skill 注册表
   ├── llm:   调用 LLM API
   └── composite: 分解为子任务
   ↓
4. 报告进度 (定期)
   ↓
5. 报告结果 (成功/失败)
```

### 3.3 MCP 工具系统

**职责**：
- 定义和注册 Skill
- 执行 Skill 并返回结果
- 提供 LLM 调用封装

**Skill 接口**：

```go
type Skill struct {
    Name        string
    Description string
    InputSchema map[string]interface{}
    Handler     SkillHandler
}

type SkillHandler func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
```

**内置 Skills**：

| Skill | 描述 | 输入 |
|-------|------|------|
| `code_generation` | 生成代码 | language, requirement |
| `code_review` | 代码审查 | code, language |
| `debug` | 调试代码 | code, error |
| `test_generation` | 生成测试 | code, test_type |
| `system_design` | 系统设计 | requirements |
| `deploy` | 部署应用 | artifact, environment |

### 3.4 消息总线

**职责**：
- Agent 间消息传递
- 支持发布-订阅和请求-响应模式
- 消息持久化（JetStream）

**消息类型**：

```go
type MessageType string

const (
    MessageTypeTaskAssign   MessageType = "task.assign"
    MessageTypeTaskProgress MessageType = "task.progress"
    MessageTypeTaskComplete MessageType = "task.complete"
    MessageTypeTaskFail     MessageType = "task.fail"
    MessageTypeHeartbeat    MessageType = "heartbeat"
    MessageTypeAgentJoin    MessageType = "agent.join"
)
```

**消息格式**：

```go
type Message struct {
    ID        string                 `json:"id"`
    Type      MessageType            `json:"type"`
    From      string                 `json:"from"`
    To        string                 `json:"to"`
    TaskID    string                 `json:"task_id"`
    Content   map[string]interface{} `json:"content"`
    Timestamp int64                  `json:"timestamp"`
}
```

### 3.5 RAG 服务

**职责**：
- 题目向量化存储
- 相似题目检索
- 多级解题提示

**架构**：

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ 题库 YAML    │ ──→ │ Embedding    │ ──→ │ Vector Store │
│ (标准格式)   │     │ (DeepSeek)   │     │ (Memory/     │
│              │     │              │     │  Chroma)     │
└──────────────┘     └──────────────┘     └──────────────┘
                                                  │
                                                  ▼
                     ┌──────────────────────────────────────┐
                     │ MCP Tools                            │
                     │ - search_similar_problems            │
                     │ - get_hint (level 1-3)               │
                     │ - explain_pattern                    │
                     └──────────────────────────────────────┘
```

**题目格式**：

```yaml
id: "001"
title: "两数之和"
difficulty: easy
tags: [array, hash-table]

description: |
  给定一个整数数组...

keywords: [数组, 哈希表, 查找]
solution_patterns:
  - pattern: "哈希表一次遍历"
    hint: "使用哈希表存储..."
    complexity: "O(n) 时间, O(n) 空间"
```

### 3.6 编译器插件系统

**职责**：
- 支持多语言编译
- 插件化架构，易于扩展
- 沙箱隔离执行

**接口定义**：

```go
type CompilerPlugin interface {
    Name() string
    Version() string
    SupportedLanguages() []string
    OptimizationLevels() []string
    Compile(ctx context.Context, req *CompileRequest) (*CompileResult, error)
    Validate() error
}
```

**已实现编译器**：

| 编译器 | 支持语言 | 优化级别 |
|--------|----------|----------|
| LLVM/Clang | C, C++ | -O0, -O1, -O2, -O3, -Os, -Oz |
| GCC | C, C++ | -O0, -O1, -O2, -O3, -Os |
| Go | Go | - |

**接入流程**：

```
1. 创建 pkg/compiler/<name>/ 目录
2. 实现 CompilerPlugin 接口
3. 添加配置到 configs/compilers.yaml
4. 调用 compiler.Register() 注册
5. 提交 PR
```

### 3.7 监控指标

**职责**：
- 采集系统运行指标
- 暴露 Prometheus 端点
- 提供实时监控数据

**指标类型**：

| 类别 | 指标 | 描述 |
|------|------|------|
| Token | `llm_tokens_input_total` | 输入 Token 总数 |
| Token | `llm_tokens_output_total` | 输出 Token 总数 |
| Token | `llm_cost_usd` | 估算费用 |
| 执行 | `execution_total` | 执行总次数 |
| 执行 | `execution_avg_latency_ms` | 平均延迟 |
| 执行 | `execution_max_memory_mb` | 最大内存 |
| 容器 | `container_cpu_pct` | CPU 使用率 |
| 容器 | `container_memory_mb` | 内存使用 |
| Agent | `agent_tasks_completed` | 完成任务数 |

---

## 4. 部署架构

### 4.1 单机部署

```yaml
# docker-compose.yml
version: '3.8'
services:
  nats:
    image: nats:latest
    ports:
      - "4222:4222"

  orchestrator:
    build: .
    ports:
      - "8080:8080"
    depends_on:
      - nats

  agent-dev:
    build: .
    environment:
      - AGENT_ID=dev-1
      - AGENT_TYPE=developer
    depends_on:
      - orchestrator
```

### 4.2 Kubernetes 部署

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ai-corp

---
# orchestrator.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orchestrator
  namespace: ai-corp
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: orchestrator
        image: ai-corp/orchestrator:latest
        ports:
        - containerPort: 8080
```

**资源隔离**：

```yaml
# ResourceQuota
resources:
  limits:
    cpu: "2"
    memory: "4Gi"
  requests:
    cpu: "500m"
    memory: "1Gi"
```

---

## 5. 前端设计

### 5.1 技术栈

- **纯 HTML/CSS/JS**：零依赖，直接浏览器运行
- **像素风样式**：Press Start 2P 字体，pixel-border 效果
- **WebSocket**：实时状态同步

### 5.2 页面结构

```
┌─────────────────────────────────────────────────────────────┐
│  TOP BAR: Logo | Connection Status | Clock                  │
├─────────────┬───────────────────────────┬───────────────────┤
│  OFFICE     │  WORKSPACE                │  SIDEBAR          │
│  ┌─────┐    │  ┌─────────────────────┐  │  ┌─────────────┐  │
│  │ 👨‍💻 │    │  │ TASK BOARD          │  │  │ AGENT INFO  │  │
│  │dev-1│    │  │ [Backlog][Prog][Done│  │  │ ID: dev-1   │  │
│  └─────┘    │  └─────────────────────┘  │  │ Status: idle│  │
│  ┌─────┐    │  ┌─────────────────────┐  │  └─────────────┘  │
│  │ 🧪 │    │  │ TERMINAL            │  │  ┌─────────────┐  │
│  │test │    │  │ > create task...    │  │  │ MCP TOOLS   │  │
│  └─────┘    │  │ [Agent response]    │  │  │ code_gen    │  │
│  [+ ADD]    │  └─────────────────────┘  │  │ debug       │  │
│             │                           │  └─────────────┘  │
│             │                           │  ┌─────────────┐  │
│             │                           │  │ MONITOR     │  │
│             │                           │  │ CPU ████ 12%│  │
│             │                           │  │ MEM ███  34%│  │
│             │                           │  └─────────────┘  │
└─────────────┴───────────────────────────┴───────────────────┘
```

---

## 6. 安全设计

### 6.1 沙箱隔离

- **Docker 容器**：每个 Agent 在独立容器中运行
- **资源限制**：CPU、内存、进程数限制
- **网络隔离**：禁用网络访问（可选）
- **go-judge**：代码执行沙箱

### 6.2 通信安全

- **CORS 配置**：限制允许的来源
- **WebSocket 鉴权**：Token 验证（待实现）
- **TLS 加密**：生产环境启用 HTTPS

---

## 7. 性能优化

### 7.1 并发处理

- **Goroutine 池**：限制并发任务数
- **Channel 缓冲**：消息队列缓冲
- **连接复用**：HTTP Keep-Alive

### 7.2 资源管理

- **内存池**：减少 GC 压力
- **连接池**：数据库/NATS 连接复用
- **缓存**：热点数据缓存

---

## 8. 扩展性

### 8.1 水平扩展

- **Orchestrator 无状态**：可多实例部署
- **NATS 集群**：消息队列高可用
- **Agent 弹性伸缩**：根据负载动态增减

### 8.2 插件扩展

- **编译器插件**：支持新语言
- **MCP 工具**：自定义 Skill
- **LLM 后端**：支持多种模型

---

## 9. 监控与运维

### 9.1 健康检查

```bash
curl http://localhost:8080/health
# {"status":"ok","timestamp":1234567890}
```

### 9.2 Prometheus 集成

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'ai-corp'
    static_configs:
      - targets: ['localhost:8080']
```

### 9.3 日志

- 结构化 JSON 日志
- 日志级别：DEBUG、INFO、WARN、ERROR
- 日志轮转和归档

---

## 10. 未来规划

详见 [ROADMAP.md](ROADMAP.md)

- **Phase 1**：Agent 沙箱增强（PII 脱敏、预算控制、审计日志）
- **Phase 2**：DAG 工作流引擎、A2A 协议
- **Phase 3**：可视化编排、实时更新
