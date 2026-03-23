# vLLM-Kunlun 技术架构深度分析

> 百度基于昆仑芯（Kunlun XPU）自研的大模型推理引擎

## 一、项目概述

**定位**：面向国产 AI 芯片的高性能 LLM 推理引擎
**核心能力**：PagedAttention + 连续批处理 + 多卡并行 + 量化推理
**技术栈**：Python + PyTorch + C++ (XPU Kernel)
**支持模型**：DeepSeek、Qwen、GLM、Llama 等 50+ 主流模型

---

## 二、整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                      API 层 (OpenAI 兼容)                     │
│         /v1/completions /v1/chat/completions                 │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                    调度层 (Scheduler)                        │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐        │
│  │ 请求接收      │ │ 批次合并      │ │ 优先级队列    │        │
│  └──────────────┘ └──────────────┘ └──────────────┘        │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                   执行层 (Worker)                            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                  Model Runner                         │  │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐             │  │
│  │  │ Attention│ │   MLP    │ │  Sampling│             │  │
│  │  │ Backend  │ │  (MoE)   │ │          │             │  │
│  │  └──────────┘ └──────────┘ └──────────┘             │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                   算子层 (Kunlun Ops)                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │PagedAttn │ │ RMSNorm  │ │  MoE     │ │ Quantize │       │
│  │  Kernel  │ │  Kernel  │ │  Kernel  │ │  Kernel  │       │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘       │
└─────────────────────────────────────────────────────────────┘
```

---

## 三、核心技术创新

### 3.1 PagedAttention 内存管理

**问题**：传统 LLM 推理的 KV Cache 内存碎片严重

**解决方案**：将 KV Cache 分块管理，类似操作系统虚拟内存

```python
# vllm_kunlun/ops/_kunlun_ops.py
class KunlunOps:
    @staticmethod
    def paged_attention_v1(
        output, query, key_cache, value_cache,
        num_kv_heads, scale, block_tables, context_lens, ...
    ):
        """
        block_tables: 每个序列的物理块索引映射
        context_lens: 每个序列的实际长度
        """
        kunlun_ops.paged_attention(
            x=query,
            k_cache=key_cache,
            v_cache=value_cache,
            block_tables=block_tables,  # 非连续内存访问
            context_lens_cpu=context_lens_cpu,
            context_lens_xpu=context_lens,
            is_context=is_context,
            is_causal=True,
            out=output,
            vo_head_dim=128,
        )
```

**内存布局**：
```
物理块池 (固定大小，如 16 tokens/块)
┌─────┬─────┬─────┬─────┬─────┬─────┐
│  0  │  1  │  2  │  3  │  4  │  5  │
└─────┴─────┴─────┴─────┴─────┴─────┘

序列 A (长度 35): [0, 2, 4]  ← 物理块不连续
序列 B (长度 20): [1, 3]     ← 可共享空闲块
```

**收益**：
- 内存利用率从 40-60% 提升到 90%+
- 支持更大 batch size
- 减少内存分配开销

### 3.2 连续批处理 (Continuous Batching)

**问题**：静态批处理等待所有请求完成才能返回

**解决方案**：动态添加/移除请求，已完成的请求立即返回

```python
# vllm_kunlun/worker/model_runner.py
class KunlunModelRunner(ModelRunner):
    def execute_model(self, execute_model_req: ExecuteModelRequest):
        # 1. 准备输入（动态合并新请求）
        model_input = self.prepare_model_input(execute_model_req)
        
        # 2. 执行推理
        hidden_states = model_executable(**execute_model_kwargs)
        
        # 3. 采样输出
        output = self.model.sample(
            logits=logits,
            sampling_metadata=model_input.sampling_metadata,
        )
        
        # 4. 已完成的序列立即返回，新请求动态加入
        return output
```

**对比**：

| 批处理类型 | GPU 利用率 | 延迟 | 吞吐 |
|-----------|-----------|------|------|
| 静态批处理 | 40-60% | 高（等待最慢请求）| 低 |
| 连续批处理 | 80-95% | 低（即时返回）| 高 |

### 3.3 MLA (Multi-head Latent Attention)

**问题**：DeepSeek V2/V3 的 KV Cache 压缩方案

**实现**：`vllm_kunlun/v1/attention/backends/mla/flashmla.py`

```python
class FlashMLAImpl(MLAImpl):
    def forward(self, query, kv_c_normed, k_pe, kv_cache, attn_metadata):
        # 1. 低秩解压缩 KV
        kv_nope = self.kv_b_proj(kv_c_normed)[0]
        kv_nope = kv_nope.view(-1, self.num_local_heads, self.qk_nope_head_dim)
        
        # 2. 合并位置编码
        k = torch.cat([
            kv_nope, 
            k_pe.expand(-1, self.num_local_heads, -1)
        ], dim=-1)
        
        # 3. FlashMLA 注意力计算
        return flashmla_attn(query, k, v, kv_cache, attn_metadata)
```

**收益**：
- KV Cache 减少 50-75%
- 支持更长上下文
- 推理速度提升 20-40%

### 3.4 专家并行 (Expert Parallelism)

**适用模型**：DeepSeek V2/V3 (MoE 架构)

**实现**：`vllm_kunlun/ops/fused_moe/layer.py`

```python
class KunlunFusedMoE(FusedMoE):
    def __init__(self, num_experts, top_k, hidden_size, ...):
        # 专家并行配置
        self.tp_size = tp_size  # Tensor Parallel
        self.ep_size = ep_size  # Expert Parallel
        self.dp_size = dp_size  # Data Parallel
        
        # EPLB: Expert Parallelism Load Balancing
        self.enable_eplb = parallel_config.enable_eplb
        self.n_redundant_experts = eplb_config.num_redundant_experts
    
    def forward(self, hidden_states, router_logits):
        # 1. 路由选择专家
        router_results = torch.softmax(router_logits, dim=-1)
        topk_weights, topk_indices = torch.topk(router_results, self.top_k)
        
        # 2. 根据 token 数量选择执行路径
        if M * self.top_k < 400:
            # 小批量优化路径
            return self.moe_forward_small_batch(...)
        else:
            # 大批量标准路径
            return self.moe_forward_fused(...)
```

**并行策略**：
```
总 GPU 数 = TP × EP × DP

示例：8 GPU
- TP=2 (张量并行，单节点内)
- EP=2 (专家并行，跨节点)
- DP=2 (数据并行，复制模型)
```

---

## 四、性能优化技术

### 4.1 算子融合

**融合策略**：将多个小算子合并为一个大算子

```python
# 融合前：3 个 kernel launch
x = rms_norm(x)
x = x + residual
x = rotary_emb(x)

# 融合后：1 个 kernel launch
kunlun_ops.rmsnorm_add_rotary(x, residual, weight, rotary_emb, output)
```

**已融合算子**：
- `rmsnorm_add`：RMSNorm + 残差连接
- `split_norm_rope_neox`：Split + Norm + RoPE
- `silu_and_mul`：SiLU 激活 + 逐元素乘
- `fused_moe`：路由 + 专家计算 + 合并

### 4.2 量化推理

**支持格式**：

| 量化类型 | 精度 | 速度提升 | 适用场景 |
|---------|------|---------|---------|
| FP16 | 16-bit | 1x | 通用 |
| AWQ | 4-bit | 2-3x | 消费级 GPU |
| GPTQ | 4-bit | 2-3x | 批处理场景 |
| FP8 | 8-bit | 1.5x | H100/A100 |

**AWQ 实现**：`vllm_kunlun/ops/quantization/awq.py`

```python
class AWQLinearMethod(LinearMethodBase):
    def apply(self, layer, x, bias):
        # 反量化并计算
        out = torch.empty((x.shape[0], layer.qweight.shape[1] * 8))
        group_size = int(layer.qweight.shape[0] / layer.scales.shape[0])
        
        kunlun_ops.awq_gemm(
            x=x,
            w=layer.qweight,
            scale=layer.scales,
            zeros=layer.qzeros,
            out=out,
            group_size=group_size
        )
        return out
```

### 4.3 Multi-LoRA 推理

**场景**：多租户共享基座模型，各自有 LoRA 适配器

**实现**：`vllm_kunlun/lora/punica_wrapper/punica_kunlun.py`

```python
class PunicaWrapperKunlun(PunicaWrapperBase):
    def add_lora_linear(self, y, x, lora_a_stacked, lora_b_stacked, ...):
        # Shrink: x @ lora_a (低秩压缩)
        self.add_shrink(buffer, x, lora_a_stacked, ...)
        
        # Expand: buffer @ lora_b (低秩恢复)
        self.add_expand(y, buffer, lora_b_stacked, ...)
        
        # 结果合并：基座输出 + LoRA 输出
        return y
```

**性能**：Multi-LoRA 推理达到非 LoRA 性能的 80%+

### 4.4 CUDA Graph 优化

**原理**：将计算图预编译为静态图，减少 CPU 开销

```python
# vllm_kunlun/v1/worker/gpu_model_runner.py
if self.use_cuda_graph:
    # 捕获计算图
    self.graph = torch.cuda.CUDAGraph()
    with torch.cuda.graph(self.graph):
        self.static_output = self.model(self.static_input)
    
    # 重放计算图（无 Python 开销）
    self.graph.replay()
```

**收益**：小 batch 场景提速 10-30%

---

## 五、模型适配架构

### 5.1 平台抽象层

**核心文件**：`vllm_kunlun/platforms/kunlun.py`

```python
class KunlunPlatform(Platform):
    _enum = PlatformEnum.CUDA  # 伪装为 CUDA 以兼容
    
    @classmethod
    def get_attn_backend_cls(cls, selected_backend, head_size, dtype, ...):
        # 根据配置返回对应的注意力后端
        if use_mla:
            return "vllm_kunlun.v1.attention.backends.mla.flashmla.FlashMLABackend"
        if use_v1:
            return "vllm_kunlun.v1.attention.backends.kunlun_attn.KunlunAttentionBackend"
```

### 5.2 Monkey Patch 机制

**核心文件**：`vllm_kunlun/__init__.py`

```python
# 替换原始算子为昆仑芯版本
from vllm.model_executor.layers.fused_moe import layer
layer.UnquantizedFusedMoEMethod = KunlunUnquantizedFusedMoEMethod
layer.FusedMoE = KunlunFusedMoE

# 替换通信函数
from vllm.distributed import parallel_state
parallel_state.GroupCoordinator.all_reduce = vllm_kunlun_all_reduce
```

**优势**：
- 最小化对上游代码的侵入
- 便于同步上游更新
- 清晰的适配边界

### 5.3 PyTorch Custom Operator

**核心文件**：`vllm_kunlun/vllm_utils_wrapper.py`

```python
@custom_op("_C::rmsnorm", mutates_args=())
def rmsnorm(x, weight, output, eps=1e-5):
    kunlun_ops.rmsnorm(x, weight, output, eps)

@impl("_C::rmsnorm", "CUDA")
def rmsnorm_cuda(x, weight, output, eps=1e-5):
    kunlun_ops.rmsnorm(x, weight, output, eps)
```

---

## 六、性能监控与调优

### 6.1 性能指标采集

| 指标 | 采集方式 | 用途 |
|-----|---------|------|
| TTFT | 首 token 生成时间 | 用户体验 |
| TPOT | 每 token 生成时间 | 吞吐评估 |
| Throughput | tokens/s | 容量规划 |
| GPU 利用率 | nvidia-smi | 资源效率 |
| 显存占用 | torch.cuda.memory_allocated | OOM 预防 |

### 6.2 性能调优建议

**1. Batch Size 选择**：
```python
# 小 batch (1-4)：延迟敏感场景
# 大 batch (8-32)：吞吐优先场景
# 动态 batch：连续批处理自动调整
```

**2. 量化策略**：
```python
# 显存受限 → AWQ/GPTQ 4-bit
# 精度优先 → FP16
# 平衡方案 → FP8 (H100)
```

**3. 并行策略**：
```python
# 单卡 → TP=1
# 多卡单节点 → TP=GPU数
# 多节点 → TP=8, EP=N/8
```

---

## 七、与 AI Corp 的关联

### 7.1 当前 AI Corp 的推理架构

```
当前：Python mock_server (模拟响应)
  ↓
目标：集成 vLLM-Kunlun (真实推理)
  ↓
未来：支持多卡并行 + 量化 + 批处理
```

### 7.2 可集成的功能

| 功能 | AI Corp 现状 | 集成 vLLM-Kunlun 后 |
|-----|-------------|-------------------|
| 推理引擎 | Mock 响应 | 真实模型推理 |
| 批处理 | 单请求处理 | 连续批处理 |
| KV Cache | 无 | PagedAttention |
| 量化 | 无 | AWQ/GPTQ 支持 |
| 多卡 | 单 CPU | 多 GPU 并行 |
| 监控 | 基础指标 | 完整性能指标 |

### 7.3 集成方案

```python
# pkg/llm/vllm_client.go
import "github.com/vllm-project/vllm"

type VLLMClient struct {
    baseURL string
    model   string
}

func (c *VLLMClient) Generate(prompt string) (string, error) {
    req := &vllm.CompletionRequest{
        Model: c.model,
        Prompt: prompt,
        MaxTokens: 512,
        Temperature: 0.7,
    }
    resp, err := c.client.CreateCompletion(req)
    return resp.Choices[0].Text, err
}
```

---

## 八、总结

### 8.1 核心设计亮点

1. **PagedAttention**：解决 KV Cache 内存碎片问题
2. **连续批处理**：最大化 GPU 利用率
3. **算子融合**：减少 kernel launch 开销
4. **量化推理**：降低显存占用，提升速度
5. **Monkey Patch**：最小化适配成本

### 8.2 对 AI Corp 的启发

1. **推理优化**：引入 PagedAttention 和连续批处理
2. **量化支持**：支持 AWQ/GPTQ 降低资源消耗
3. **监控体系**：完善的性能指标采集
4. **扩展架构**：插件化设计便于功能扩展

### 8.3 学习价值

- **国产芯片适配**：展示了如何将开源框架适配到国产硬件
- **性能优化技巧**：算子融合、量化、并行等通用技术
- **软件工程实践**：Monkey Patch、Custom Operator 等设计模式
