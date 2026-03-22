# 云计算 & 弹性计算技术栈学习文档

> 本文档基于 AI Corp 项目中使用的技术，梳理云计算与弹性计算的核心知识体系。

## 目录

1. [容器化与编排](#1-容器化与编排)
2. [弹性伸缩架构](#2-弹性伸缩架构)
3. [服务发现与负载均衡](#3-服务发现与负载均衡)
4. [分布式存储](#4-分布式存储)
5. [可观测性](#5-可观测性)

---

## 1. 容器化与编排

### 1.1 Docker 基础

**项目中使用：** AI Corp 的每个 Agent 运行在独立容器中，隔离 CPU/内存资源。

```bash
# 构建 Agent 镜像
docker build -t ai-corp/agent:latest -f Dockerfile .

# 限制资源（弹性计算核心）
docker run --cpus="1.5" --memory="2g" ai-corp/agent:latest

# 查看容器资源使用
docker stats
```

**关键概念：**

| 概念 | 说明 | AI Corp 应用 |
|------|------|-------------|
| cgroup | 限制 CPU/内存 | Agent 资源配额 |
| namespace | 隔离网络/进程 | Agent 间隔离 |
| 镜像分层 | 复用基础层 | 多 Agent 共享基础镜像 |
| Volume | 持久化存储 | 模型文件/日志 |

### 1.2 Kubernetes 核心对象

**项目参考：** AI Corp 的 Master-Worker 架构直接借鉴了 K8s 设计。

```yaml
# Agent 对应 K8s Pod
apiVersion: v1
kind: Pod
metadata:
  name: ai-agent-frontend
  labels:
    workspace: frontend
    model: deepseek-coder
spec:
  containers:
  - name: agent
    image: ai-corp/agent:latest
    resources:
      requests:
        cpu: "0.5"
        memory: "1Gi"
      limits:
        cpu: "2"
        memory: "4Gi"

---
# Workspace 对应 K8s Namespace
apiVersion: v1
kind: Namespace
metadata:
  name: frontend-workspace
```

**核心对象对比：**

| K8s 对象 | AI Corp 对应 | 作用 |
|---------|------------|------|
| Pod | Agent 实例 | 最小运行单元 |
| Deployment | AgentPool | 管理副本数 |
| Service | Agent 服务发现 | 内部通信 |
| ConfigMap | configs/config.yaml | 配置管理 |
| HPA | 任务队列自动扩缩 | 弹性伸缩 |
| Namespace | Workspace | 资源隔离 |

### 1.3 HPA（Horizontal Pod Autoscaler）弹性伸缩

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: ai-agent-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ai-agent-pool
  minReplicas: 2
  maxReplicas: 20
  metrics:
  # CPU 触发扩容
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  # 自定义指标：任务队列深度触发扩容
  - type: External
    external:
      metric:
        name: task_queue_length
      target:
        type: Value
        value: "10"
```

**扩缩容策略：**

```
任务队列 > 10  →  增加 Agent 副本
任务队列 < 2   →  缩减 Agent 副本（保留最小 2 个）
CPU > 70%     →  立即触发扩容
冷却时间       →  扩容 30s / 缩容 300s（避免抖动）
```

---

## 2. 弹性伸缩架构

### 2.1 弹性计算的核心指标

```
响应时间 (Latency)     → 决定是否需要扩容
吞吐量 (Throughput)    → 决定最优副本数
资源利用率 (Utilization) → 成本优化依据
队列深度 (Queue Depth) → 最直接的扩容信号
```

### 2.2 AI Corp 的弹性架构设计

```
用户请求
    ↓
[Load Balancer] ← nginx/Envoy
    ↓
[Orchestrator] ← 任务调度中心 (单点 or HA)
    ↓
[Task Queue] ← 内存队列 / NATS / Kafka
    ↓
[Agent Pool] ← 可弹性伸缩的 Worker 池
  ├─ frontend-agents (0~10个)
  ├─ backend-agents  (0~10个)
  ├─ testing-agents  (0~5个)
  └─ devops-agents   (0~5个)
    ↓
[LLM Inference] ← Ollama / API (可独立扩容)
```

### 2.3 VPA（Vertical Pod Autoscaler）垂直扩缩

```yaml
# 自动调整单个 Pod 的 CPU/内存配额
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: llm-inference-vpa
spec:
  targetRef:
    apiVersion: "apps/v1"
    kind: Deployment
    name: llm-server
  updatePolicy:
    updateMode: "Auto"  # 自动更新
  resourcePolicy:
    containerPolicies:
    - containerName: llm
      minAllowed:
        memory: "4Gi"
      maxAllowed:
        memory: "32Gi"
```

### 2.4 KEDA（基于事件的弹性伸缩）

```yaml
# 基于 NATS 消息队列深度触发 Agent 扩容
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: ai-agent-scaler
spec:
  scaleTargetRef:
    name: ai-agent-deployment
  minReplicaCount: 1
  maxReplicaCount: 50
  triggers:
  - type: nats-jetstream
    metadata:
      natsServerMonitoringEndpoint: "nats://localhost:8222"
      stream: "TASKS"
      consumer: "agent-consumer"
      lagThreshold: "5"  # 队列积压 5 条消息时触发扩容
```

---

## 3. 服务发现与负载均衡

### 3.1 AI Corp 内部服务通信

```go
// pkg/message/ 实现了基于内存的消息队列
// 生产环境替换为 NATS / Kafka

// Orchestrator → Agent 任务分发
type TaskMessage struct {
    TaskID    string `json:"task_id"`
    AgentType string `json:"agent_type"`
    Payload   string `json:"payload"`
}
```

### 3.2 服务网格（Service Mesh）

```yaml
# Istio 为 Agent 间通信添加：
# 1. mTLS 加密
# 2. 流量限制
# 3. 熔断器
# 4. 链路追踪

apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: llm-service
spec:
  hosts:
  - llm-service
  http:
  - retries:
      attempts: 3
      perTryTimeout: 30s
    fault:
      delay:
        percentage:
          value: 0.1  # 1% 请求注入 5s 延迟（混沌测试）
        fixedDelay: 5s
```

### 3.3 DNS 服务发现

```bash
# K8s 内部 DNS 格式：
# <service>.<namespace>.svc.cluster.local

# AI Corp 各组件访问地址
orchestrator.ai-corp.svc.cluster.local:8080
llm-server.ai-corp.svc.cluster.local:11434
nats.ai-corp.svc.cluster.local:4222
```

---

## 4. 分布式存储

### 4.1 模型文件存储

```
本地开发:  /home/haoqian.li/ai-corp/models/gguf/
生产环境方案:
  ├─ NFS/NAS  → 多节点共享模型文件
  ├─ S3/OSS   → 模型版本管理 (华为云OBS/阿里云OSS)
  └─ PVC      → K8s 持久化存储卷
```

```yaml
# 模型文件 PVC 配置
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: model-storage
spec:
  accessModes:
  - ReadWriteMany  # 多节点共享读
  resources:
    requests:
      storage: 100Gi
  storageClassName: nfs-client
```

### 4.2 向量数据库（RAG 检索）

```
AI Corp pkg/rag/ 实现了简单的向量检索
生产环境选型：
  ├─ Milvus   → 专业向量数据库，支持亿级向量
  ├─ Qdrant   → Rust 实现，低延迟
  ├─ Weaviate → GraphQL API，易用
  └─ pgvector → PostgreSQL 扩展，简单场景
```

---

## 5. 可观测性

### 5.1 指标（Metrics）

```go
// AI Corp pkg/metrics/ 使用 Prometheus 格式
// 关键指标：

agent_task_total{status="completed"}      // 任务完成数
agent_task_duration_seconds               // 任务耗时
llm_inference_tokens_total{model="..."}   // Token 消耗
llm_inference_latency_seconds             // 推理延迟
task_queue_depth                          // 队列深度（扩容信号）
```

```yaml
# Prometheus 采集配置
scrape_configs:
  - job_name: 'ai-corp'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
    scrape_interval: 15s
```

### 5.2 日志（Logging）

```
结构化日志格式 (JSON):
{
  "level": "info",
  "time": "2024-03-21T15:00:00Z",
  "agent_id": "agent-123",
  "task_id": "task-456",
  "model": "deepseek-coder:1.3b",
  "tokens": 256,
  "latency_ms": 1200,
  "msg": "task completed"
}

日志采集链路：
Pod stdout → Fluent Bit → Elasticsearch → Kibana
```

### 5.3 链路追踪（Tracing）

```
用户请求 → Orchestrator → Agent → LLM
   │             │           │       │
   └─────────────┴───────────┴───────┘
                   OpenTelemetry Trace

TraceID: abc123  (全链路唯一 ID)
SpanID:
  ├─ /api/v1/tasks (50ms)
  ├─ task_schedule (5ms)
  ├─ agent_execute (1200ms)
  └─ llm_inference (1100ms)  ← 瓶颈在这里
```
