#!/usr/bin/env python3
"""
本地 LLM 服务 - 模拟 Ollama API (模拟模式)
用于测试前端界面，返回模拟响应
"""

import json
import os
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse
import time
import random

# 模型配置
MODEL_BASE_PATH = "/home/haoqian.li/ai-corp/models/gguf"
model_configs = {
    "deepseek-coder:1.3b": {
        "local_path": f"{MODEL_BASE_PATH}/deepseek-coder-1.3b.Q4_K_M.gguf",
        "context_length": 4096,
        "available": True,
        "size": 834000000
    },
    "deepseek-coder:6.7b": {
        "context_length": 8192,
        "available": False,
        "size": 0
    },
    "qwen2.5:3b": {
        "context_length": 8192,
        "available": False,
        "size": 0
    },
    "llama3.2:3b": {
        "context_length": 8192,
        "available": False,
        "size": 0
    },
    "codellama:7b": {
        "context_length": 4096,
        "available": False,
        "size": 0
    }
}

# 模拟响应模板
MOCK_RESPONSES = {
    "code": """Here's a code solution:

```python
def hello_world():
    print("Hello, World!")
    return True

if __name__ == "__main__":
    hello_world()
```

This is a simple Python function that prints "Hello, World!" to the console.""",
    
    "chat": """I'm a local AI assistant running on DeepSeek Coder 1.3B model. I can help you with:

1. Code generation and review
2. Debugging assistance  
3. Technical questions
4. Project planning

How can I assist you today?""",
    
    "default": """I understand your request. As a local AI model running on CPU, I'm processing your input.

Note: This is running in simulation mode for testing purposes. To enable full functionality, please ensure the model is properly loaded.

Your request has been received and I'm ready to help with programming tasks, code review, or technical questions."""
}

def get_mock_response(prompt):
    """生成模拟响应"""
    prompt_lower = prompt.lower()
    
    if any(kw in prompt_lower for kw in ['code', 'write', 'function', 'program', 'python', 'javascript', 'java']):
        return MOCK_RESPONSES["code"]
    elif any(kw in prompt_lower for kw in ['hello', 'hi', 'help', 'what can you']):
        return MOCK_RESPONSES["chat"]
    else:
        return MOCK_RESPONSES["default"]


class OllamaAPIHandler(BaseHTTPRequestHandler):
    """模拟 Ollama API 的 HTTP 处理器"""
    
    def log_message(self, format, *args):
        print(f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {format % args}")
    
    def send_json_response(self, data, status=200):
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS')
        self.send_header('Access-Control-Allow-Headers', 'Content-Type')
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False).encode('utf-8'))
    
    def do_OPTIONS(self):
        self.send_response(200)
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS')
        self.send_header('Access-Control-Allow-Headers', 'Content-Type')
        self.end_headers()
    
    def do_GET(self):
        parsed_path = urlparse(self.path)
        path = parsed_path.path
        
        if path == '/api/tags' or path == '/api/models':
            models = []
            for name, config in model_configs.items():
                available = config.get('available', False)
                models.append({
                    "name": name,
                    "modified_at": time.strftime("%Y-%m-%dT%H:%M:%S.000000Z"),
                    "size": config.get('size', 0),
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
            self.send_json_response({"version": "0.1.0-local-mock"})
            
        elif path == '/':
            self.send_json_response({
                "status": "ok",
                "message": "Local LLM Server (Ollama-compatible Mock Mode)",
                "models_available": sum(1 for c in model_configs.values() if c.get('available'))
            })
        else:
            self.send_json_response({"error": "Not found"}, 404)
    
    def do_POST(self):
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
        model_name = data.get('model', 'deepseek-coder:1.3b')
        prompt = data.get('prompt', '')
        
        # 模拟处理延迟
        time.sleep(0.5 + random.random() * 0.5)
        
        response_text = get_mock_response(prompt)
        
        result = {
            "model": model_name,
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%S.000000Z"),
            "response": response_text,
            "done": True,
            "context": [],
            "total_duration": 500000000,
            "load_duration": 0,
            "prompt_eval_count": len(prompt.split()),
            "prompt_eval_duration": 100000000,
            "eval_count": len(response_text.split()),
            "eval_duration": 400000000
        }
        self.send_json_response(result)
    
    def handle_chat(self, data):
        model_name = data.get('model', 'deepseek-coder:1.3b')
        messages = data.get('messages', [])
        
        # 构建提示词
        prompt = ""
        for msg in messages:
            content = msg.get('content', '')
            prompt += content + " "
        
        # 模拟处理延迟
        time.sleep(0.5 + random.random() * 0.5)
        
        response_text = get_mock_response(prompt)
        
        result = {
            "model": model_name,
            "created_at": time.strftime("%Y-%m-%dT%H:%M:%S.000000Z"),
            "message": {
                "role": "assistant",
                "content": response_text
            },
            "done": True,
            "total_duration": 500000000,
            "eval_count": len(response_text.split()),
            "eval_duration": 400000000
        }
        self.send_json_response(result)
    
    def handle_embeddings(self, data):
        # 返回模拟嵌入向量
        embedding = [random.gauss(0, 0.1) for _ in range(768)]
        self.send_json_response({
            "embeddings": embedding,
            "model": data.get('model', 'unknown')
        })


def main():
    port = int(os.environ.get('OLLAMA_PORT', 11434))
    host = os.environ.get('OLLAMA_HOST', '0.0.0.0')
    
    print("=" * 60)
    print("  本地 LLM 服务 (Ollama API 兼容 - 模拟模式)")
    print("=" * 60)
    print(f"\n[INFO] 服务地址: http://{host}:{port}")
    print(f"[INFO] API 端点:")
    print(f"  - GET  /api/tags     - 列出可用模型")
    print(f"  - POST /api/generate - 生成文本")
    print(f"  - POST /api/chat     - 聊天对话")
    print(f"\n[INFO] 可用模型:")
    for name, config in model_configs.items():
        status = "已下载" if config.get('available') else "未下载"
        print(f"  - {name} [{status}]")
    print("\n[INFO] 正在启动服务...")
    
    server = HTTPServer((host, port), OllamaAPIHandler)
    
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[INFO] 服务已停止")
        server.shutdown()


if __name__ == '__main__':
    main()
