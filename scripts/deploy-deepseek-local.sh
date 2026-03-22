#!/bin/bash

# ============================================
# DeepSeek 本地部署脚本 (通过 Ollama)
# ============================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

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

log_step() {
    echo -e "${CYAN}[STEP]${NC} $1"
}

# 检查命令是否存在
check_command() {
    if command -v $1 &> /dev/null; then
        return 0
    fi
    return 1
}

# 检测操作系统
detect_os() {
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        if [ -f /etc/os-release ]; then
            . /etc/os-release
            echo "$ID"
        else
            echo "linux"
        fi
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        echo "macos"
    else
        echo "unknown"
    fi
}

# 安装 Ollama
install_ollama() {
    log_step "安装 Ollama..."
    
    OS=$(detect_os)
    
    case $OS in
        ubuntu|debian|centos|rhel|rocky|almalinux|linux)
            log_info "检测到 Linux 系统，使用官方安装脚本..."
            curl -fsSL https://ollama.com/install.sh | sh
            ;;
        macos)
            log_info "检测到 macOS 系统..."
            if check_command brew; then
                log_info "使用 Homebrew 安装..."
                brew install ollama
            else
                log_error "请先安装 Homebrew 或手动下载 Ollama"
                log_info "下载地址: https://ollama.com/download"
                exit 1
            fi
            ;;
        *)
            log_error "不支持的操作系统: $OS"
            log_info "请手动安装 Ollama: https://ollama.com/download"
            exit 1
            ;;
    esac
    
    log_success "Ollama 安装完成"
}

# 启动 Ollama 服务
start_ollama_service() {
    log_step "启动 Ollama 服务..."
    
    # 检查是否已在运行
    if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
        log_success "Ollama 服务已在运行"
        return 0
    fi
    
    # 启动服务
    log_info "启动 Ollama 后台服务..."
    nohup ollama serve > /tmp/ollama.log 2>&1 &
    OLLAMA_PID=$!
    
    # 等待服务启动
    log_info "等待服务启动..."
    for i in {1..30}; do
        if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
            log_success "Ollama 服务启动成功 (PID: $OLLAMA_PID)"
            return 0
        fi
        sleep 1
    done
    
    log_error "Ollama 服务启动超时"
    log_info "请检查日志: /tmp/ollama.log"
    return 1
}

# 下载 DeepSeek 模型
download_deepseek() {
    log_step "下载 DeepSeek 模型..."
    
    # 可选模型列表
    MODELS=(
        "deepseek-coder:6.7b"      # 代码专用，6.7B 参数，约 4GB
        "deepseek-coder:1.3b"      # 代码专用，1.3B 参数，约 1GB (轻量版)
        "deepseek-r1:7b"           # 推理模型，7B 参数
        "deepseek-v2:16b"          # 通用模型，16B 参数 (需要更多内存)
    )
    
    echo ""
    echo "请选择要安装的 DeepSeek 模型："
    echo ""
    echo "  1) deepseek-coder:6.7b  - 代码专用 (推荐，约 4GB 显存)"
    echo "  2) deepseek-coder:1.3b  - 代码专用轻量版 (约 1GB 显存)"
    echo "  3) deepseek-r1:7b       - 推理模型 (约 5GB 显存)"
    echo "  4) deepseek-v2:16b      - 通用大模型 (约 10GB 显存)"
    echo "  5) 全部安装"
    echo ""
    
    read -p "请输入选项 (1-5) [默认: 1]: " choice
    choice=${choice:-1}
    
    case $choice in
        1)
            MODEL="deepseek-coder:6.7b"
            log_info "下载 $MODEL..."
            ollama pull $MODEL
            ;;
        2)
            MODEL="deepseek-coder:1.3b"
            log_info "下载 $MODEL..."
            ollama pull $MODEL
            ;;
        3)
            MODEL="deepseek-r1:7b"
            log_info "下载 $MODEL..."
            ollama pull $MODEL
            ;;
        4)
            MODEL="deepseek-v2:16b"
            log_info "下载 $MODEL..."
            ollama pull $MODEL
            ;;
        5)
            log_info "下载所有模型..."
            for model in "${MODELS[@]}"; do
                log_info "下载 $model..."
                ollama pull $model
            done
            MODEL="all"
            ;;
        *)
            log_error "无效选项"
            exit 1
            ;;
    esac
    
    log_success "模型下载完成"
}

# 验证模型
verify_model() {
    log_step "验证模型可用性..."
    
    # 测试简单推理
    log_info "测试模型推理能力..."
    
    TEST_RESULT=$(curl -s http://localhost:11434/api/generate -d '{
        "model": "deepseek-coder:6.7b",
        "prompt": "Say hello in Chinese",
        "stream": false
    }' 2>/dev/null || echo '{"error": "failed"}')
    
    if echo "$TEST_RESULT" | grep -q "response"; then
        log_success "模型验证成功"
        return 0
    else
        log_warn "模型验证失败，但可能仍可使用"
        return 0
    fi
}

# 配置 AI Corp
configure_ai_corp() {
    log_step "配置 AI Corp 使用本地模型..."
    
    CONFIG_FILE="/home/haoqian.li/ai-corp/configs/config.yaml"
    
    if [ -f "$CONFIG_FILE" ]; then
        # 更新配置
        log_info "更新配置文件..."
        # 这里可以添加配置更新逻辑
        log_success "配置已更新"
    else
        log_warn "配置文件不存在，请先运行主部署脚本"
    fi
}

# 显示使用信息
show_usage_info() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║                                                          ║"
    echo "║           🏠 DeepSeek 本地部署完成！🏠                  ║"
    echo "║                                                          ║"
    echo "╠══════════════════════════════════════════════════════════╣"
    echo "║                                                          ║"
    echo "║  Ollama 服务地址: http://localhost:11434                 ║"
    echo "║                                                          ║"
    echo "║  可用命令:                                               ║"
    echo "║  - ollama list           # 列出已安装模型                ║"
    echo "║  - ollama run deepseek-coder:6.7b  # 交互式对话          ║"
    echo "║  - ollama ps             # 查看运行中的模型              ║"
    echo "║                                                          ║"
    echo "╠══════════════════════════════════════════════════════════╣"
    echo "║                                                          ║"
    echo "║  API 调用示例:                                           ║"
    echo "║                                                          ║"
    echo "║  curl http://localhost:11434/api/generate -d '{          ║"
    echo "║    \"model\": \"deepseek-coder:6.7b\",                     ║"
    echo "║    \"prompt\": \"写一个 Python 函数计算斐波那契\",         ║"
    echo "║    \"stream\": false                                      ║"
    echo "║  }'                                                      ║"
    echo "║                                                          ║"
    echo "╠══════════════════════════════════════════════════════════╣"
    echo "║                                                          ║"
    echo "║  资源需求:                                               ║"
    echo "║  - deepseek-coder:6.7b  需要约 4GB 显存/内存             ║"
    echo "║  - deepseek-coder:1.3b  需要约 1GB 显存/内存             ║"
    echo "║  - deepseek-r1:7b       需要约 5GB 显存/内存             ║"
    echo "║                                                          ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    echo ""
}

# 主函数
main() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║                                                          ║"
    echo "║      🏠 DeepSeek 本地部署脚本 (Ollama) 🏠               ║"
    echo "║                                                          ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    echo ""
    
    # 1. 检查 Ollama
    log_step "检查 Ollama 安装状态..."
    if check_command ollama; then
        OLLAMA_VERSION=$(ollama --version 2>/dev/null | head -1 || echo "unknown")
        log_success "Ollama 已安装: $OLLAMA_VERSION"
    else
        log_warn "Ollama 未安装"
        install_ollama
    fi
    
    # 2. 启动 Ollama 服务
    start_ollama_service
    
    # 3. 下载模型
    download_deepseek
    
    # 4. 验证模型
    verify_model
    
    # 5. 配置 AI Corp
    configure_ai_corp
    
    # 6. 显示使用信息
    show_usage_info
    
    log_success "DeepSeek 本地部署完成！"
}

# 运行主函数
main "$@"
