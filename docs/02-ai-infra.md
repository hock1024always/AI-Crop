# AI Infra 技术栈学习文档

> 本文档梳理 AI 基础设施的核心技术：推理优化、自训练迭代、可视化监控。

## 目录

1. [推理优化](#1-推理优化)
2. [模型部署架构](#2-模型部署架构)
3. [自训练迭代（Fine-tuning）](#3-自训练迭代)
4. [可视化与监控](#4-可视化与监控)
5. [AI Corp 实现对照](#5-ai-corp-实现对照)

---

## 1. 推理优化

### 1.1 量化（Quantization）

将模型权重从 float32 压缩为更低精度，减小内存占用、加速推理。

| 量化格式 | 内存占用 | 质量损失 | 推荐场景 |
|---------|---------|---------|---------|
| FP32    | 100%    | 无      | 训练     |
| FP16    | 50%     | 极小    | GPU 推理 |
| INT8    | 25%     | 小      | GPU/CPU  |
| Q8_0 (GGUF) | 25% | 小    | CPU 推理 |
| Q4_K_M (GGUF) | 12.5% | 中 | CPU 推理（推荐） |
| Q2_K   | 6.25%   | 大      | 极限压缩 |

```bash
# 使用 llama.cpp 对模型进行量化
./quantize deepseek-coder-1.3b-f16.gguf \
           deepseek-coder-1.3b-Q4_K_M.gguf Q4_K_M

# AI Corp 已下载的量化模型：
# deepseek-coder-1.3b.Q4_K_M.gguf (834MB, 原始 2.7GB)
```

**AI Corp 量化参数参考（pkg/aiinfra/inference.go）：**

```
DeepSeek-Coder-1.3B  FP16: 2.7GB → Q4_K_M: 0.8GB  速度: ~15 tok/s (CPU)
DeepSeek-Coder-6.7B  FP16: 13.4GB → Q4_K_M: 4.1GB  速度: ~4 tok/s (CPU)
Qwen2.5-7B           FP16: 15GB   → Q4_K_M: 4.5GB  速度: ~3 tok/s (CPU)
```

### 1.2 KV Cache（键值缓存）

重复 Prompt 前缀复用计算结果，减少推理 FLOPs。

```
无 Cache：
  每次请求都重新计算所有 Token 的 KV
  系统 Prompt (100 tokens) × N 请求 = N × 100 次计算

有 KV Cache：
  系统 Prompt 只计算一次，后续复用
  节省 ~30-40% 推理时间（对话场景）
```

```go
// AI Corp pkg/aiinfra/inference.go 实现了简单 Prompt Cache
cache := NewInferenceCache(1000)

key := sha256(model + ":" + prompt)
if entry, hit := cache.Get(key); hit {
    return entry.Response  // 直接返回缓存
}
// 否则调用 LLM 并缓存结果
```

### 1.3 批处理（Batching）

将多个请求合并为一个批次并行推理，提升 GPU 利用率。

```
连续批处理 (Continuous Batching) - vLLM 核心技术：

t=0: [req1: token1, token2, ...]
t=1: [req1: token3, req2: token1, ...]  ← req2 动态加入
t=2: [req1: token4, req2: token2, req3: token1, ...]

对比静态批处理：GPU 利用率从 40% → 80%+
```

### 1.4 推理引擎选型

| 引擎 | 特点 | 适用场景 |
|------|------|---------|
| llama.cpp | CPU 友好，支持 GGUF | 本地部署（AI Corp 当前使用）|
| Ollama | llama.cpp 封装，API 友好 | 本地服务化 |
| vLLM | PagedAttention，高吞吐 | GPU 生产环境 |
| TensorRT-LLM | NVIDIA 优化，最高性能 | A100/H100 |
| TGI (text-generation-inference) | HuggingFace 出品 | 中等规模 |
| SGLang | 编程接口，多请求复用 | 复杂推理链 |

### 1.5 投机采样（Speculative Decoding）

用小模型草稿 + 大模型验证，提速 2-3x：

```
草稿模型 (1.3B): 快速生成候选 token
目标模型 (70B):  并行验证，接受或拒绝

速度提升原理：
- 单次前向传播可验证多个 token
- 平均接受率约 70-80%
- 实际加速比约 2x-3x
```

---

## 2. 模型部署架构

### 2.1 单机部署（AI Corp 当前）

```
┌─────────────────────────────┐
│      AI Corp Server         │
│  ┌────────┐  ┌────────────┐ │
│  │Orchestr│  │ Local LLM  │ │
│  │ ator   │→ │ Server     │ │
│  │:8080   │  │ :11434     │ │
│  └────────┘  │(mock mode) │ │
│              └────────────┘ │
│  Model: deepseek-1.3b.gguf  │
│  RAM: 15GB, CPU only        │
└─────────────────────────────┘
```

### 2.2 多节点分布式部署

```
                    ┌──────────────────┐
                    │   API Gateway    │
                    │   (Kong/APISIX)  │
                    └────────┬─────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
     ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
     │ Orchestrator │ │ Orchestrator │ │ Orchestrator │
     │  (Master)    │ │  (Master)    │ │  (Standby)   │
     └──────┬───────┘ └──────┬───────┘ └──────────────┘
            │                │            HA via etcd
            └────────┬───────┘
                     │ NATS/Kafka
    ┌────────────────┼────────────────┐
    ▼                ▼                ▼
┌────────┐    ┌────────────┐   ┌────────────┐
│Frontend│    │  Backend   │   │   DevOps   │
│Agents  │    │  Agents    │   │   Agents   │
│(Node1) │    │  (Node2)   │   │  (Node3)   │
└────────┘    └────────────┘   └────────────┘
    │                │                │
┌───▼────────────────▼────────────────▼───┐
│          LLM Inference Cluster          │
│  ┌──────────────┐  ┌──────────────┐     │
│  │ vLLM Node 1  │  │ vLLM Node 2  │     │
│  │ (GPU A100)   │  │ (GPU A100)   │     │
│  └──────────────┘  └──────────────┘     │
└─────────────────────────────────────────┘
```

### 2.3 模型路由策略

```go
// 根据任务类型路由到最合适的模型
func routeToModel(agentType, taskType string) string {
    switch {
    case agentType == "developer" && taskType == "code_gen":
        return "deepseek-coder:6.7b"  // 代码任务用代码模型
    case agentType == "pm":
        return "deepseek-chat"         // 沟通任务用对话模型
    case taskType == "embedding":
        return "nomic-embed-text"      // 嵌入任务用嵌入模型
    default:
        return "deepseek-coder:1.3b"   // 默认轻量模型
    }
}
```

---

## 3. 自训练迭代

### 3.1 数据飞轮（Data Flywheel）

```
用户使用 AI Corp
    ↓
Agent 执行任务 → 用户给出反馈（点赞/踩/编辑）
    ↓
pkg/aiinfra/inference.go TrainingDataset.Add()
    ↓
积累高质量数据集（Alpaca/ShareGPT 格式）
    ↓
定期 Fine-tuning
    ↓
更好的模型 → 更好的 Agent 表现
```

### 3.2 数据收集（已实现于 pkg/aiinfra）

```go
// 前端用户编辑了 Agent 回复后触发
dataset.Add(TrainingExample{
    Prompt:     "请帮我写一个 Python 排序函数",
    Response:   agent_original_response,
    EditedResp: user_edited_response,  // 用户认为更好的版本
    Feedback:   FeedbackEdit,
    AgentType:  "developer",
    Model:      "deepseek-coder:1.3b",
    Score:      4.5,
})

// 导出为微调格式
alpacaData := dataset.ExportForFineTuning("alpaca")
```

### 3.3 微调方法

| 方法 | 参数量 | 显存需求 | 推荐场景 |
|------|--------|---------|---------|
| 全量微调 | 100% | 极高 (80GB+) | 有大量数据 |
| LoRA | 0.1-1% | 较低 (8-16GB) | 通用微调 |
| QLoRA | 0.1-1% | 极低 (4-8GB) | 消费级 GPU |
| RLHF | - | 高 | 对齐人类偏好 |
| DPO | 0.1-1% | 较低 | RLHF 简化版 |

```bash
# 使用 LLaMA-Factory 进行 QLoRA 微调
git clone https://github.com/hiyouga/LLaMA-Factory
cd LLaMA-Factory

# 准备数据（从 AI Corp dataset 导出的 Alpaca 格式）
cp /home/haoqian.li/ai-corp/data/training/alpaca_data.json data/

# 启动微调
llamafactory-cli train \
  --model_name_or_path deepseek-ai/deepseek-coder-1.3b-instruct \
  --dataset alpaca_data \
  --finetuning_type lora \
  --lora_rank 8 \
  --num_train_epochs 3 \
  --per_device_train_batch_size 4 \
  --output_dir ./output/deepseek-coder-finetuned
```

### 3.4 模型评估

```python
# 评估指标
from evaluate import load

# 代码生成评估
code_eval = load("code_eval")
results = code_eval.compute(
    predictions=[generated_code],
    references=[test_cases]
)
# pass@1: 单次生成通过率
# pass@10: 10次生成至少1次通过率

# 对话评估
rouge = load("rouge")
results = rouge.compute(
    predictions=model_outputs,
    references=reference_outputs
)
```

---

## 4. 可视化与监控

### 4.1 推理监控仪表板

**Grafana Dashboard 关键面板：**

```
┌─────────────────────────────────────────────────┐
│  AI Corp - LLM Inference Dashboard              │
├─────────────┬───────────────┬───────────────────┤
│ Req/s       │ Avg Latency   │ Token Throughput  │
│ [图表]      │ [图表]        │ [图表]            │
├─────────────┴───────────────┴───────────────────┤
│  Error Rate by Model                            │
│  [时序图]                                       │
├─────────────────────────────────────────────────┤
│  Queue Depth (扩容信号)  │  Cache Hit Rate      │
│  [实时指标]              │  [饼图]              │
└─────────────────────────────────────────────────┘
```

**Prometheus 指标（AI Corp 已暴露 /metrics 端点）：**

```promql
# 平均推理延迟（P95）
histogram_quantile(0.95, 
  sum(rate(llm_inference_duration_seconds_bucket[5m])) by (le, model)
)

# Token 生成速率
rate(llm_tokens_total[1m])

# 任务队列积压
task_queue_depth > 10  # 告警阈值
```

### 4.2 训练可视化

```bash
# TensorBoard - 训练曲线可视化
tensorboard --logdir=./output/runs

# 关键曲线：
# - Train Loss    (下降代表收敛)
# - Eval Loss     (与 Train Loss 差距大 → 过拟合)
# - Learning Rate (Warmup + Cosine Decay)
# - Gradient Norm (异常大 → 梯度爆炸)
```

### 4.3 模型行为可视化

```python
# Attention 可视化 - 理解模型关注点
import bertviz

tokens = tokenizer(prompt, return_tensors='pt')
outputs = model(**tokens, output_attentions=True)

# 可视化注意力头
bertviz.head_view(
    attention=outputs.attentions,
    tokens=tokenizer.convert_ids_to_tokens(tokens['input_ids'][0])
)
```

### 4.4 W&B（Weights & Biases）实验追踪

```python
import wandb

wandb.init(project="ai-corp-finetuning")
wandb.config.update({
    "model": "deepseek-coder-1.3b",
    "lora_rank": 8,
    "learning_rate": 1e-4,
    "batch_size": 4,
})

# 训练过程中自动记录指标
wandb.log({"train_loss": loss, "eval_loss": eval_loss})
```

---

## 5. AI Corp 实现对照

| AI Infra 能力 | AI Corp 代码位置 | 状态 |
|--------------|----------------|------|
| 推理 KV Cache | `pkg/aiinfra/inference.go` InferenceCache | ✅ 已实现 |
| 推理指标采集 | `pkg/aiinfra/inference.go` InferenceMetrics | ✅ 已实现 |
| 训练数据收集 | `pkg/aiinfra/inference.go` TrainingDataset | ✅ 已实现 |
| 量化配置参考 | `pkg/aiinfra/inference.go` ModelQuantizationGuide | ✅ 已实现 |
| Prometheus 指标 | `pkg/metrics/` + `/metrics` 端点 | ✅ 已实现 |
| 本地推理服务 | `local-llm/mock_server.py` | ✅ 运行中 |
| 双模式 LLM | `pkg/llm/dual_mode.go` | ✅ 已实现 |
| 模型文件 | `models/gguf/*.gguf` | ✅ 已下载 1.3B |
| vLLM 集成 | 待实现（需 GPU） | 🔜 规划中 |
| 微调流水线 | 待实现 | 🔜 规划中 |
