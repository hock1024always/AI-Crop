#!/usr/bin/env python3
"""
从 ModelScope 下载 DeepSeek Coder 模型
"""

import os
import sys

def download_from_modelscope():
    """从 ModelScope 下载模型"""
    try:
        from modelscope import snapshot_download
        
        print("=" * 60)
        print("  从 ModelScope 下载 DeepSeek Coder 1.3B 模型")
        print("=" * 60)
        
        # DeepSeek Coder 1.3B Instruct - 较小，适合测试
        model_id = 'deepseek-ai/deepseek-coder-1.3b-instruct'
        
        print(f"\n[INFO] 正在下载模型: {model_id}")
        print("[INFO] 这可能需要几分钟时间...\n")
        
        model_dir = snapshot_download(
            model_id,
            cache_dir='/home/haoqian.li/ai-corp/models'
        )
        
        print(f"\n[SUCCESS] 模型下载完成!")
        print(f"[INFO] 模型路径: {model_dir}")
        
        return model_dir
        
    except Exception as e:
        print(f"[ERROR] 下载失败: {e}")
        import traceback
        traceback.print_exc()
        return None

def download_gguf_model():
    """下载 GGUF 格式的模型（更小更快）"""
    try:
        from huggingface_hub import hf_hub_download
        
        print("=" * 60)
        print("  下载 DeepSeek Coder GGUF 模型")
        print("=" * 60)
        
        # GGUF 格式，量化后更小
        model_id = "TheBloke/deepseek-coder-1.3b-instruct-GGUF"
        filename = "deepseek-coder-1.3b-instruct.Q4_K_M.gguf"
        
        print(f"\n[INFO] 正在下载: {model_id}/{filename}")
        print("[INFO] 文件大小约 800MB...\n")
        
        model_path = hf_hub_download(
            repo_id=model_id,
            filename=filename,
            cache_dir='/home/haoqian.li/ai-corp/models'
        )
        
        print(f"\n[SUCCESS] GGUF 模型下载完成!")
        print(f"[INFO] 模型路径: {model_path}")
        
        return model_path
        
    except Exception as e:
        print(f"[ERROR] GGUF 下载失败: {e}")
        import traceback
        traceback.print_exc()
        return None

if __name__ == '__main__':
    # 先尝试 GGUF 格式（更适合 CPU 推理）
    model_path = download_gguf_model()
    
    if not model_path:
        print("\n[INFO] 尝试从 ModelScope 下载...")
        model_path = download_from_modelscope()
    
    if model_path:
        print("\n" + "=" * 60)
        print("  模型下载成功！")
        print("=" * 60)
        print(f"\n可以使用以下命令测试:")
        print(f"  python3 -c \"from ctransformers import AutoModelForCausalLM; m = AutoModelForCausalLM.from_pretrained('{model_path}'); print(m('Hello'))\"")
