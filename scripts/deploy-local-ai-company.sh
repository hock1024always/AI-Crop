#!/bin/bash

# ============================================
# 本地 AI 外包公司 - 一键部署脚本
# 完全免费，无需 API Key
# ============================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
PURPLE='\033[0;35m'
NC='\033[0m'

# 打印函数
log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()    { echo -e "${CYAN}[STEP]${NC} $1"; }
log_model()   { echo -e "${PURPLE}[MODEL]${NC} $1"; }

# 检查命令
check_command() {
    if command -v $1 &> /dev/null; then
        return 0
    fi
    return 1
}

# 显示 Banner
show_banner() {
    clear
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║                                                                  ║"
    echo "║     🏢 本地 AI 外包公司 - 一键部署脚本 🏢                       ║"
    echo "║                                                                  ║"
    echo "║     完全免费 · 无需 API Key · 数据本地化                        ║"
    echo "║                                                                  ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""
}

# 检测系统
detect_system() {
    log_step "检测系统环境..."
    
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        OS="linux"
        if [ -f /etc/os-release ]; then
            . /etc/os-release
            DISTRO=$ID
        fi
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        OS="macos"
        DISTRO="macos"
    else
        OS="unknown"
    fi
    
    log_info "操作系统: $OS ($DISTRO)"
    
    # 检测内存
    if [ -f /proc/meminfo ]; then
        TOTAL_MEM=$(grep MemTotal /proc/meminfo | awk '{print $2}')
        TOTAL_MEM_GB=$((TOTAL_MEM / 1024 / 1024))
        log_info "系统内存: ${TOTAL_MEM_GB}GB"
    fi
    
    # 检测 GPU
    if check_command nvidia-smi; then
        GPU_INFO=$(nvidia-smi --query-gpu=name,memory.total --format=csv,noheader | head -1)
        log_success "检测到 GPU: $GPU_INFO"
        HAS_GPU=true
    else
        log_warn "未检测到 NVIDIA GPU，将使用 CPU 模式"
        HAS_GPU=false
    fi
}

# 安装 Ollama
install_ollama() {
    log_step "安装 Ollama..."
    
    if check_command ollama; then
        OLLAMA_VERSION=$(ollama --version 2>/dev/null | head -1 || echo "已安装")
        log_success "Ollama 已安装: $OLLAMA_VERSION"
        return 0
    fi
    
    log_info "开始安装 Ollama..."
    
    case $OS in
        linux)
            curl -fsSL https://ollama.com/install.sh | sh
            ;;
        macos)
            if check_command brew; then
                brew install ollama
            else
                log_error "请先安装 Homebrew: https://brew.sh"
                exit 1
            fi
            ;;
        *)
            log_error "不支持的操作系统，请手动安装 Ollama"
            log_info "下载地址: https://ollama.com/download"
            exit 1
            ;;
    esac
    
    log_success "Ollama 安装完成"
}

# 启动 Ollama 服务
start_ollama() {
    log_step "启动 Ollama 服务..."
    
    # 检查是否已在运行
    if curl -s http://localhost:11434/api/tags > /dev/null 2>&1; then
        log_success "Ollama 服务已在运行"
        return 0
    fi
    
    log_info "启动 Ollama 后台服务..."
    nohup ollama serve > /tmp/ollama.log 2>&1 &
    OLLAMA_PID=$!
    
    # 等待服务启动
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

# 选择要安装的模型
select_models() {
    log_step "选择要安装的模型..."
    
    echo ""
    echo "═══════════════════════════════════════════════════════════════════"
    echo "                      可用的开源模型                                "
    echo "═══════════════════════════════════════════════════════════════════"
    echo ""
    echo "【代码开发组推荐】"
    echo "  1) deepseek-coder:6.7b   - DeepSeek 代码模型 (推荐, ~4GB)"
    echo "  2) deepseek-coder:1.3b   - DeepSeek 轻量版 (~1GB)"
    echo "  3) qwen2.5-coder:7b      - Qwen 代码模型 (推荐, ~5GB)"
    echo "  4) codellama:7b          - Meta CodeLlama (~5GB)"
    echo ""
    echo "【对话/推理模型】"
    echo "  5) deepseek-r1:7b        - DeepSeek 推理增强版 (推荐, ~5GB)"
    echo "  6) qwen2.5:7b            - Qwen 通用模型 (推荐, ~5GB)"
    echo "  7) llama3.1:8b           - Meta Llama 3.1 (推荐, ~5GB)"
    echo "  8) mistral:7b            - Mistral 7B (推荐, ~5GB)"
    echo ""
    echo "【向量嵌入模型】"
    echo "  9) nomic-embed-text      - 向量嵌入 (推荐, ~300MB)"
    echo ""
    echo "【预设方案】"
    echo "  10) 轻量方案 (适合 8GB 内存)"
    echo "  11) 标准方案 (适合 16GB 内存) [推荐]"
    echo "  12) 完整方案 (适合 32GB+ 内存)"
    echo "  13) 全部安装"
    echo ""
    echo "  0) 跳过模型安装"
    echo ""
    echo "═══════════════════════════════════════════════════════════════════"
    echo ""
    
    read -p "请选择 (多选用空格分隔, 如 '1 5 9'): " choices
    
    SELECTED_MODELS=()
    
    for choice in $choices; do
        case $choice in
            1) SELECTED_MODELS+=("deepseek-coder:6.7b") ;;
            2) SELECTED_MODELS+=("deepseek-coder:1.3b") ;;
            3) SELECTED_MODELS+=("qwen2.5-coder:7b") ;;
            4) SELECTED_MODELS+=("codellama:7b") ;;
            5) SELECTED_MODELS+=("deepseek-r1:7b") ;;
            6) SELECTED_MODELS+=("qwen2.5:7b") ;;
            7) SELECTED_MODELS+=("llama3.1:8b") ;;
            8) SELECTED_MODELS+=("mistral:7b") ;;
            9) SELECTED_MODELS+=("nomic-embed-text") ;;
            10)
                # 轻量方案
                SELECTED_MODELS+=(
                    "deepseek-coder:1.3b"
                    "qwen2.5:7b"
                    "nomic-embed-text"
                )
                ;;
            11)
                # 标准方案
                SELECTED_MODELS+=(
                    "deepseek-coder:6.7b"
                    "deepseek-r1:7b"
                    "qwen2.5:7b"
                    "nomic-embed-text"
                )
                ;;
            12)
                # 完整方案
                SELECTED_MODELS+=(
                    "deepseek-coder:6.7b"
                    "deepseek-r1:7b"
                    "qwen2.5-coder:7b"
                    "qwen2.5:7b"
                    "llama3.1:8b"
                    "mistral:7b"
                    "nomic-embed-text"
                )
                ;;
            13)
                # 全部安装
                SELECTED_MODELS=(
                    "deepseek-coder:6.7b"
                    "deepseek-coder:1.3b"
                    "qwen2.5-coder:7b"
                    "codellama:7b"
                    "deepseek-r1:7b"
                    "qwen2.5:7b"
                    "llama3.1:8b"
                    "mistral:7b"
                    "nomic-embed-text"
                )
                ;;
            0)
                log_info "跳过模型安装"
                return 0
                ;;
            *)
                log_warn "无效选项: $choice"
                ;;
        esac
    done
    
    if [ ${#SELECTED_MODELS[@]} -eq 0 ]; then
        log_warn "未选择任何模型"
        return 0
    fi
    
    log_info "将安装以下模型: ${SELECTED_MODELS[*]}"
}

# 安装模型
install_models() {
    if [ ${#SELECTED_MODELS[@]} -eq 0 ]; then
        return 0
    fi
    
    log_step "开始安装模型..."
    
    for model in "${SELECTED_MODELS[@]}"; do
        log_model "正在下载: $model"
        ollama pull $model
        log_success "安装完成: $model"
    done
    
    log_success "所有模型安装完成"
}

# 验证模型
verify_models() {
    log_step "验证模型安装..."
    
    INSTALLED=$(ollama list 2>/dev/null | tail -n +2 | wc -l)
    
    if [ "$INSTALLED" -gt 0 ]; then
        log_success "已安装 $INSTALLED 个模型:"
        ollama list
    else
        log_warn "未安装任何模型"
    fi
}

# 编译 AI Corp
build_ai_corp() {
    log_step "编译 AI Corp..."
    
    cd /home/haoqian.li/ai-corp
    
    # 检查 Go
    if ! check_command go; then
        log_error "Go 未安装，请先安装 Go 1.21+"
        exit 1
    fi
    
    log_info "下载依赖..."
    go mod download
    
    log_info "编译项目..."
    go build -o bin/orchestrator ./cmd/orchestrator/
    
    log_success "编译完成"
}

# 创建配置文件
create_config() {
    log_step "创建配置文件..."
    
    mkdir -p /home/haoqian.li/ai-corp/configs
    
    cat > /home/haoqian.li/ai-corp/configs/config.yaml << 'EOF'
# ============================================
# 本地 AI 外包公司配置文件
# ============================================

# 集群配置
cluster:
  name: "ai-outsourcing-company"
  mode: "local"  # local / distributed
  
# Ollama 配置
ollama:
  base_url: "http://localhost:11434"
  default_model: "deepseek-coder:6.7b"
  
# 模型路由配置
model_router:
  # 代码任务使用的模型
  code_tasks: "deepseek-coder:6.7b"
  # 对话任务使用的模型
  chat_tasks: "qwen2.5:7b"
  # 推理任务使用的模型
  reasoning_tasks: "deepseek-r1:7b"
  # 向量嵌入模型
  embedding_model: "nomic-embed-text"
  
# 工作区配置（外包公司部门）
workspaces:
  - id: "frontend"
    name: "前端开发组"
    default_model: "deepseek-coder:6.7b"
    max_agents: 5
    
  - id: "backend"
    name: "后端开发组"
    default_model: "deepseek-coder:6.7b"
    max_agents: 8
    
  - id: "testing"
    name: "测试组"
    default_model: "deepseek-coder:6.7b"
    max_agents: 4
    
  - id: "devops"
    name: "运维组"
    default_model: "qwen2.5:7b"
    max_agents: 3
    
  - id: "ai"
    name: "AI算法组"
    default_model: "deepseek-r1:7b"
    max_agents: 4
    
  - id: "pm"
    name: "项目管理组"
    default_model: "qwen2.5:7b"
    max_agents: 2

# 服务配置
server:
  port: 8080
  host: "0.0.0.0"
  
# 日志配置
logging:
  level: "info"
  file: "logs/ai-corp.log"
EOF
    
    log_success "配置文件已创建: configs/config.yaml"
}

# 启动服务
start_service() {
    log_step "启动 AI Corp 服务..."
    
    cd /home/haoqian.li/ai-corp
    
    # 检查端口
    if lsof -Pi :8080 -sTCP:LISTEN -t >/dev/null 2>&1; then
        log_warn "端口 8080 已被占用"
        pkill -f './bin/orchestrator' 2>/dev/null || true
        sleep 2
    fi
    
    # 创建日志目录
    mkdir -p logs
    
    # 启动服务
    nohup ./bin/orchestrator > logs/orchestrator.log 2>&1 &
    PID=$!
    
    sleep 3
    
    if ps -p $PID > /dev/null; then
        log_success "服务启动成功 (PID: $PID)"
    else
        log_error "服务启动失败，请检查日志: logs/orchestrator.log"
        exit 1
    fi
}

# 显示完成信息
show_completion() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║                                                                  ║"
    echo "║              🎉 本地 AI 外包公司部署完成！🎉                    ║"
    echo "║                                                                  ║"
    echo "╠══════════════════════════════════════════════════════════════════╣"
    echo "║                                                                  ║"
    echo "║  访问地址:                                                       ║"
    echo "║  • 像素酒馆界面: http://localhost:8080                           ║"
    echo "║  • API 接口:     http://localhost:8080/api/v1/                   ║"
    echo "║  • 健康检查:     http://localhost:8080/health                    ║"
    echo "║                                                                  ║"
    echo "║  已安装模型:                                                     ║"
    ollama list | tail -n +2 | while read line; do
        echo "║  • $line"
    done
    echo "║                                                                  ║"
    echo "╠══════════════════════════════════════════════════════════════════╣"
    echo "║                                                                  ║"
    echo "║  工作区划分:                                                     ║"
    echo "║  • 前端开发组 - Web/移动端开发                                   ║"
    echo "║  • 后端开发组 - 服务端/API开发                                   ║"
    echo "║  • 测试组 - 自动化测试/质量保证                                  ║"
    echo "║  • 运维组 - 部署/监控/基础设施                                   ║"
    echo "║  • AI算法组 - 机器学习/数据分析                                  ║"
    echo "║  • 项目管理组 - 项目规划/进度管理                                ║"
    echo "║                                                                  ║"
    echo "╠══════════════════════════════════════════════════════════════════╣"
    echo "║                                                                  ║"
    echo "║  常用命令:                                                       ║"
    echo "║  • ollama list              # 列出已安装模型                     ║"
    echo "║  • ollama run <model>       # 交互式对话                         ║"
    echo "║  • ollama ps                # 查看运行中的模型                   ║"
    echo "║                                                                  ║"
    echo "║  日志文件: logs/orchestrator.log                                 ║"
    echo "║                                                                  ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""
}

# 主函数
main() {
    show_banner
    
    # 1. 检测系统
    detect_system
    
    # 2. 安装 Ollama
    install_ollama
    
    # 3. 启动 Ollama 服务
    start_ollama
    
    # 4. 选择并安装模型
    select_models
    install_models
    
    # 5. 验证模型
    verify_models
    
    # 6. 编译项目
    build_ai_corp
    
    # 7. 创建配置
    create_config
    
    # 8. 启动服务
    start_service
    
    # 9. 显示完成信息
    show_completion
}

# 运行主函数
main "$@"
