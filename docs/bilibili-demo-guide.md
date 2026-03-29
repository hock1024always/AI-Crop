# AI Corp 演示操作手册

> 录制 Bilibili 视频时的演示操作指南，按顺序执行。

---

## 前置准备

```bash
# 确保 PostgreSQL 运行
pg_ctl -D /usr/local/pgsql/data status

# 确保 Ollama 运行（如果要演示本地模型）
ollama list

# 编译最新代码
cd /path/to/ai-corp
go build -o orchestrator ./cmd/orchestrator/
```

---

## 演示 1：启动服务

```bash
# 启动 Orchestrator
export LLM_API_KEY=your_key    # 或不设置，用 Ollama
export JWT_SECRET=demo-secret
./orchestrator
```

期望输出：
```
LLM client initialized: ...
Ollama client initialized: ...
Database connected: ...
Inference service initialized ...
Self-improvement loop initialized ...
Orchestrator starting on :8080
```

---

## 演示 2：创建 Agent（curl 命令）

```bash
# 创建后端开发 Agent
curl -s http://localhost:8080/api/v1/agents -X POST \
  -H "Content-Type: application/json" \
  -d '{"name":"Atlas","type":"developer"}' | python3 -m json.tool

# 创建测试 Agent
curl -s http://localhost:8080/api/v1/agents -X POST \
  -H "Content-Type: application/json" \
  -d '{"name":"Sentinel","type":"tester"}' | python3 -m json.tool

# 查看 Agent 列表
curl -s http://localhost:8080/api/v1/agents | python3 -m json.tool
```

---

## 演示 3：与 Agent 对话（展示记忆注入）

```bash
# 第一次对话
curl -s http://localhost:8080/api/v1/chat -X POST \
  -H "Content-Type: application/json" \
  -d '{"message":"请帮我设计一个用户登录的 API","agent_type":"developer"}' | python3 -m json.tool

# 第二次对话（如果自我迭代已运行，会注入第一次的经验）
curl -s http://localhost:8080/api/v1/chat -X POST \
  -H "Content-Type: application/json" \
  -d '{"message":"登录 API 需要加上限流吗？","agent_type":"developer"}' | python3 -m json.tool
```

看终端日志中 `[Chat] Injected N memories` 表示记忆注入成功。

---

## 演示 4：PII 脱敏

```bash
# 测试 PII 检测
curl -s http://localhost:8080/api/v1/pii/check -X POST \
  -H "Content-Type: application/json" \
  -d '{"text":"我的手机是13812345678，身份证110101199001011234，邮箱test@example.com"}' | python3 -m json.tool
```

期望输出：
```json
{
  "has_pii": true,
  "detections": [...],
  "sanitized": "我的手机是138****5678，身份证110***********1234，邮箱t***@example.com"
}
```

---

## 演示 5：JWT 认证

```bash
# 生成 Token
curl -s http://localhost:8080/api/v1/auth/token -X POST \
  -H "Content-Type: application/json" \
  -d '{"user_id":"demo_user","role":"admin"}' | python3 -m json.tool

# 用 Token 请求（替换 <TOKEN>）
curl -s http://localhost:8080/api/v1/quota/stats \
  -H "Authorization: Bearer <TOKEN>" | python3 -m json.tool
```

---

## 演示 6：Token 配额

```bash
# 查看配额状态
curl -s http://localhost:8080/api/v1/quota/stats | python3 -m json.tool

# 连续发送多个 chat 请求后再查看（会看到 token 使用量增加）
```

---

## 演示 7：创建并运行工作流

```bash
# 创建外包项目工作流
curl -s http://localhost:8080/api/v1/workflows -X POST \
  -H "Content-Type: application/json" \
  -d '{"name":"电商网站","template":"outsourcing"}' | python3 -m json.tool

# 运行工作流（替换 <WORKFLOW_ID>）
curl -s http://localhost:8080/api/v1/workflows/<WORKFLOW_ID>/run -X POST | python3 -m json.tool

# 查看工作流状态
curl -s http://localhost:8080/api/v1/workflows/<WORKFLOW_ID> | python3 -m json.tool
```

---

## 演示 8：审计日志

```bash
# 查看最近的审计日志（前面的操作都已经自动记录）
curl -s http://localhost:8080/api/v1/db/audit | python3 -m json.tool
```

---

## 演示 9：查看监控指标

```bash
# Prometheus 格式
curl -s http://localhost:8080/metrics | head -30

# JSON 格式
curl -s http://localhost:8080/api/v1/metrics | python3 -m json.tool

# 数据库统计
curl -s http://localhost:8080/api/v1/db/stats | python3 -m json.tool
```

---

## 演示 10：运行测试套件

```bash
# 运行全部测试
go test ./pkg/security/ ./pkg/workflow/ ./pkg/message/ ./pkg/memory/ ./pkg/sandbox/ -v -count=1 2>&1 | tail -20

# 期望看到: ok 和 71 个 PASS
```

---

## 演示顺序建议

1. 启动服务 → 展示日志输出，说明各组件初始化
2. 创建 Agent → 展示 API 设计
3. 对话 → 展示 LLM 调用链路（Ollama 优先 → 云端回退）+ 记忆注入
4. PII 脱敏 → 展示安全过滤能力
5. JWT + 配额 → 展示认证和限流
6. 工作流 → 展示 DAG 引擎并行执行
7. 审计日志 → 说明前面所有操作都自动记录了
8. 运行测试 → 展示 71 个测试全部通过

每个演示之间可以切到代码编辑器展示关键代码，参考讲解稿中的"关键代码展示清单"。
