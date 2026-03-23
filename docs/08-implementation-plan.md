# AI Infra 功能实现计划

> 基于 Cube Studio 和 vLLM-Kunlun 分析，制定可执行的实现路线图

## 一、总体目标

将 AI Corp 从简单的 Agent 协作平台升级为完整的 AI Infra 平台，具备：
- 真实模型推理能力
- 可视化工作流编排
- 完善的监控告警
- 模型全生命周期管理
- 自动扩缩容

---

## 二、分阶段实施计划

### Phase 1: 核心能力补齐 (2-3周)

#### 2.1 集成 vLLM 推理引擎 (Week 1)

**目标**：替换 mock_server，实现真实模型推理

**任务清单**：
- [ ] 1.1 安装 vLLM (Python 依赖)
- [ ] 1.2 部署 DeepSeek 6.7B 模型 (量化版本)
- [ ] 1.3 实现 Go 客户端调用 vLLM API
- [ ] 1.4 配置连续批处理和 PagedAttention
- [ ] 1.5 压力测试 (QPS、延迟、显存)

**技术方案**：
```bash
# 1. 安装 vLLM
pip install vllm

# 2. 下载量化模型
huggingface-cli download TheBloke/deepseek-coder-6.7b-instruct-AWQ

# 3. 启动服务
python -m vllm.entrypoints.api_server \
  --model TheBloke/deepseek-coder-6.7b-instruct-AWQ \
  --quantization awq \
  --max-model-len 4096 \
  --host 0.0.0.0 --port 8000
```

```go
// pkg/llm/vllm_client.go
type VLLMClient struct {
    baseURL string
    client  *http.Client
}

func (c *VLLMClient) Generate(prompt string) (string, error) {
    req := map[string]interface{}{
        "prompt": prompt,
        "max_tokens": 512,
        "temperature": 0.7,
    }
    // 发送 HTTP 请求到 vLLM
}
```

**验收标准**：
- [ ] 单卡推理 QPS ≥ 10
- [ ] TTFT ≤ 500ms
- [ ] 显存占用 ≤ 8GB (AWQ)

#### 2.2 监控指标采集 (Week 1)

**目标**：采集推理性能指标

**任务清单**：
- [ ] 2.1 集成 Prometheus Go 客户端
- [ ] 2.2 暴露核心指标：QPS、延迟、显存、Token 吞吐
- [ ] 2.3 部署 Prometheus + Grafana
- [ ] 2.4 创建基础监控 Dashboard

**指标定义**：
```go
var (
    qpsCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_qps_total",
            Help: "Total queries per second",
        },
        []string{"model"},
    )
    latencyHistogram = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "llm_latency_seconds",
            Help:    "Latency distribution",
            Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
        },
        []string{"model"},
    )
)
```

#### 2.3 数据库迁移准备 (Week 2)

**目标**：为后续功能准备数据存储

**任务清单**：
- [ ] 3.1 安装 PostgreSQL 16
- [ ] 3.2 安装 pgvector 扩展
- [ ] 3.3 设计核心表结构 (agents, tasks, metrics)
- [ ] 3.4 实现 Go ORM 映射 (GORM/pgx)

**表结构**：
```sql
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255),
    type VARCHAR(50),
    config JSONB,
    status VARCHAR(50),
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID REFERENCES agents(id),
    input JSONB,
    output JSONB,
    status VARCHAR(50),
    created_at TIMESTAMP DEFAULT NOW(),
    completed_at TIMESTAMP
);
```

---

### Phase 2: 可视化能力 (3-4周)

#### 2.1 拖拽式工作流编排 (Week 3-4)

**目标**：实现可视化 Agent 协作流程设计

**前端技术栈**：
- React 18 + TypeScript
- ReactFlow (可视化流程图)
- Zustand (状态管理)
- Ant Design (UI 组件)

**任务清单**：
- [ ] 4.1 搭建前端项目结构
- [ ] 4.2 集成 ReactFlow 画布
- [ ] 4.3 实现节点组件 (Agent/Condition/Parallel)
- [ ] 4.4 实现边连接和数据流
- [ ] 4.5 属性面板配置
- [ ] 4.6 工作流保存/加载 (JSON 格式)

**数据结构**：
```typescript
interface WorkflowNode {
  id: string;
  type: 'agent' | 'condition' | 'parallel';
  position: { x: number; y: number };
  data: {
    agentType?: string;
    condition?: string;
    config?: Record<string, any>;
  };
}

interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  data?: {
    condition?: boolean;
  };
}
```

**后端 API**：
```go
type Workflow struct {
    ID          string        `json:"id"`
    Name        string        `json:"name"`
    Nodes       []WorkflowNode `json:"nodes"`
    Edges       []WorkflowEdge `json:"edges"`
    CreatedAt   time.Time     `json:"created_at"`
}

// API Endpoints
POST   /api/workflows          // 创建工作流
GET    /api/workflows          // 获取工作流列表
GET    /api/workflows/{id}     // 获取工作流详情
POST   /api/workflows/{id}/run // 执行工作流
```

#### 2.2 监控 Dashboard (Week 4)

**目标**：可视化展示系统运行状态

**技术方案**：
- Grafana Dashboard
- Prometheus 数据源
- 自定义 Panel (React + Grafana Panel SDK)

**核心面板**：
```
┌─────────────────────────────────────────────────────┐
│  AI Corp 实时监控                                    │
├─────────┬─────────┬─────────────────────────────────┤
│ Agents  │ Tasks   │  LLM Performance                │
│ 24      │ 98.5%   │  QPS: 12.3                      │
│ Online  │ Success │  Avg Latency: 420ms             │
├─────────┴─────────┴─────────────────────────────────┤
│  GPU Utilization                                    │
│  GPU0: ████████░░ 85%  Mem: 12GB/16GB              │
│  GPU1: ██████░░░░ 60%  Mem: 8GB/16GB               │
├─────────────────────────────────────────────────────┤
│  Task Distribution                                  │
│  Frontend: 30%  Backend: 40%  DevOps: 15%  PM: 15% │
└─────────────────────────────────────────────────────┘
```

---

### Phase 3: 高级功能 (4-6周)

#### 3.1 模型版本管理 (Week 5)

**目标**：支持模型上传、版本控制、一键部署

**功能模块**：
- 模型注册 (HuggingFace/本地上传)
- 版本对比 (精度/性能)
- 血缘追踪 (训练任务 → 模型 → 服务)
- 一键部署 (模型 → 推理服务)

**技术方案**：
```go
type Model struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Version     string            `json:"version"`
    Format      string            `json:"format"`  // gguf, safetensors
    StoragePath string            `json:"storage_path"`
    Metrics     map[string]float64 `json:"metrics"`  // accuracy, latency
    Metadata    map[string]string  `json:"metadata"`
}

// 对象存储 (MinIO/S3)
// 元数据存储 (PostgreSQL)
// 索引 (Elasticsearch)
```

#### 3.2 自动扩缩容 (Week 6)

**目标**：基于负载自动调整 Agent 实例数

**技术方案**：
```yaml
# KEDA ScaledObject
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: agent-scaler
spec:
  scaleTargetRef:
    name: ai-agent-deployment
  minReplicaCount: 2
  maxReplicaCount: 20
  triggers:
  - type: prometheus
    metadata:
      serverAddress: http://prometheus:9090
      metricName: queue_depth
      threshold: '10'
      query: queue_depth{service="orchestrator"}
```

**冷却策略**：
- 扩容冷却：30s
- 缩容冷却：300s
- 预测扩容：基于历史负载

---

## 三、技术债务清理

### 重构计划

| 模块 | 当前状态 | 重构目标 | 时间 |
|-----|---------|---------|------|
| pkg/llm | mock 模式 | vLLM 集成 | Week 1 |
| 前端 UI | 像素风格 | React + AntD | Week 3 |
| 数据存储 | 内存结构 | PostgreSQL | Week 2 |
| 部署方式 | 二进制 | Docker Compose | Week 2 |

---

## 四、风险与应对

### 4.1 技术风险

| 风险 | 影响 | 应对措施 |
|-----|------|---------|
| GPU 显存不足 | 推理失败 | 使用量化模型 (AWQ/GPTQ) |
| vLLM 兼容性问题 | 功能缺失 | 准备备选方案 (llama.cpp) |
| 前端性能问题 | 用户体验差 | React.memo + 虚拟滚动 |

### 4.2 时间风险

| 风险 | 影响 | 应对措施 |
|-----|------|---------|
| 功能延期 | 项目延期 | MVP 优先，渐进交付 |
| 学习曲线陡峭 | 开发效率低 | 先搭架子，再填细节 |

---

## 五、交付物清单

### Phase 1 交付物
- [ ] vLLM 集成文档
- [ ] 监控指标 Dashboard
- [ ] PostgreSQL 数据库设计文档

### Phase 2 交付物
- [ ] 工作流编排前端页面
- [ ] 工作流执行引擎
- [ ] 监控大屏

### Phase 3 交付物
- [ ] 模型管理后台
- [ ] 自动扩缩容配置
- [ ] 完整部署文档

---

## 六、后续规划

### 6个月后目标
- 支持 10+ 种开源模型
- 服务 100+ 并发用户
- 99.9% 可用性
- 完整的 CI/CD 流水线

### 1年后目标
- 支持国产芯片 (昆仑芯/昇腾)
- 联邦学习能力
- 商业化 SaaS 服务
