# AI Infra 项目可添加功能清单

> 基于 Cube Studio 和 vLLM-Kunlun 分析，结合业界最佳实践

## 一、功能优先级矩阵

| 优先级 | 功能 | 复杂度 | 价值 | 参考项目 |
|-------|------|-------|------|---------|
| P0 | 拖拽式工作流编排 | 高 | 极高 | Cube Studio |
| P0 | 推理引擎集成 (vLLM) | 中 | 极高 | vLLM-Kunlun |
| P1 | 资源监控 Dashboard | 中 | 高 | Cube Studio |
| P1 | 模型版本管理 | 中 | 高 | MLflow |
| P1 | 量化推理支持 | 中 | 高 | vLLM-Kunlun |
| P2 | 自动扩缩容 | 中 | 中 | KEDA |
| P2 | A/B 测试框架 | 中 | 中 | Cube Studio |
| P2 | 多租户隔离增强 | 高 | 中 | Cube Studio |
| P3 | 边缘推理 | 高 | 低 | KubeEdge |
| P3 | 联邦学习 | 极高 | 低 | FATE |

---

## 二、核心功能详解

### 2.1 拖拽式工作流编排 (P0)

**功能描述**：可视化设计 Agent 协作流程

**技术方案**：
```
前端: React + ReactFlow / X6
后端: 生成 DAG → 存储为 JSON → 调度执行
存储: PostgreSQL (工作流定义) + Redis (运行时状态)
```

**核心组件**：
- **画布**：无限画布，支持缩放/拖拽
- **节点**：Agent 节点、条件节点、并行节点、子流程节点
- **边**：数据流依赖，支持条件分支
- **属性面板**：配置节点参数

**参考实现**：
- Cube Studio: `myapp/frontend/src/pages/Pipeline/`
- n8n: 开源工作流引擎

**AI Corp 适配**：
```yaml
# 工作流示例：代码审查流程
nodes:
  - id: code-review
    type: agent
    agent_type: backend
    input: "{{trigger.code}}"
  
  - id: check-quality
    type: condition
    condition: "{{code-review.quality_score}} > 80"
  
  - id: approve
    type: agent
    agent_type: pm
    input: "代码质量通过，请审批"
    when: "check-quality == true"
  
  - id: fix-code
    type: agent
    agent_type: backend
    input: "请修复以下问题: {{code-review.issues}}"
    when: "check-quality == false"
```

---

### 2.2 推理引擎集成 (P0)

**功能描述**：集成 vLLM 作为真实推理后端

**架构设计**：
```
AI Corp Orchestrator
    ↓ HTTP/gRPC
vLLM Inference Server (Docker/K8s)
    ↓
GPU/CPU 推理
```

**集成方案**：
```go
// pkg/llm/vllm_client.go
package llm

import (
    "context"
    "encoding/json"
    "net/http"
)

type VLLMClient struct {
    BaseURL string
    Model   string
    client  *http.Client
}

type CompletionRequest struct {
    Model       string  `json:"model"`
    Prompt      string  `json:"prompt"`
    MaxTokens   int     `json:"max_tokens"`
    Temperature float64 `json:"temperature"`
}

type CompletionResponse struct {
    Choices []struct {
        Text string `json:"text"`
    } `json:"choices"`
    Usage struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
    } `json:"usage"`
}

func (c *VLLMClient) Generate(ctx context.Context, prompt string) (*CompletionResponse, error) {
    req := CompletionRequest{
        Model:       c.Model,
        Prompt:      prompt,
        MaxTokens:   512,
        Temperature: 0.7,
    }
    
    resp, err := c.client.Post(
        c.BaseURL+"/v1/completions",
        "application/json",
        jsonBody(req),
    )
    // ... 解析响应
}
```

**部署配置**：
```yaml
# docker-compose.yml
services:
  vllm:
    image: vllm/vllm-openai:latest
    command: >
      --model deepseek-ai/deepseek-coder-6.7b-instruct
      --tensor-parallel-size 2
      --max-num-seqs 256
    volumes:
      - ./models:/models
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 2
              capabilities: [gpu]
```

**性能优化**：
- 连续批处理：动态合并请求
- PagedAttention：优化 KV Cache
- 量化：AWQ/GPTQ 降低显存

---

### 2.3 资源监控 Dashboard (P1)

**功能描述**：实时监控 Agent 和推理资源

**监控指标**：

| 层级 | 指标 | 采集方式 |
|-----|------|---------|
| Agent | 任务数/成功率/延迟 | 应用埋点 |
| LLM | TTFT/TPOT/吞吐 | vLLM metrics |
| GPU | 利用率/显存/温度 | DCGM |
| 系统 | CPU/内存/磁盘 | Node Exporter |

**技术栈**：
```
采集: Prometheus
存储: Prometheus TSDB / VictoriaMetrics
展示: Grafana
告警: AlertManager
```

**Dashboard 设计**：
```
┌─────────────────────────────────────────────────────────┐
│  AI Corp - 实时监控大盘                                   │
├─────────────────┬─────────────────┬─────────────────────┤
│ 在线 Agent 数    │ 任务成功率      │ 平均响应时间        │
│ 24              │ 98.5%           │ 1.2s                │
├─────────────────┴─────────────────┴─────────────────────┤
│ 推理服务性能                                             │
│ [QPS 曲线] [TTFT 分布] [Token 吞吐]                     │
├─────────────────────────────────────────────────────────┤
│ GPU 资源使用                                             │
│ GPU0: 85% ████████░░  显存: 12GB/16GB                   │
│ GPU1: 60% ██████░░░░  显存: 8GB/16GB                    │
└─────────────────────────────────────────────────────────┘
```

**参考实现**：
- Cube Studio: `myapp/views/view_monitor.py`
- Prometheus Go Client: `github.com/prometheus/client_golang`

---

### 2.4 模型版本管理 (P1)

**功能描述**：模型全生命周期管理

**功能清单**：
1. **模型注册**：上传/导入模型文件
2. **版本管理**：多版本共存，版本对比
3. **元数据**：模型大小、精度、训练数据
4. **血缘追踪**：训练任务 → 模型 → 推理服务
5. **一键部署**：模型 → 推理服务

**技术方案**：
```
存储后端:
  - 模型文件: S3/MinIO/OSS
  - 元数据: PostgreSQL
  - 索引: Elasticsearch (全文搜索)

模型格式支持:
  - PyTorch: .pt, .pth
  - HuggingFace: 完整目录
  - ONNX: .onnx
  - GGUF: .gguf (本地推理)
```

**数据模型**：
```go
type Model struct {
    ID          string
    Name        string
    Version     string
    Description string
    Format      string  // pytorch, onnx, gguf
    Size        int64
    Checksum    string  // SHA256
    StoragePath string  // S3 path
    Metrics     map[string]float64  // 精度指标
    CreatedAt   time.Time
    CreatedBy   string
    Tags        []string
}

type ModelVersion struct {
    ModelID     string
    Version     string
    ParentVersion string  // 血缘关系
    TrainingJobID string
    Metrics     map[string]float64
    Status      string  // training, ready, deprecated
}
```

**参考实现**：
- MLflow: 开源模型管理
- Cube Studio: `myapp/views/view_model.py`

---

### 2.5 量化推理支持 (P1)

**功能描述**：支持 AWQ/GPTQ/FP8 量化，降低显存占用

**量化方案对比**：

| 方法 | 精度损失 | 速度提升 | 显存节省 | 适用场景 |
|-----|---------|---------|---------|---------|
| FP16 | 无 | 1x | 50% | 通用 |
| AWQ | 极小 | 2-3x | 75% | 消费级 GPU |
| GPTQ | 小 | 2-3x | 75% | 批处理 |
| FP8 | 极小 | 1.5x | 50% | H100/A100 |

**集成方案**：
```python
# 量化模型转换服务
from awq import AutoAWQForCausalLM
from transformers import AutoTokenizer

def quantize_model(model_path, output_path, quant_config):
    model = AutoAWQForCausalLM.from_pretrained(model_path)
    tokenizer = AutoTokenizer.from_pretrained(model_path)
    
    model.quantize(
        tokenizer,
        quant_config={
            "zero_point": True,
            "q_group_size": 128,
            "w_bit": 4,
            "version": "GEMM"
        }
    )
    
    model.save_quantized(output_path)
    tokenizer.save_pretrained(output_path)
```

**运行时切换**：
```go
// 根据 GPU 显存自动选择模型精度
func selectModelPrecision(gpuMemoryGB int) string {
    switch {
    case gpuMemoryGB >= 24:
        return "fp16"
    case gpuMemoryGB >= 12:
        return "awq-4bit"
    case gpuMemoryGB >= 6:
        return "awq-4bit-g128"
    default:
        return "gguf-q4_k_m"
    }
}
```

**参考实现**：
- vLLM-Kunlun: `vllm_kunlun/ops/quantization/`
- AutoAWQ: 开源量化工具

---

### 2.6 自动扩缩容 (P2)

**功能描述**：基于负载自动调整 Agent/推理实例数

**触发指标**：
- 任务队列深度 > 10 → 扩容
- 平均响应时间 > 2s → 扩容
- GPU 利用率 > 80% → 扩容
- 空闲时间 > 5min → 缩容

**技术方案**：
```yaml
# KEDA ScaledObject
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: ai-agent-scaler
spec:
  scaleTargetRef:
    name: ai-agent-deployment
  minReplicaCount: 2
  maxReplicaCount: 20
  triggers:
  # 基于队列深度
  - type: metrics-api
    metadata:
      targetValue: "10"
      url: "http://orchestrator:8080/metrics/queue_depth"
  # 基于响应时间
  - type: prometheus
    metadata:
      serverAddress: http://prometheus:9090
      metricName: response_time_p95
      threshold: '2000'
      query: |
        histogram_quantile(0.95, 
          sum(rate(http_request_duration_seconds_bucket[5m])) by (le)
        )
```

**冷却策略**：
```
扩容冷却: 30s (快速响应负载增长)
缩容冷却: 300s (避免抖动)
预测扩容: 基于历史模式提前扩容
```

---

### 2.7 A/B 测试框架 (P2)

**功能描述**：对比不同 Agent 策略/模型的效果

**实验设计**：
```yaml
experiment:
  name: code-review-strategy
  description: 对比两种代码审查策略
  
  variants:
    - name: control
      weight: 50
      config:
        agent_type: backend
        prompt_template: standard
    
    - name: treatment
      weight: 50
      config:
        agent_type: backend-v2
        prompt_template: detailed
  
  metrics:
    - name: approval_rate
      type: conversion
    - name: review_time
      type: duration
    - name: bug_escape_rate
      type: conversion
      window: 7d  # 7天后统计漏检率
```

**分析报表**：
```
┌─────────────────────────────────────────────────────────┐
│ 实验: code-review-strategy                               │
│ 运行时间: 7天 | 样本量: 1000                             │
├─────────────────┬─────────────────┬─────────────────────┤
│ 指标            │ Control         │ Treatment           │
├─────────────────┼─────────────────┼─────────────────────┤
│ 审批通过率      │ 75%             │ 82% (+9.3%) ✓       │
│ 平均审查时间    │ 5.2min          │ 4.1min (-21%) ✓     │
│ 漏检率(7d)      │ 8%              │ 5% (-37.5%) ✓       │
│ 置信度          │ -               │ 95%                 │
└─────────────────┴─────────────────┴─────────────────────┘
结论: Treatment 策略显著优于 Control，建议全量上线
```

---

## 三、技术选型建议

### 3.1 前端技术栈

| 功能 | 推荐方案 | 备选方案 |
|-----|---------|---------|
| 工作流编排 | ReactFlow | X6, Rete.js |
| 可视化图表 | ECharts | AntV, D3.js |
| 状态管理 | Zustand | Redux, Jotai |
| UI 组件 | Ant Design | Chakra UI |

### 3.2 后端技术栈

| 功能 | 推荐方案 | 备选方案 |
|-----|---------|---------|
| 工作流引擎 | Temporal | Cadence, Argo |
| 消息队列 | NATS | RabbitMQ, Kafka |
| 缓存 | Redis | KeyDB |
| 数据库 | PostgreSQL | MySQL |
| 对象存储 | MinIO | Ceph |

### 3.3 基础设施

| 功能 | 推荐方案 | 备选方案 |
|-----|---------|---------|
| 容器编排 | Kubernetes | Docker Swarm |
| 服务网格 | Istio | Linkerd |
| 监控 | Prometheus + Grafana | Datadog |
| 日志 | Loki + Grafana | ELK |

---

## 四、实施路线图

### Phase 1 (1-2月)：核心功能
- [ ] 集成 vLLM 推理引擎
- [ ] 基础监控 Dashboard
- [ ] 模型版本管理

### Phase 2 (2-3月)：体验优化
- [ ] 拖拽式工作流编排
- [ ] 量化推理支持
- [ ] A/B 测试框架

### Phase 3 (3-4月)：企业特性
- [ ] 自动扩缩容
- [ ] 多租户隔离增强
- [ ] 高级监控告警

### Phase 4 (4-6月)：生态扩展
- [ ] 边缘推理
- [ ] 联邦学习
- [ ] 模型市场
