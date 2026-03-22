#!/bin/bash

# ============================================
# AI Corp 一键部署脚本
# ============================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查命令是否存在
check_command() {
    if ! command -v $1 &> /dev/null; then
        return 1
    fi
    return 0
}

# 检测操作系统
detect_os() {
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        echo "linux"
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        echo "macos"
    else
        echo "unknown"
    fi
}

# 主函数
main() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║                                                          ║"
    echo "║       ⚔️  AI Corp 像素酒馆 - 一键部署脚本  ⚔️           ║"
    echo "║                                                          ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    echo ""
    
    # 1. 检查环境
    log_info "检查系统环境..."
    OS=$(detect_os)
    log_info "检测到操作系统: $OS"
    
    # 2. 检查 Go
    log_info "检查 Go 环境..."
    if check_command go; then
        GO_VERSION=$(go version | awk '{print $3}')
        log_success "Go 已安装: $GO_VERSION"
    else
        log_error "Go 未安装，请先安装 Go 1.21+"
        log_info "安装方法: https://go.dev/doc/install"
        exit 1
    fi
    
    # 3. 检查 Docker（可选）
    log_info "检查 Docker..."
    if check_command docker; then
        DOCKER_VERSION=$(docker --version | awk '{print $3}' | tr -d ',')
        log_success "Docker 已安装: $DOCKER_VERSION"
    else
        log_warn "Docker 未安装，沙箱功能将不可用"
    fi
    
    # 4. 检查 Ollama（可选）
    log_info "检查 Ollama..."
    if check_command ollama; then
        log_success "Ollama 已安装"
    else
        log_warn "Ollama 未安装，本地模型功能将不可用"
        log_info "如需本地模型，请运行: ./scripts/deploy-deepseek-local.sh"
    fi
    
    # 5. 下载依赖
    log_info "下载 Go 依赖..."
    cd /home/haoqian.li/ai-corp
    go mod download
    log_success "依赖下载完成"
    
    # 6. 编译项目
    log_info "编译项目..."
    go build -o bin/orchestrator ./cmd/orchestrator/
    log_success "编译完成: bin/orchestrator"
    
    # 7. 创建必要目录
    log_info "创建工作目录..."
    mkdir -p /tmp/ai-corp-workspace
    mkdir -p logs
    
    # 8. 检查配置文件
    log_info "检查配置文件..."
    if [ ! -f "configs/config.yaml" ]; then
        log_warn "配置文件不存在，创建默认配置..."
        mkdir -p configs
        cat > configs/config.yaml << 'EOF'
# AI Corp 配置文件

llm:
  provider: deepseek
  api_key: ""
  model: deepseek-chat
  timeout: 60s

orchestrator:
  port: 8080
  nats_url: ""

ollama:
  base_url: http://localhost:11434
  model: deepseek-coder:6.7b
EOF
        log_success "默认配置已创建: configs/config.yaml"
    fi
    
    # 9. 检查端口
    log_info "检查端口 8080..."
    if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then
        log_warn "端口 8080 已被占用"
        log_info "尝试停止旧进程..."
        pkill -f './bin/orchestrator' 2>/dev/null || true
        sleep 2
    fi
    
    # 10. 启动服务
    log_info "启动 AI Corp 服务..."
    
    # 获取 API Key
    if [ -z "$LLM_API_KEY" ]; then
        log_warn "未设置 LLM_API_KEY 环境变量"
        log_info "云端 API 功能将不可用，但本地模型仍可使用"
    fi
    
    # 后台启动
    nohup ./bin/orchestrator > logs/orchestrator.log 2>&1 &
    PID=$!
    
    sleep 3
    
    # 检查进程是否存活
    if ps -p $PID > /dev/null; then
        log_success "服务启动成功 (PID: $PID)"
    else
        log_error "服务启动失败，请检查日志: logs/orchestrator.log"
        exit 1
    fi
    
    # 11. 验证服务
    log_info "验证服务状态..."
    sleep 2
    
    if curl -s http://localhost:8080/health | grep -q "ok"; then
        log_success "服务健康检查通过"
    else
        log_warn "健康检查失败，请检查日志"
    fi
    
    # 12. 显示访问信息
    echo ""
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║                                                          ║"
    echo "║                  🎉 部署完成！🎉                         ║"
    echo "║                                                          ║"
    echo "╠══════════════════════════════════════════════════════════╣"
    echo "║                                                          ║"
    echo "║  访问地址: http://localhost:8080                         ║"
    echo "║  API 文档: http://localhost:8080/api/v1/                 ║"
    echo "║  健康检查: http://localhost:8080/health                  ║"
    echo "║  监控指标: http://localhost:8080/metrics                 ║"
    echo "║                                                          ║"
    echo "║  日志文件: logs/orchestrator.log                         ║"
    echo "║  进程 PID: $PID                                          ║"
    echo "║                                                          ║"
    echo "╠══════════════════════════════════════════════════════════╣"
    echo "║                                                          ║"
    echo "║  提示:                                                   ║"
    echo "║  - 如需本地模型，请运行:                                 ║"
    echo "║    ./scripts/deploy-deepseek-local.sh                    ║"
    echo "║                                                          ║"
    echo "║  - 设置云端 API Key:                                     ║"
    echo "║    export LLM_API_KEY=your-api-key                       ║"
    echo "║                                                          ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    echo ""
}

# 运行主函数
main "$@"
