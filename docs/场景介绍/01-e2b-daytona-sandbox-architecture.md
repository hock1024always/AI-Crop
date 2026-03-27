# E2B / Daytona 风格隔离执行架构（场景介绍）

本系统实现了“每个任务在独立沙箱内执行”的安全隔离能力：默认使用 Docker 容器，并通过 `--cap-drop=ALL` + seccomp + `--network none` + 内存/CPU/进程数硬限制来降低越权与逃逸风险；任务失败/超时只会影响该任务对应的沙箱，不影响宿主机与其他任务。

> 参考实现位置：
> - Docker 沙箱核心：`pkg/sandbox/docker_sandbox.go`
> - 架构总览：`docs/ARCHITECTURE.md`
> - Phase2 沙箱文档：`docs/10-phase2-features.md`

---

## 1. 如果 Docker 容器想联网，应该怎么实现？

在你的约束里（`--network none` 默认隔离），容器内代码**不能直接访问公网**。要实现“需要联网的能力”，通常有三种做法，本项目对应了其中两类：

### 1.1 通过宿主/Orchestrator 代为联网（推荐）

把所有“外网访问”变成宿主层的能力，而沙箱只负责计算与生成结果：

1. Orchestrator/Agent Runtime 需要联网时，在容器外发起请求（例如调用外部 API、抓取网页、下载依赖）。
2. 将结果（文本/文件/压缩包）写回沙箱挂载的工作目录 `/workspace`，或作为输入传给沙箱执行逻辑。
3. 沙箱在离线状态下完成后续代码运行、解析、分析等任务。

这样做的核心收益是：沙箱始终保持 `--network none`，外网风险不会进入执行环境。

### 1.2 允许网络时采用“internal 网络 + 域名白名单”（可选）

项目也支持某些沙箱模板启用网络，例如 `WebScraperSandbox()`。在代码里：

- `DefaultSandboxConfig()` 默认 `NetworkEnabled: false`
- `WebScraperSandbox()` 会设置 `NetworkEnabled: true`，并提供 `AllowedHosts: []string{"wikipedia.org", "github.com"}`
- 在 `buildDockerRunArgs()` 中：
  - `NetworkEnabled == false`：追加 `--network none`
  - `NetworkEnabled == true && AllowedHosts 非空`：使用 `--network ai-corp-sandbox-net`
  - 同时为允许域名注入解析记录（当前实现通过 `--add-host` 组装）

并且 `ensureSandboxNetwork()` 创建的 `ai-corp-sandbox-net` 使用了 `--internal`，意味着该网络**没有外网出口**，降低了“容器里任意联网”的风险。

### 1.3 预下载依赖（构建时/镜像时完成）

对于经常用到的依赖（如 pip/npm 包、模型文件、离线数据集），可以在镜像构建阶段预先下载。运行时沙箱保持无网络，从而稳定、可审计、可复现。

---

## 2. 每个 Docker 的生命周期是“AI 员工”还是“任务”？

在本项目里，Docker 沙箱的生命周期是**“任务执行（task run / task attempt）生命周期”**，而不是“AI 员工（Agent）生命周期”。

对应实现点：

- `SandboxManager.CreateSandbox(...)`：为某个 `taskID` 创建一个容器，并返回 sandbox 对象（容器状态从 creating -> running）。
- `SandboxManager.ExecuteInSandbox(...)`：对该 sandbox 对应的容器执行 `docker exec`。
- `TaskSandbox.Cleanup()` / `SandboxManager.StopSandbox(...)`：停止容器、清理工作目录（`/tmp/.../sandboxID`），并更新状态。
- `docker run` 使用 `--rm`：容器退出后自动删除（配合 Cleanup 的 `docker stop`）。

换句话说：

- Agent（研发/测试/架构/运维等）是“长期存在的逻辑/运行协调者”
- 每个具体任务（或任务的某次尝试）会临时拉起一个隔离容器来执行

因此任务失败/超时只会导致该沙箱失败或超时清理，不应影响其他任务或其他 Agent。

---

## 3. Docker 之间如何通信？是统一 API-server 还是直接交互？

结论：在默认 `--network none` 的策略下，Docker 之间**不依赖容器互相直连**；它们通过“宿主侧控制面”进行通信。

本项目的通信链路更接近下面这种分层：

1. **控制面（Orchestrator / Agent Runtime）**：负责任务分配、结果回收、状态流转（例如 `task_complete / task_fail`）。
2. **数据面（沙箱输出/工作目录/DB）**：
   - `ExecuteInSandbox` 通过 `docker exec` 获取容器内 stdout/stderr（作为结果）
   - `/workspace` 挂载到宿主工作目录，用于输入输出交换（但网络仍默认禁用）
   - 结果/指标/审计等持久化到 PostgreSQL；并通过 Prometheus/Grafana 做监控

当需要跨沙箱的协作（例如多个任务共享“经验/反思/技能”）时，本项目使用“记忆/向量库/共享记忆”机制，让 Orchestrator 将必要信息注入到后续任务的 system prompt（而不是让容器互相发请求）。

---

## 4. “模仿 E2B 架构”的部分：E2B/Daytona 长什么样？我们模仿了什么？

### 4.1 E2B / Daytona 的典型形态（抽象视角）

典型做法可以抽象成“Supervisor（控制器） + Ephemeral Sandbox（短生命周期执行环境） + 工具/联网由控制器代办”：

```text
Client / Agent 调用
        |
        v
Supervisor（任务控制面）
  - 申请资源与隔离配置
  - 启动一次性沙箱（容器/微虚拟机）
  - 流式收集输出/日志
  - 超时/失败后销毁沙箱
        |
        v
Ephemeral Sandbox（任务执行面）
  - 无特权/受限系统调用（seccomp）
  - 去能力（cap-drop）
  - 默认无网络（--network none）
  - 资源上限（内存/CPU/进程数）
```

关键点是：沙箱尽可能“只做纯执行”；需要“外部世界”的能力（联网、文件下载、API 调用等）尽量通过控制面完成，然后把结果回灌给沙箱。

### 4.2 我们的架构模仿了哪些点？

在本项目里，对应关系基本是：

1. **Supervisor / Control Plane 对应 Orchestrator + SandboxManager**
   - `docs/ARCHITECTURE.md`：Orchestrator 统一调度任务、触发执行与事件回收
   - `pkg/sandbox/docker_sandbox.go`：`SandboxManager` 负责创建容器、执行命令、超时处理、清理资源

2. **Ephemeral Sandbox（短生命周期）对应“每任务创建的 Docker 容器”**
   - `CreateSandbox` 为 `taskID` 创建一次运行环境
   - `ExecuteInSandbox` 用 `docker exec` 在容器内执行
   - `Cleanup/StopSandbox` 负责停止并删除工作目录（避免残留）

3. **默认无网络对应 `--network none`**
   - `DefaultSandboxConfig()` 的 `NetworkEnabled: false`
   - `buildDockerRunArgs()` 在 `!NetworkEnabled` 时追加 `--network none`

4. **可选受控网络对应 “internal 网络 + 白名单域名”**
   - `ensureSandboxNetwork()` 创建 `--internal ai-corp-sandbox-net`
   - `WebScraperSandbox()` 通过 `AllowedHosts` 进入可网络模式（仅用于明确任务类型）

5. **安全基线对应 `--cap-drop=ALL` + seccomp + no-new-privileges**
   - `buildDockerRunArgs()` 里包含 `--cap-drop=ALL`、`--no-new-privileges`、`seccomp=default`

6. **任务失败隔离对应超时/清理逻辑**
   - `ExecuteInSandbox` 使用 `context.WithTimeout` 判定 timeout
   - 超时/失败只改变该 sandbox 状态，最终由 Cleanup 清理，不让宿主机“积累不受控的进程/资源”

---

## 小结

你问的四点，本项目的落地方式可以概括为：

1. 默认 `--network none`：联网能力通过控制面（宿主/Orchestrator）代办；或在特定沙箱里启用 internal+白名单。
2. Docker 生命周期：属于“任务执行”，不是“AI 员工”本体。
3. Docker 通信：不依赖容器互连；通过 Orchestrator、stdout/stderr 回传、挂载目录、DB/向量库实现协作。
4. E2B/Daytona 模仿：核心模仿的是 Supervisor 管理短生命周期隔离执行环境 + 沙箱默认无网络 + 安全基线与超时回收机制。

