# 弹性计算核心技术学习文档

> 面向云计算/弹性计算方向，深入梳理 AI Corp 项目中涉及的弹性计算技术。

## 目录

1. [弹性计算基础概念](#1-弹性计算基础概念)
2. [资源调度算法](#2-资源调度算法)
3. [AI 场景的特殊挑战](#3-ai-场景的特殊挑战)
4. [成本优化策略](#4-成本优化策略)
5. [实战：AI Corp 弹性架构演进](#5-实战ai-corp-弹性架构演进)

---

## 1. 弹性计算基础概念

### 1.1 弹性的三个维度

```
水平弹性 (Scale Out/In)
  → 增减实例数量
  → AI Corp: 增减 Agent 副本数
  → 实现: HPA / KEDA

垂直弹性 (Scale Up/Down)
  → 调整单实例 CPU/内存
  → AI Corp: 调整 LLM Server 内存配额
  → 实现: VPA

深度弹性 (Scale Deep)
  → 调整模型大小/精度（AI 独有）
  → AI Corp: 大任务用 6.7B，小任务用 1.3B
  → 实现: 模型路由策略
```

### 1.2 弹性触发指标

| 指标类型 | 示例 | 延迟 | 准确性 |
|---------|------|------|-------|
| 资源指标 | CPU > 70% | 低 | 滞后（已满负荷才触发）|
| 应用指标 | 队列深度 > 10 | 极低 | 最准确 |
| 预测性指标 | 基于历史流量预测 | 超前 | 需要足够历史数据 |
| 定时策略 | 工作日 9-18 点扩容 | 超前 | 适合规律性业务 |

**AI Corp 推荐：优先使用任务队列深度作为弹性触发指标**

```go
// pkg/metrics/ 已暴露此指标
task_queue_depth = len(orchestrator.taskQueue)
// KEDA 监听此指标，队列 > 5 时触发扩容
```

### 1.3 冷启动问题

```
问题：扩容一个新 Agent 需要时间
  容器启动:    ~2s
  服务就绪:    ~3s
  模型加载:    ~30-120s  ← LLM 是主要瓶颈

解决方案：
1. 预热实例池 (Warm Pool)
   - 预启动若干空闲 Agent，模型已加载完毕
   - 代价：浪费资源（可用 Spot 实例降低成本）

2. 模型预加载
   - 容器启动时立即加载模型
   - 使用 Kubernetes Init Container

3. 模型共享内存
   - 多个 Agent 进程共享同一份模型权重
   - 使用 mmap + 只读文件系统
```

---

## 2. 资源调度算法

### 2.1 AI Corp 任务调度策略（pkg/cluster/manager.go）

```go
// 三种调度策略
type SchedulePolicy int
const (
    RoundRobin    SchedulePolicy = iota  // 轮询 - 均匀分布
    LeastLoaded                          // 最小负载 - 性能优先
    ModelAware                           // 模型感知 - 减少模型切换开销
)

// ModelAware 策略：优先选择已加载目标模型的 Worker
// 减少模型换入换出的时间开销（每次换模型需要 30s+）
func (s *TaskScheduler) modelAwareSchedule(task *Task) *WorkerNode {
    // 1. 找到已加载所需模型的 Worker
    for _, w := range s.workers {
        if w.LoadedModel == task.RequiredModel && w.IsAvailable() {
            return w
        }
    }
    // 2. fallback: 找负载最低的 Worker
    return s.leastLoadedSchedule(task)
}
```

### 2.2 Bin Packing（装箱算法）

K8s 调度器使用此算法，最大化节点利用率：

```
问题：将 Pod 分配到 Node，使整体资源利用率最大

AI Corp 场景：
  Node 1: 16 CPU, 32GB RAM
    ├─ LLM Server: 4 CPU, 8GB (模型占用)
    ├─ Agent × 4: 1 CPU × 4, 2GB × 4
    └─ 剩余: 4 CPU, 16GB

  最优装箱：
    - 大模型 (6.7B) → 高内存节点
    - 小模型 (1.3B) × 多个 → 普通节点
    - CPU Agent → 低成本节点
```

### 2.3 抢占式调度（Preemption）

```yaml
# 高优先级任务可抢占低优先级任务的资源
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: urgent-task
value: 1000  # 高优先级

---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: background-task
value: 100  # 低优先级，可被抢占
```

```
场景：
  正在执行低优先级批量任务（代码分析）
  突然来了高优先级任务（生产 Bug 修复）
  → 系统暂停批量任务，释放资源给紧急任务
  → 批量任务排队等待
```

---

## 3. AI 场景的特殊挑战

### 3.1 GPU 异构资源调度

```
传统弹性计算：CPU + 内存 两个维度
AI 弹性计算：CPU + 内存 + GPU + 显存 + NVLink 四个维度

GPU 调度难点：
1. GPU 显存不可超卖（OOM 会 crash，不像 CPU 会降速）
2. NVLink 拓扑影响多卡通信效率
3. GPU 型号差异大（A100=80GB, T4=16GB, RTX3090=24GB）
4. GPU 碎片化：4GB 可用显存 + 12GB 可用显存 ≠ 16GB 连续显存

解决方案：
- MIG (Multi-Instance GPU): 将 A100 切分为 7 个 10GB 实例
- GPU 时分复用: 多任务轮流使用 GPU（低效但便宜）
- 显存虚拟化: vGPU / NVIDIA vComputeServer
```

### 3.2 长尾延迟问题

```
LLM 推理延迟特性（与传统服务不同）：
  首 Token 延迟 (TTFT): 200ms - 5s
  生成速度 (TPS):        3 - 100 tok/s
  总延迟:               10s - 5min（取决于输出长度）

对弹性策略的影响：
  × 不能用平均延迟作为扩容指标（尾部延迟被稀释）
  ✓ 使用 P99 延迟 + 队列等待时间作为扩容指标
  ✓ 设置最大 Context 长度限制（避免单请求占用过久）
```

### 3.3 模型热切换

```
场景：同一台机器运行多个模型，根据任务类型动态切换

内存换出换入策略：
  模型 A 正在推理 → 模型 B 请求到来
  → 将模型 A 权重换出到 CPU 内存（或磁盘）
  → 加载模型 B 到 GPU 显存
  → 切换耗时：2-30s（取决于模型大小和带宽）

优化手段：
1. 模型分片：按层切分，只换出部分层
2. 预取：预测下一个任务，提前加载模型
3. 模型复用：尽量让同类任务路由到同一实例（ModelAware 调度）
```

---

## 4. 成本优化策略

### 4.1 Spot 实例的使用

```
AI Corp 场景下的 Spot 实例使用策略：

适合 Spot：
  ✅ 无状态 Agent（任务可重试）
  ✅ 批量代码分析任务
  ✅ 模型训练任务（支持 checkpoint）
  ✅ 预热实例池

不适合 Spot：
  ❌ Orchestrator（单点，需要 On-Demand）
  ❌ LLM Server（模型加载慢，中断代价高）
  ❌ 有 SLA 要求的实时任务
```

### 4.2 分时利用率优化

```
AI Corp 典型负载曲线（程序员外包公司）：
  09:00-12:00  高峰（60% 利用率）
  12:00-14:00  低谷（20% 利用率）
  14:00-18:00  高峰（70% 利用率）
  18:00-09:00  低谷（10% 利用率）

弹性策略：
  工作时间：保持最小 5 个 Agent，自动扩容到 20
  非工作时间：缩容到最小 1 个（保活），节省 80% 成本

KEDA CronHPA 配置：
  早高峰前 8:45 扩容到 5 个，避免冷启动延迟
```

### 4.3 模型成本对比

```
AI Corp 双模式成本分析：

本地模式（DeepSeek 1.3B）：
  成本：电费 ~0.1 元/小时（服务器已有）
  性能：~15 tok/s，适合简单任务
  隐私：完全本地，数据不出境

云端 API（DeepSeek API）：
  成本：~0.14 元/1M input tokens
  性能：~50 tok/s，质量更高
  隐私：数据上传云端

最优策略（AI Corp 已实现）：
  简单任务 → 本地模型（1.3B）
  复杂任务 → 云端 API（deepseek-chat）
  预计节省 60-80% API 费用
```

---

## 5. 实战：AI Corp 弹性架构演进

### 阶段 1：单机部署（当前）

```
优点：简单，零成本
缺点：无弹性，单点故障
适合：开发测试

组件：
  Orchestrator × 1 (固定)
  Agent (内存中的 goroutine)
  LLM Server × 1 (固定)
```

### 阶段 2：容器化 + 基础弹性

```
改造：
  1. 每个 Agent 类型独立 Docker 容器
  2. Docker Compose 管理多容器
  3. 基于 CPU 的 HPA

docker-compose.yml:
  orchestrator: replicas: 1
  frontend-agent: replicas: 2-10
  backend-agent:  replicas: 2-10
  llm-server:     replicas: 1 (资源密集)
```

### 阶段 3：K8s + KEDA（生产推荐）

```
改造：
  1. 迁移到 Kubernetes
  2. 使用 KEDA 基于任务队列扩容
  3. Spot 实例降低成本
  4. HPA 保底（CPU/内存）

预期效果：
  - 高峰处理能力：10x
  - 平均成本：降低 50%
  - 可用性：99.9%
```

### 阶段 4：多云 + AI 专属调度

```
改造：
  1. GPU 节点用于 LLM Server
  2. CPU Spot 节点用于 Agent
  3. 模型动态加载/卸载
  4. 自研 AI 调度器（ModelAware）

组件新增：
  - GPU Node Pool (A100/T4)
  - 模型仓库 (S3/OSS)
  - 推理引擎 (vLLM)
  - 向量数据库 (Milvus)
```
