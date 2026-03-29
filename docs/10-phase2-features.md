# AI Corp 核心功能技术说明

## Docker 任务沙箱隔离系统

### 概述

基于业界最佳实践（E2B、Daytona、Open Interpreter），实现了完整的 Docker 任务沙箱隔离系统，确保 AI 员工执行任务时的安全性和资源隔离。

### 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                    SandboxManager                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐         │
│  │  Task #1    │  │  Task #2    │  │  Task #3    │         │
│  │  Sandbox    │  │  Sandbox    │  │  Sandbox    │         │
│  │ ┌─────────┐ │  │ ┌─────────┐ │  │ ┌─────────┐ │         │
│  │ │Container│ │  │ │Container│ │  │ │Container│ │         │
│  │ │ 512MB   │ │  │ │ 1GB     │ │  │ │ 2GB     │ │         │
│  │ │ 0.5 CPU │ │  │ │ 1 CPU   │ │  │ │ 2 CPU   │ │         │
│  │ │ No Net  │ │  │ │ No Net  │ │  │ │ Net OK  │ │         │
│  │ └─────────┘ │  │ └─────────┘ │  │ └─────────┘ │         │
│  └─────────────┘  └─────────────┘  └─────────────┘         │
└─────────────────────────────────────────────────────────────┘
```

### 核心特性

#### 1. 资源隔离

| 资源类型 | 配置项 | 说明 |
|---------|--------|------|
| 内存 | `--memory` | 限制容器内存使用，防止内存泄漏影响宿主机 |
| CPU | `--cpu-quota`, `--cpus` | 限制 CPU 使用率，防止单任务占满 CPU |
| 进程数 | `--pids-limit` | 限制进程数，防止 fork 炸弹 |
| 网络 | `--network none` | 默认禁用网络，防止恶意外连 |

#### 2. 安全加固

```go
// 安全配置示例
args := []string{
    "--no-new-privileges",           // 禁止提权
    "--security-opt", "no-new-privileges:true",
    "--cap-drop=ALL",                // 移除所有能力
    "--pids-limit", "256",           // 限制进程数
    "--network", "none",             // 禁用网络
}
```

#### 3. 预定义沙箱模板

```go
// 代码执行沙箱 - 适合运行用户代码
CodeExecutionSandbox() // 1GB 内存, 1 CPU, 无网络

// 网页抓取沙箱 - 适合需要网络的任务
WebScraperSandbox() // 512MB 内存, 0.5 CPU, 白名单网络

// 数据处理沙箱 - 适合计算密集型任务
DataProcessingSandbox() // 2GB 内存, 2 CPU, 无网络
```

### 使用示例

```go
// 创建沙箱管理器
sm, _ := sandbox.NewSandboxManager(nil)

// 创建沙箱
sandbox, _ := sm.CreateSandbox(ctx, "task-001", "python:3.11-slim",
    sandbox.CodeExecutionSandbox())

// 执行命令
output, exitCode, _ := sm.ExecuteInSandbox(ctx, sandbox.ID,
    []string{"python3", "-c", "print('Hello')"})

// 执行脚本
output, exitCode, _ = sm.ExecuteScript(ctx, sandbox.ID,
    "import os; print(os.listdir('/'))", "python")

// 清理
sm.StopSandbox(sandbox.ID)
```

### 数据库 Schema

```sql
CREATE TABLE sandbox_executions (
    id              VARCHAR(128) PRIMARY KEY,
    task_id         VARCHAR(128) NOT NULL,
    container_id    VARCHAR(128),
    image           VARCHAR(256) NOT NULL,
    status          VARCHAR(32),  -- creating, running, completed, failed, timeout
    memory_mb       INTEGER DEFAULT 512,
    cpu_quota       INTEGER DEFAULT 50000,
    network_enabled BOOLEAN DEFAULT false,
    created_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);
```

---

## AI 自我迭代与经验共享系统

### 概述

参考 MemGPT/Letta 的 Memory Blocks 和 Reflexion 框架，实现了完整的 AI 自我迭代与经验共享机制。

### 架构设计

```
┌─────────────────────────────────────────────────────────────────┐
│                    Self-Improvement Loop                         │
│                                                                   │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │   Task       │───▶│  Experience  │───▶│   Memory     │       │
│  │   Result     │    │  Extractor   │    │   Manager    │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│         │                                         │               │
│         ▼                                         ▼               │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │  Reflection  │───▶│    Skill     │───▶│   Knowledge  │       │
│  │   Engine     │    │   Learner    │    │    Sharer    │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
        ┌─────────────────────────────────────────┐
        │            Memory Store (PostgreSQL)     │
        │  ┌─────────┐ ┌─────────┐ ┌─────────┐   │
        │  │ Memory  │ │  Exp    │ │  Skill  │   │
        │  │ Blocks  │ │ Records │ │   DB    │   │
        │  └─────────┘ └─────────┘ └─────────┘   │
        └─────────────────────────────────────────┘
```

### 核心组件

#### 1. Memory Types（记忆类型）

| 类型 | 说明 | 持久性 | 用途 |
|------|------|--------|------|
| `short_term` | 短期记忆 | 1小时 | 当前工作上下文 |
| `long_term` | 长期记忆 | 永久 | 重要经验固化 |
| `reflection` | 反思记忆 | 永久 | 自我改进分析 |
| `skill` | 技能记忆 | 永久 | 动态学习的技能 |
| `shared` | 共享记忆 | 永久 | Agent 间传递 |

#### 2. Experience Extraction（经验提取）

```go
// 从任务结果自动提取经验
type Experience struct {
    TaskID      string    // 关联任务
    TaskType    string    // 任务类型
    Success     bool      // 是否成功
    Lessons     []string  // 学到的教训
    Patterns    []string  // 识别的模式
    Suggestions []string  // 改进建议
    Confidence  float64   // 置信度
}
```

LLM Prompt 示例：
```
请从以下任务执行中提取可复用的经验：
任务类型: code_gen
执行状态: 成功
输入: {用户请求}
输出: {生成的代码}

请以 JSON 格式输出：
{
  "lessons": ["教训1", "教训2"],
  "patterns": ["发现的模式"],
  "suggestions": ["改进建议"],
  "confidence": 0.85
}
```

#### 3. Reflection Engine（反思引擎）

```go
// 对任务进行深度反思
type Reflection struct {
    TriggerTaskID string          // 触发任务
    TriggerType   string          // success/failure/timeout
    Analysis      string          // 分析过程
    Insights      []string        // 洞察
    ActionItems   []string        // 行动项
    SkillLearned  *SkillDefinition // 学到的新技能
    Confidence    float64
}
```

反思流程：
1. 分析任务执行过程
2. 识别成功/失败的根本原因
3. 提取可复用的模式和教训
4. 生成改进行动项
5. 动态学习新技能（如适用）

#### 4. Knowledge Sharing（知识共享）

```go
// 共享经验给其他 Agent
sharer.ShareExperience(ctx, exp, []string{"agent-2", "agent-3"})

// 共享技能给其他 Agent
sharer.ShareSkill(ctx, skill, []string{"agent-2", "agent-3"})
```

### 使用示例

```go
// 创建自我改进循环
sil := memory.NewSelfImprovementLoop(store, llmClient,
    []string{"agent-1", "agent-2", "agent-3"})

// 处理任务结果（自动触发完整流程）
result := &memory.TaskResult{
    TaskID:     "task-001",
    TaskType:   "code_gen",
    Success:    true,
    TokensUsed: 150,
    LatencyMs:  1200,
}
sil.ProcessTaskResult(ctx, "agent-1", result)

// 获取相关记忆用于任务执行
memories, _ := sil.GetRelevantMemories(ctx, "agent-1", "code_gen")
```

### 数据库 Schema

```sql
-- 记忆块存储
CREATE TABLE agent_memory (
    id          VARCHAR(128) PRIMARY KEY,
    agent_id    VARCHAR(128),
    type        VARCHAR(32),     -- short_term, long_term, reflection, skill, shared
    title       VARCHAR(256),
    content     TEXT,
    importance  DOUBLE PRECISION,
    embedding   vector(1536),    -- 向量嵌入
    expires_at  TIMESTAMPTZ      -- 过期时间
);

-- 经验记录
CREATE TABLE agent_experiences (
    id          VARCHAR(128) PRIMARY KEY,
    agent_id    VARCHAR(128),
    task_id     VARCHAR(128),
    success     BOOLEAN,
    lessons     JSONB,           -- ["教训1", "教训2"]
    patterns    JSONB,
    confidence  DOUBLE PRECISION
);

-- 反思记录
CREATE TABLE agent_reflections (
    id              VARCHAR(128) PRIMARY KEY,
    agent_id        VARCHAR(128),
    trigger_task_id VARCHAR(128),
    analysis        TEXT,
    insights        JSONB,
    skill_learned   JSONB,       -- 动态学习的技能
    confidence      DOUBLE PRECISION
);

-- 技能定义
CREATE TABLE agent_skills (
    id           VARCHAR(128) PRIMARY KEY,
    name         VARCHAR(128) UNIQUE,
    description  TEXT,
    template     TEXT,           -- 执行模板
    success_rate DOUBLE PRECISION,
    use_count    INTEGER,
    source       VARCHAR(32)     -- learned, predefined
);
```

### 自我改进流程

```
任务完成
    │
    ▼
┌─────────────────┐
│ 经验提取        │ ─── 存储到 agent_experiences
│ - 教训          │
│ - 模式          │
│ - 建议          │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 反思分析        │ ─── 存储到 agent_reflections
│ - 根本原因      │
│ - 洞察          │
│ - 行动项        │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 技能学习        │ ─── 存储到 agent_skills
│ - 新技能识别    │
│ - 模板生成      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ 知识共享        │ ─── 共享给其他 Agent
│ - 经验共享      │
│ - 技能共享      │
└─────────────────┘
```

---

## 技术亮点总结

### 亮点一：Docker 任务沙箱隔离

> 实现基于 Docker 的任务沙箱隔离系统，支持内存/CPU/网络/进程多维度资源限制，通过 `--cap-drop=ALL`、`--no-new-privileges` 等安全加固确保 AI 任务执行的安全性。

**技术细节**：
- 参考 E2B（Firecracker 微虚拟机）和 Daytona（Docker 容器）架构设计
- 支持代码执行、网页抓取、数据处理三种预定义沙箱模板
- 实现硬超时、网络白名单、只读挂载等多层安全措施
- `cleanupOnce` 保证资源清理幂等，防止容器泄露

### 亮点二：AI 自我迭代与经验共享

> 设计并实现 AI Agent 自我迭代机制，通过经验提取、反思分析、技能学习三层闭环，实现 Agent 自动从任务结果中学习并共享知识给其他 Agent。

**技术细节**：
- 参考 MemGPT/Letta 的 Memory Blocks 分层模型（Core/Archival/Recall Memory）
- 五种记忆类型：短期（TTL 1h）、长期（持久化）、反思、技能、共享
- 完整的自我改进循环：经验提取 -> 反思分析 -> 技能学习 -> 知识共享
- `sync.RWMutex` 保护 Agent ID 列表，`sync/atomic` 实现原子化固化计数
- 关键记忆块生成 Embedding 向量，支持 pgvector 语义相似度检索

### 亮点三：PostgreSQL + pgvector 向量检索

> 基于 PostgreSQL 16 + pgvector 0.8.0 实现 RAG 知识库，IVFFlat 索引支持大规模向量余弦相似度检索，应用层自动管理索引生命周期。

**技术细节**：
- 一键部署脚本：PG16 + pgvector 0.8.0 源码编译
- `lists = sqrt(rows)` 自适应索引参数
- 数据量增长 4 倍自动重建索引
- 双路检索：向量语义 Top5 + 传统按 agent/type 过滤

### 亮点四：LLM 双模式推理

> 统一接口支持云端 API（DeepSeek/OpenAI）和本地 Ollama 部署，`Auto` 模式自动 Fallback，支持 DeepSeek 蒸馏等任意本地模型。

**技术细节**：
- Ollama 原生 `/api/chat` 接口调用（非 OpenAI 兼容层）
- 云端多 Provider 优先级 Fallback：deepseek > openai > claude
- `InferenceService` 统一计时、Token 统计、Prometheus 指标上报

---

## 文件清单

```
pkg/
├── sandbox/
│   ├── docker_sandbox.go        # 沙箱核心实现
│   └── docker_sandbox_test.go   # 单元测试
├── memory/
│   ├── self_improvement.go      # 自我迭代核心实现
│   ├── postgres_store.go        # PostgreSQL 存储实现
│   └── self_improvement_test.go # 单元测试
deploy/postgresql/
└── memory_schema.sql            # 数据库 Schema 扩展
```

## 测试覆盖

```
pkg/sandbox/docker_sandbox_test.go
- TestDefaultSandboxConfig        PASS
- TestCodeExecutionSandbox        PASS
- TestWebScraperSandbox           PASS
- TestDataProcessingSandbox       PASS
- TestBuildDockerRunArgs          PASS

pkg/memory/self_improvement_test.go
- TestMemoryBlockTypes            PASS
- TestMemoryBlockExpiration       PASS
- TestExperienceStructure         PASS
- TestReflectionStructure         PASS
- TestSkillDefinition             PASS
- TestMemoryManagerAddShortTerm   PASS
- TestMemoryManagerLimit          PASS
- TestMemoryManagerConsolidation  PASS
- TestBuildReflectionPrompt       PASS
- TestParseReflectionResponse     PASS
- TestBuildExtractionPrompt       PASS
- TestParseExperienceResponse     PASS
- TestShareExperience             PASS
- TestShareSkill                  PASS
- TestSelfImprovementLoop         PASS
- TestAgentIDManagement           PASS
- TestKnowledgeBroadcast          PASS
- TestSemanticSearch              PASS
- TestConsolidateByTaskCount      PASS
- TestEmbeddingOnShareExperience  PASS
- TestEmbeddingOnShareSkill       PASS
- TestProcessTaskResultEmbedding  PASS
- TestRelevantMemoriesWithQuery   PASS
- TestEmbeddingClientAdapter      PASS
- TestSelfImprovementLoopCreation PASS
```
