#!/usr/bin/env python3
"""
本地 LLM 服务 - 模拟 Ollama API
使用 ctransformers 运行 GGUF 格式的本地模型
"""

import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse
import threading
import time

# 本地模型路径
MODEL_BASE_PATH = "/home/haoqian.li/ai-corp/models/gguf"

# 模型缓存
loaded_models = {}
model_configs = {
    "deepseek-coder:1.3b": {
        "local_path": f"{MODEL_BASE_PATH}/deepseek-coder-1.3b.Q4_K_M.gguf",
        "model_type": "llama",
        "context_length": 4096,
        "gpu_layers": 0,  # CPU only
        "available": True
    },
    "deepseek-coder:6.7b": {
        "model_id": "TheBloke/deepseek-coder-6.7b-instruct-GGUF",
        "model_file": "deepseek-coder-6.7b-instruct.Q4_K_M.gguf",
        "context_length": 8192,
        "gpu_layers": 0,
        "available": False  # 需要下载
    },
    "qwen2.5:3b": {
        "model_id": "Qwen/Qwen2.5-3B-Instruct-GGUF",
        "model_file": "qwen2.5-3b-instruct-q4_k_m.gguf",
        "context_length": 8192,
        "gpu_layers": 0,
        "available": False
    },
    "llama3.2:3b": {
        "model_id": "llamaste/Llama-3.2-3B-Instruct-GGUF",
        "model_file": "llama-3.2-3b-instruct-q4_k_m.gguf",
        "context_length": 8192,
        "gpu_layers": 0,
        "available": False
    },
    "codellama:7b": {
        "model_id": "TheBloke/CodeLlama-7B-Instruct-GGUF",
        "model_file": "codellama-7b-instruct.Q4_K_M.gguf",
        "context_length": 4096,
        "gpu_layers": 0,
        "available": False
    }
}

def get_model(model_name):
    """获取或加载模型"""
    if model_name in loaded_models:
        return loaded_models[model_name]
    
    if model_name not in model_configs:
        # 尝试模糊匹配
        for key in model_configs:
            if model_name.lower() in key.lower() or key.lower() in model_name.lower():
                model_name = key
                break
        else:
            return None
    
    config = model_configs[model_name]
    
    # 检查模型是否可用
    if not config.get('available', False):
        print(f"[WARN] 模型 {model_name} 尚未下载，请先下载模型")
        return None
    
    try:
        from ctransformers import AutoModelForCausalLM
        
        print(f"[INFO] 正在加载模型: {model_name}")
        
        # 优先使用本地路径
        if 'local_path' in config:
            print(f"[INFO] 本地路径: {config['local_path']}")
            model = AutoModelForCausalLM(
                model_path=config['local_path'],
                model_type=config.get('model_type', 'llama'),
                context_length=config['context_length'],
                gpu_layers=config['gpu_layers']
            )
        else:
            print(f"[INFO] 从远程加载: {config['model_id']}")
            model = AutoModelForCausalLM.from_pretrained(
                config['model_id'],
                model_file=config['model_file'],
                context_length=config['context_length'],
                gpu_layers=config['gpu_layers']
            )
        
        loaded_models[model_name] = model
        print(f"[INFO] 模型 {model_name} 加载成功!")
        return model
    except Exception as e:
        print(f"[ERROR] 加载模型失败: {e}")
        import traceback
        traceback.print_exc()
        return None


class OllamaAPIHandler(BaseHTTPRequestHandler):
    """模拟 Ollama API 的 HTTP 处理器"""
    
    def log_message(self, format, *args):
        """自定义日志格式"""
        print(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {format % args}")
    
    def send_json_response(self, data, status=200):
        """发送 JSON 响应"""
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Access-Control-Allow-Origin', '*')
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False).encode('utf-8'))
    
    def do_GET(self):
        """处理 GET 请求"""
        parsed_path = urlparse(self.path)
        path = parsed_path.path
        
        if path == '/api/tags' or path == '/api/models':
            # 列出可用模型
            models = []
            for name, config in model_configs.items():
                available = config.get('available', False)
                models.append({
                    "name": name,
                    "modified_at": time.strftime("%Y-%m-%dT%H:%M:%S.000000Z"),
                    "size": 834000000 if available else 0,
                    "digest": "local" if available else "not_downloaded",
                    "details": {
                        "format": "gguf",
                        "family": name.split(':')[0],
                        "parameter_size": name.split(':')[1] if ':' in name else "unknown",
                        "available": available
                    }
                })
            self.send_json_response({"models": models})
            
        elif path == '/api/version':
            self.send_json_response({"version": "0.1.0-local"})
            
        elif path.startswith('/api/show'):
            # 显示模型详情
            query = parsed_path.query
            model_name = query.replace('name=', '') if 'name=' in query else 'unknown'
            
            if model_name in model_configs:
                config = model_configs[model_name]
                self.send_json_response({
                    "license": "MIT",
                    "modelfile": f"FROM {config['model_id']}",
                    "parameters": {
                        "num_ctx": config['context_length']
                    },
                    "details": {
                        "format": "gguf",
                        "family": model_name.split(':')[0]
                    }
                })
            else:
                self.send_json_response({"error": "model not found"}, 404)
        else:
            self.send_json_response({"status": "ok", "message": "Local LLM Server (Ollama-compatible)"})
    
    def do_POST(self):
        """处理 POST 请求"""
        parsed_path = urlparse(self.path)
        path = parsed_path.path
        
        content_length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(content_length).decode('utf-8')
        
        try:
            data = json.loads(body) if body else {}
        except json.JSONDecodeError:
            self.send_json_response({"error": "Invalid JSON"}, 400)
            return
        
        if path == '/api/generate':
            self.handle_generate(data)
        elif path == '/api/chat':
            self.handle_chat(data)
        elif path == '/api/embeddings':
            self.handle_embeddings(data)
        else:
            self.send_json_response({"error": "Unknown endpoint"}, 404)
    
    def handle_generate(self, data):
        """处理生成请求"""
        model_name = data.get('model', 'deepseek-coder:1.3b')
        prompt = data.get('prompt', '')
        stream = data.get('stream', False)
        
        model = get_model(model_name)
        if model is None:
            self.send_json_response({"error": f"Model {model_name} not found or failed to load"}, 404)
            return
        
        try:
            # 生成文本
            start_time = time.time()
            response_text = model(prompt, max_new_tokens=512, temperature=0.7)
            elapsed = time.time() - start_time
            
            result = {
                "model": model_name,
                "created_at": time.strftime("%Y-%m-%dT%H:%M:%S.000000Z"),
                "response": response_text,
                "done": True,
                "context": [],
                "total_duration": int(elapsed * 1e9),
                "load_duration": 0,
                "prompt_eval_count": len(prompt.split()),
                "prompt_eval_duration": 0,
                "eval_count": len(response_text.split()),
                "eval_duration": int(elapsed * 1e9)
            }
            self.send_json_response(result)
            
        except Exception as e:
            self.send_json_response({"error": str(e)}, 500)
    
    def handle_chat(self, data):
        """处理聊天请求"""
        model_name = data.get('model', 'deepseek-coder:1.3b')
        messages = data.get('messages', [])
        stream = data.get('stream', False)
        
        model = get_model(model_name)
        if model is None:
            self.send_json_response({"error": f"Model {model_name} not found or failed to load"}, 404)
            return
        
        try:
            # 构建提示词
            prompt = ""
            for msg in messages:
                role = msg.get('role', 'user')
                content = msg.get('content', '')
                if role == 'system':
                    prompt += f"System: {content}\n"
                elif role == 'user':
                    prompt += f"User: {content}\n"
                elif role == 'assistant':
                    prompt += f"Assistant: {content}\n"
            
            prompt += "Assistant: "
            
            # 生成响应
            start_time = time.time()
            response_text = model(prompt, max_new_tokens=512, temperature=0.7)
            elapsed = time.time() - start_time
            
            result = {
                "model": model_name,
                "created_at": time.strftime("%Y-%m-%dT%H:%M:%S.000000Z"),
                "message": {
                    "role": "assistant",
                    "content": response_text
                },
                "done": True,
                "total_duration": int(elapsed * 1e9),
                "eval_count": len(response_text.split()),
                "eval_duration": int(elapsed * 1e9)
            }
            self.send_json_response(result)
            
        except Exception as e:
            self.send_json_response({"error": str(e)}, 500)
    
    def handle_embeddings(self, data):
        """处理嵌入请求（简化实现）"""
        self.send_json_response({
            "embeddings": [0.0] * 768,  # 简化的嵌入向量
            "model": data.get('model', 'unknown')
        })


def main():
    """主函数"""
    port = int(os.environ.get('OLLAMA_PORT', 11434))
    host = os.environ.get('OLLAMA_HOST', '0.0.0.0')
    
    print("=" * 60)
    print("  本地 LLM 服务 (Ollama API 兼容)")
    print("  使用 ctransformers 运行 GGUF 模型")
    print("=" * 60)
    print(f"\n[INFO] 服务地址: http://{host}:{port}")
    print(f"[INFO] API 端点:")
    print(f"  - GET  /api/tags     - 列出可用模型")
    print(f"  - POST /api/generate - 生成文本")
    print(f"  - POST /api/chat     - 聊天对话")
    print(f"\n[INFO] 可用模型:")
    for name in model_configs:
        print(f"  - {name}")
    print("\n[INFO] 正在启动服务...")
    
    server = HTTPServer((host, port), OllamaAPIHandler)
    
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[INFO] 服务已停止")
        server.shutdown()


if __name__ == '__main__':
    main()
