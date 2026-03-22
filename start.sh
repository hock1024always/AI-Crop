#!/bin/bash

# AI Corp - 启动脚本

set -e

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_DIR"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 显示帮助
show_help() {
    echo "AI Corp - 多智能体协作平台"
    echo ""
    echo "用法: $0 <命令> [选项]"
    echo ""
    echo "命令:"
    echo "  start       启动所有服务"
    echo "  stop        停止所有服务"
    echo "  restart     重启所有服务"
    echo "  orchestrator 启动 Orchestrator"
    echo "  agent       启动 Agent (需要指定类型)"
    echo "  build       构建所有二进制"
    echo "  test        运行测试"
    echo "  clean       清理构建产物"
    echo ""
    echo "Agent 类型: developer, tester, architect, devops"
    echo ""
    echo "示例:"
    echo "  $0 start"
    echo "  $0 agent developer"
    echo "  $0 build"
}

# 启动 Orchestrator
start_orchestrator() {
    log_info "启动 Orchestrator..."
    go run cmd/orchestrator/main.go &
    ORCH_PID=$!
    echo $ORCH_PID > /tmp/ai-corp-orchestrator.pid
    log_info "Orchestrator 已启动 (PID: $ORCH_PID)"
    log_info "访问 http://localhost:8080"
}

# 启动 Agent
start_agent() {
    local agent_type=$1
    local agent_id="${agent_type}-$(date +%s | tail -c 4)"
    local agent_name="${agent_type}-agent"

    case $agent_type in
        developer)
            agent_name="研发工程师"
            ;;
        tester)
            agent_name="测试工程师"
            ;;
        architect)
            agent_name="架构师"
            ;;
        devops)
            agent_name="运维工程师"
            ;;
        *)
            log_error "未知 Agent 类型: $agent_type"
            exit 1
            ;;
    esac

    log_info "启动 Agent: $agent_id ($agent_name)..."
    AGENT_ID=$agent_id AGENT_NAME="$agent_name" AGENT_TYPE=$agent_type \
        go run cmd/agent-runtime/main.go &
    AGENT_PID=$!
    echo $AGENT_PID > /tmp/ai-corp-agent-${agent_id}.pid
    log_info "Agent 已启动 (PID: $AGENT_PID)"
}

# 停止所有服务
stop_all() {
    log_info "停止所有服务..."

    # 停止 Orchestrator
    if [ -f /tmp/ai-corp-orchestrator.pid ]; then
        PID=$(cat /tmp/ai-corp-orchestrator.pid)
        if kill -0 $PID 2>/dev/null; then
            kill $PID
            log_info "Orchestrator 已停止"
        fi
        rm -f /tmp/ai-corp-orchestrator.pid
    fi

    # 停止所有 Agent
    for pid_file in /tmp/ai-corp-agent-*.pid; do
        if [ -f "$pid_file" ]; then
            PID=$(cat "$pid_file")
            if kill -0 $PID 2>/dev/null; then
                kill $PID
            fi
            rm -f "$pid_file"
        fi
    done

    log_info "所有服务已停止"
}

# 构建所有二进制
build_all() {
    log_info "构建所有二进制..."
    mkdir -p bin
    go build -o bin/orchestrator cmd/orchestrator/main.go
    go build -o bin/agent-runtime cmd/agent-runtime/main.go
    go build -o bin/problem-cli cmd/problem-cli/main.go
    log_info "构建完成: bin/"
    ls -la bin/
}

# 运行测试
run_tests() {
    log_info "运行测试..."
    go test ./... -v
}

# 清理
clean() {
    log_info "清理构建产物..."
    rm -rf bin/
    log_info "清理完成"
}

# 主入口
case "$1" in
    start)
        start_orchestrator
        sleep 2
        start_agent developer
        start_agent tester
        log_info "所有服务已启动"
        log_info "访问 http://localhost:8080"
        ;;
    stop)
        stop_all
        ;;
    restart)
        stop_all
        sleep 1
        start_orchestrator
        sleep 2
        start_agent developer
        ;;
    orchestrator)
        start_orchestrator
        ;;
    agent)
        if [ -z "$2" ]; then
            log_error "请指定 Agent 类型: developer, tester, architect, devops"
            exit 1
        fi
        start_agent $2
        ;;
    build)
        build_all
        ;;
    test)
        run_tests
        ;;
    clean)
        clean
        ;;
    *)
        show_help
        ;;
esac
