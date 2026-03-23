# Cube Studio 技术架构深度分析

> 腾讯音乐开源的一站式云原生机器学习平台

## 一、项目概述

**定位**：面向企业级 AI 开发的全流程 MLOps 平台
**核心能力**：数据管理 → 开发环境 → 模型训练 → 推理服务 → 监控运维
**技术栈**：Python + React + Kubernetes

---

## 二、整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                        前端层 (React)                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ 项目管理  │ │ 工作流编排 │ │ 资源监控  │ │ 模型市场  │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└────────────────────────┬────────────────────────────────────┘
                         │ REST API / WebSocket
┌────────────────────────▼────────────────────────────────────┐
│                      API 网关层                              │
│              (Nginx / Kong / APISIX)                        │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                     服务层 (Python Flask)                    │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ 用户服务  │ │ 任务服务  │ │ 模型服务  │ │ 监控服务  │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                    调度层 (Airflow/Argo)                     │
│              DAG 编排、任务调度、依赖管理                      │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                   执行层 (Kubernetes)                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ Notebook │ │ 训练任务  │ │ 推理服务  │ │ 数据处理  │       │
│  │   Pod    │ │   Pod    │ │   Pod    │ │   Pod    │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└─────────────────────────────────────────────────────────────┘
```

---

## 三、核心模块技术实现

### 3.1 项目管理模块

**功能**：多租户隔离、资源配额、权限控制

**关键设计**：
- 项目作为资源隔离的基本单位
- 支持资源组绑定（开发/训练/推理资源分离）
- RBAC 权限模型：管理员/开发者/访客

**代码位置**：`myapp/views/project.py`

```python
class Project:
    """项目实体"""
    id: int
    name: str
    resource_group: str  # 绑定资源组
    users: List[User]    # 项目成员
    quotas: ResourceQuota  # 资源配额
```

### 3.2 拖拽式工作流编排

**功能**：可视化 Pipeline 设计、任务依赖管理

**技术实现**：
- 前端：React + ReactFlow（或类似库）
- 后端：Airflow DAG 生成
- 存储：任务模板 JSON + DAG Python 文件

**关键特性**：
1. **算子市场**：预置 50+ 算子（数据处理、特征工程、模型训练）
2. **参数传递**：任务间通过 XCom 传递数据
3. **条件分支**：支持 if/else 逻辑节点
4. **循环执行**：支持 for 循环节点

**代码位置**：
- 前端：`myapp/frontend/src/pages/Pipeline/`
- 后端：`myapp/views/pipeline.py`
- 模板：`job-template/`

### 3.3 Notebook 开发环境

**功能**：JupyterLab / VSCode 在线 IDE

**技术实现**：
```yaml
# Notebook Pod 模板
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: notebook
    image: ccr.ccs.tencentyun.com/cube-studio/notebook:gpu
    resources:
      limits:
        nvidia.com/gpu: 1  # GPU 分配
    volumeMounts:
    - name: workspace
      mountPath: /home/jovyan
  volumes:
  - name: workspace
    persistentVolumeClaim:
      claimName: pvc-{project-id}
```

**关键特性**：
- 镜像预置：TensorFlow/PyTorch/大数据环境
- SSH 远程开发：支持本地 IDE 连接
- 自动保存：定时快照用户环境
- GPU 监控：实时显示显存使用

### 3.4 模型训练调度

**功能**：分布式训练、超参搜索、自动调参

**调度策略**：

| 调度器 | 适用场景 | 特点 |
|-------|---------|------|
| Kubernetes Scheduler | 通用任务 | 原生调度，支持资源限制 |
| Volcano | 批处理/AI 任务 | 支持 Gang Scheduling |
| Ray | 分布式训练 | 弹性伸缩，故障恢复 |

**分布式训练支持**：
- **数据并行**：PyTorch DDP / Horovod
- **模型并行**：DeepSpeed / Megatron-LM
- **流水线并行**：GPipe / PipeDream

**代码位置**：`job-template/job/`

### 3.5 推理服务管理

**功能**：模型部署、A/B 测试、弹性伸缩

**架构设计**：
```
用户请求 → Ingress → Service → Pod (推理服务)
                ↓
           负载均衡 (Round Robin / Session Affinity)
                ↓
           弹性伸缩 (HPA based on QPS/Latency)
```

**关键特性**：
1. **多框架支持**：TF Serving / Triton / TorchServe / vLLM
2. **流量管理**：金丝雀发布、蓝绿部署、流量复制
3. **自动扩缩容**：基于 QPS/延迟/队列深度
4. **模型版本管理**：多版本共存、灰度切换

**代码位置**：`myapp/views/inference.py`

### 3.6 资源监控体系

**监控维度**：

| 层级 | 监控项 | 采集方式 |
|-----|-------|---------|
| 集群 | CPU/内存/磁盘/网络 | Prometheus Node Exporter |
| GPU | 显存/温度/利用率 | DCGM / nvidia-smi |
| Pod | 资源使用/日志 | cAdvisor + Fluentd |
| 服务 | QPS/延迟/错误率 | Prometheus Metrics |
| 任务 | 训练进度/损失曲线 | TensorBoard / MLflow |

**可视化**：
- Grafana Dashboard：集群资源大盘
- TensorBoard：训练过程可视化
- 自定义大屏：项目级资源使用

---

## 四、GPU 资源管理

### 4.1 GPU 调度策略

**整卡分配**（默认）：
```yaml
resources:
  limits:
    nvidia.com/gpu: 1  # 独占整卡
```

**vGPU 共享**（需配置）：
```yaml
resources:
  limits:
    nvidia.com/gpu: 0.5  # 共享半卡
```

**GPU 共享模式**（时间片轮转）：
- 多任务分时使用 GPU
- 适合开发调试场景
- 不适合训练（性能损耗大）

### 4.2 异构算力支持

| 芯片类型 | 支持状态 | 调度标签 |
|---------|---------|---------|
| NVIDIA GPU | ✅ 完整支持 | nvidia.com/gpu |
| 华为 NPU (Ascend) | ✅ 支持 | huawei.com/Ascend910 |
| 海光 DCU | ✅ 支持 | dcu.com/gpu |
| 寒武纪 MLU | ✅ 支持 | cambricon.com/mlu |
| 昆仑芯 XPU | ✅ 支持 | baidu.com/xpu |
| 天数智芯 GPU | ✅ 支持 | iluvatar.com/gpu |

**实现方式**：
- Device Plugin：各厂商提供 K8s 设备插件
- 调度器扩展：自定义调度器识别异构资源
- 运行时适配：containerd 配置不同运行时

### 4.3 资源配额体系

**多级配额**：
```
平台级配额 → 租户级配额 → 项目级配额 → 用户级配额
     ↓            ↓            ↓            ↓
  总 GPU 数    部门 GPU 数   项目 GPU 数   个人 GPU 数
```

**配额限制维度**：
- 开发资源：Notebook 数量、GPU 时长
- 训练资源：并行任务数、GPU 时长
- 推理资源：服务副本数、QPS 限制

---

## 五、高可用与容错

### 5.1 任务失败重试

**重试策略**：
```python
@retry(
    stop=stop_after_attempt(3),  # 最多重试 3 次
    wait=wait_exponential(multiplier=1, min=4, max=10),  # 指数退避
    retry=retry_if_exception_type(TransientError)
)
def execute_task(task):
    # 任务执行逻辑
    pass
```

**失败类型处理**：
- **可重试错误**：网络超时、资源暂时不可用
- **不可重试错误**：代码错误、数据错误

### 5.2 分布式训练容错

**Checkpoint 机制**：
```python
# PyTorch DDP Checkpoint
torch.save({
    'epoch': epoch,
    'model_state_dict': model.state_dict(),
    'optimizer_state_dict': optimizer.state_dict(),
    'loss': loss,
}, checkpoint_path)
```

**弹性训练**：
- 支持动态增减 Worker
- 故障 Worker 自动剔除
- 训练进度不丢失

### 5.3 服务高可用

**多副本部署**：
```yaml
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
```

**健康检查**：
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10
```

---

## 六、可借鉴的设计模式

### 6.1 云原生架构

**设计原则**：
1. **容器化**：所有组件运行在容器中
2. **微服务**：功能模块独立部署
3. **声明式 API**：通过 YAML 定义期望状态
4. **不可变基础设施**：镜像版本化管理

### 6.2 插件化设计

**算子市场机制**：
```
算子定义 (YAML) → 镜像构建 → 注册到平台 → 拖拽使用
```

**自定义算子**：
```yaml
# 算子定义示例
name: custom-training
description: 自定义训练算子
image: my-registry/custom-training:latest
inputs:
  - name: dataset
    type: dataset
  - name: epochs
    type: int
    default: 10
outputs:
  - name: model
    type: model
```

### 6.3 多租户隔离

**隔离维度**：
- 网络隔离：项目间网络策略
- 存储隔离：独立 PVC
- 资源隔离：ResourceQuota
- 权限隔离：RBAC

### 6.4 可视化设计

**拖拽式编排**：
- 节点：算子/任务
- 边：数据依赖
- 状态：颜色区分（成功/失败/运行中）

**实时监控**：
- WebSocket 推送状态更新
- 进度条显示任务进度
- 日志实时滚动

---

## 七、核心代码文件路径

| 功能模块 | 核心文件路径 |
|---------|-------------|
| 项目模型 | `myapp/models/model_project.py` |
| 任务模型 | `myapp/models/model_job.py` |
| Pipeline 视图 | `myapp/views/view_pipeline.py` |
| Notebook 视图 | `myapp/views/view_notebook.py` |
| 推理服务视图 | `myapp/views/view_inferenceservice.py` |
| 任务模板 | `job-template/job/` |
| 前端组件 | `myapp/frontend/src/components/` |
| 前端页面 | `myapp/frontend/src/pages/` |
| 安装脚本 | `install/` |

---

## 八、与 AI Corp 的对比

| 维度 | Cube Studio | AI Corp |
|-----|-------------|---------|
| **定位** | 企业级 MLOps 平台 | AI 员工外包公司 |
| **用户** | 算法工程师 | AI Agent |
| **核心功能** | 训练/推理全流程 | Agent 协作/任务调度 |
| **架构** | K8s + Airflow | Go + 自研调度 |
| **可视化** | 拖拽式 Pipeline | 像素酒馆风格 |
| **GPU 管理** | 完整支持异构 GPU | 当前仅 CPU |
| **扩展性** | 算子市场 | MCP/Skill 市场 |

---

## 九、可借鉴的功能点

### 9.1 立即可以借鉴

1. **拖拽式工作流编排**：可视化 Agent 协作流程
2. **资源监控 Dashboard**：GPU/CPU/内存实时监控
3. **任务模板市场**：预置常见 AI 任务模板
4. **多租户隔离**：Workspace 资源配额管理

### 9.2 中期可以引入

1. **Notebook 开发环境**：在线 IDE 调试 Agent
2. **模型版本管理**：MLflow 集成
3. **A/B 测试框架**：Agent 策略对比
4. **自动扩缩容**：KEDA 集成

### 9.3 长期规划

1. **分布式训练支持**：Ray/DeepSpeed 集成
2. **异构算力支持**：国产 GPU 适配
3. **边缘推理**：边缘节点部署
4. **联邦学习**：跨组织协作训练
