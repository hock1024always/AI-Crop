#!/bin/bash
# ============================================================
# AI Corp - PostgreSQL 16 + pgvector 0.8.0 一键编译部署脚本
# 适用于 CentOS 7 / RHEL 7 及类似系统
# ============================================================

set -euo pipefail

# === 配置参数 ===
PG_VERSION="16.6"
PGVECTOR_VERSION="0.8.0"
PG_INSTALL_DIR="/usr/local/pgsql16"
PG_DATA_DIR="/var/lib/pgsql/data"
PG_LOG_DIR="/var/lib/pgsql/log"
PG_USER="postgres"
BUILD_DIR="/tmp/pg-build-$$"
DB_NAME="aicorp"

# === 颜色输出 ===
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $(date '+%H:%M:%S') $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $(date '+%H:%M:%S') $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $1"; }

# === 前置检查 ===
check_prerequisites() {
    log_info "检查前置条件..."

    if [ "$(id -u)" -ne 0 ]; then
        log_error "请使用 root 权限运行此脚本"
        exit 1
    fi

    # 检查是否已安装
    if [ -x "${PG_INSTALL_DIR}/bin/pg_config" ]; then
        local installed_ver
        installed_ver=$("${PG_INSTALL_DIR}/bin/pg_config" --version 2>/dev/null || true)
        log_warn "检测到已安装的 PostgreSQL: ${installed_ver}"
        read -p "是否继续覆盖安装? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "已取消安装"
            exit 0
        fi
    fi

    # 检查磁盘空间（至少需要 1GB）
    local free_space_mb
    free_space_mb=$(df -BM /tmp | awk 'NR==2{print $4}' | tr -d 'M')
    if [ "${free_space_mb}" -lt 1024 ]; then
        log_error "/tmp 磁盘空间不足 (需要 >= 1GB, 当前 ${free_space_mb}MB)"
        exit 1
    fi

    log_info "前置检查通过"
}

# === 安装编译依赖 ===
install_build_deps() {
    log_info "安装编译依赖..."

    if command -v yum &>/dev/null; then
        yum install -y gcc make readline-devel zlib-devel openssl-devel \
            libxml2-devel curl wget tar bzip2
    elif command -v apt-get &>/dev/null; then
        apt-get update
        apt-get install -y build-essential libreadline-dev zlib1g-dev \
            libssl-dev libxml2-dev curl wget tar bzip2
    else
        log_error "不支持的包管理器，请手动安装: gcc make readline-devel zlib-devel openssl-devel"
        exit 1
    fi

    log_info "编译依赖安装完成"
}

# === 编译安装 PostgreSQL ===
build_postgresql() {
    log_info "开始编译 PostgreSQL ${PG_VERSION}..."

    mkdir -p "${BUILD_DIR}" && cd "${BUILD_DIR}"

    # 下载源码
    local pg_tarball="postgresql-${PG_VERSION}.tar.bz2"
    if [ ! -f "${pg_tarball}" ]; then
        log_info "下载 PostgreSQL ${PG_VERSION} 源码..."
        curl -L -o "${pg_tarball}" \
            "https://ftp.postgresql.org/pub/source/v${PG_VERSION}/${pg_tarball}"
    fi

    tar xjf "${pg_tarball}"
    cd "postgresql-${PG_VERSION}"

    # 配置
    log_info "配置 PostgreSQL..."
    ./configure \
        --prefix="${PG_INSTALL_DIR}" \
        --with-openssl \
        --with-libxml \
        --without-icu

    # 编译
    log_info "编译 PostgreSQL (使用 $(nproc) 核)..."
    make -j"$(nproc)"

    # 安装
    log_info "安装 PostgreSQL 到 ${PG_INSTALL_DIR}..."
    make install

    # 编译 contrib 模块 (pg_trgm, pg_stat_statements 等)
    log_info "编译 contrib 模块..."
    cd contrib
    make -j"$(nproc)"
    make install
    cd ..

    log_info "PostgreSQL ${PG_VERSION} 编译安装完成"
}

# === 编译安装 pgvector ===
build_pgvector() {
    log_info "开始编译 pgvector ${PGVECTOR_VERSION}..."

    cd "${BUILD_DIR}"

    # 下载 pgvector 源码
    local pgv_tarball="pgvector-${PGVECTOR_VERSION}.tar.gz"
    if [ ! -f "${pgv_tarball}" ]; then
        log_info "下载 pgvector ${PGVECTOR_VERSION} 源码..."
        curl -L -o "${pgv_tarball}" \
            "https://github.com/pgvector/pgvector/archive/refs/tags/v${PGVECTOR_VERSION}.tar.gz"
    fi

    tar xzf "${pgv_tarball}"
    cd "pgvector-${PGVECTOR_VERSION}"

    # 编译安装（指向自编译的 PG）
    log_info "编译 pgvector..."
    PG_CONFIG="${PG_INSTALL_DIR}/bin/pg_config" make -j"$(nproc)"
    PG_CONFIG="${PG_INSTALL_DIR}/bin/pg_config" make install

    log_info "pgvector ${PGVECTOR_VERSION} 编译安装完成"
}

# === 初始化数据库集群 ===
init_database() {
    log_info "初始化 PostgreSQL 数据库集群..."

    # 创建 postgres 用户（如不存在）
    if ! id "${PG_USER}" &>/dev/null; then
        useradd -r -s /bin/bash -d /var/lib/pgsql "${PG_USER}"
        log_info "创建用户 ${PG_USER}"
    fi

    # 创建数据目录和日志目录
    mkdir -p "${PG_DATA_DIR}" "${PG_LOG_DIR}"
    chown -R "${PG_USER}:${PG_USER}" /var/lib/pgsql

    # 如果数据目录已初始化，跳过
    if [ -f "${PG_DATA_DIR}/PG_VERSION" ]; then
        log_warn "数据目录已初始化，跳过 initdb"
    else
        su - "${PG_USER}" -c "${PG_INSTALL_DIR}/bin/initdb -D ${PG_DATA_DIR} -E UTF8 --locale=C"
        log_info "数据库集群初始化完成"
    fi

    # 配置 pg_hba.conf：允许本地密码连接
    if ! grep -q "local.*all.*all.*trust" "${PG_DATA_DIR}/pg_hba.conf" 2>/dev/null; then
        cat >> "${PG_DATA_DIR}/pg_hba.conf" <<'EOF'

# AI Corp: 本地信任连接
local   all   all                 trust
host    all   all   127.0.0.1/32  trust
host    all   all   ::1/128       trust
EOF
        log_info "pg_hba.conf 已配置"
    fi

    # 配置 postgresql.conf: 基础调优
    local pgconf="${PG_DATA_DIR}/postgresql.conf"
    if ! grep -q "# AI Corp tuning" "${pgconf}" 2>/dev/null; then
        cat >> "${pgconf}" <<EOF

# AI Corp tuning
listen_addresses = 'localhost'
port = 5432
shared_buffers = 256MB
effective_cache_size = 768MB
work_mem = 16MB
maintenance_work_mem = 128MB
max_connections = 100
wal_level = minimal
max_wal_senders = 0
logging_collector = on
log_directory = '${PG_LOG_DIR}'
log_filename = 'postgresql-%Y-%m-%d.log'
log_statement = 'ddl'
EOF
        log_info "postgresql.conf 已调优"
    fi
}

# === 启动 PostgreSQL ===
start_postgresql() {
    log_info "启动 PostgreSQL..."

    # 检查是否已经在运行
    if su - "${PG_USER}" -c "${PG_INSTALL_DIR}/bin/pg_ctl -D ${PG_DATA_DIR} status" &>/dev/null; then
        log_warn "PostgreSQL 已在运行中"
        return 0
    fi

    su - "${PG_USER}" -c "${PG_INSTALL_DIR}/bin/pg_ctl -D ${PG_DATA_DIR} -l ${PG_LOG_DIR}/startup.log start"

    # 等待启动
    local retries=10
    while [ $retries -gt 0 ]; do
        if su - "${PG_USER}" -c "${PG_INSTALL_DIR}/bin/pg_isready -q" 2>/dev/null; then
            log_info "PostgreSQL 启动成功"
            return 0
        fi
        sleep 1
        retries=$((retries - 1))
    done

    log_error "PostgreSQL 启动超时"
    exit 1
}

# === 创建数据库并启用扩展 ===
setup_database() {
    log_info "创建数据库 ${DB_NAME} 并启用扩展..."

    local PSQL="${PG_INSTALL_DIR}/bin/psql"

    # 创建数据库（如不存在）
    su - "${PG_USER}" -c "${PG_INSTALL_DIR}/bin/createdb ${DB_NAME} 2>/dev/null || true"

    # 启用 pgvector 和 pg_trgm 扩展
    su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME}" <<'SQL'
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
SQL

    # 验证扩展
    local ext_check
    ext_check=$(su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -t -c \"SELECT extname, extversion FROM pg_extension WHERE extname IN ('vector', 'pg_trgm') ORDER BY extname;\"")
    log_info "已启用的扩展:"
    echo "${ext_check}"

    log_info "数据库 ${DB_NAME} 设置完成"
}

# === 执行 Schema ===
apply_schema() {
    log_info "应用数据库 Schema..."

    local PSQL="${PG_INSTALL_DIR}/bin/psql"
    local SCRIPT_DIR
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

    # 应用主 schema
    if [ -f "${SCRIPT_DIR}/schema.sql" ]; then
        su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -f ${SCRIPT_DIR}/schema.sql"
        log_info "主 schema (schema.sql) 已应用"
    else
        log_warn "未找到 ${SCRIPT_DIR}/schema.sql，跳过"
    fi

    # 应用 memory schema
    if [ -f "${SCRIPT_DIR}/memory_schema.sql" ]; then
        su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -f ${SCRIPT_DIR}/memory_schema.sql"
        log_info "记忆系统 schema (memory_schema.sql) 已应用"
    else
        log_warn "未找到 ${SCRIPT_DIR}/memory_schema.sql，跳过"
    fi

    log_info "Schema 应用完成"
}

# === 验证安装 ===
verify_installation() {
    log_info "验证安装..."

    local PSQL="${PG_INSTALL_DIR}/bin/psql"

    echo ""
    echo "============================================"
    echo " PostgreSQL + pgvector 安装验证报告"
    echo "============================================"

    # PG 版本
    echo -n "PostgreSQL 版本: "
    su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -t -c 'SELECT version();'" | head -1

    # pgvector 版本
    echo -n "pgvector 版本:   "
    su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -t -c \"SELECT extversion FROM pg_extension WHERE extname = 'vector';\"" | head -1

    # 向量功能测试
    log_info "测试向量操作..."
    su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME}" <<'SQL'
-- 测试向量创建和距离计算
SELECT
    '[1,2,3]'::vector(3) <=> '[4,5,6]'::vector(3) AS cosine_distance,
    '[1,2,3]'::vector(3) <-> '[4,5,6]'::vector(3) AS l2_distance;
SQL

    # 表统计
    echo ""
    echo "数据库表:"
    su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -c \"SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename;\""

    # 索引统计
    echo ""
    echo "IVFFlat 向量索引:"
    su - "${PG_USER}" -c "${PSQL} -d ${DB_NAME} -c \"SELECT indexname, indexdef FROM pg_indexes WHERE indexdef LIKE '%ivfflat%';\""

    echo ""
    echo "============================================"
    log_info "验证完成"
}

# === 配置 PATH ===
setup_path() {
    local profile_file="/etc/profile.d/pgsql16.sh"
    if [ ! -f "${profile_file}" ]; then
        cat > "${profile_file}" <<EOF
export PATH=${PG_INSTALL_DIR}/bin:\$PATH
export LD_LIBRARY_PATH=${PG_INSTALL_DIR}/lib:\$LD_LIBRARY_PATH
export PGDATA=${PG_DATA_DIR}
EOF
        chmod 644 "${profile_file}"
        log_info "PATH 已配置到 ${profile_file}"
    fi
}

# === 清理构建目录 ===
cleanup() {
    if [ -d "${BUILD_DIR}" ]; then
        log_info "清理构建临时目录..."
        rm -rf "${BUILD_DIR}"
    fi
}

# === 生成 systemd 服务文件（如果支持） ===
create_systemd_service() {
    if ! command -v systemctl &>/dev/null; then
        log_warn "未检测到 systemd，跳过服务文件创建"
        return 0
    fi

    local service_file="/etc/systemd/system/postgresql-16.service"
    if [ -f "${service_file}" ]; then
        log_warn "systemd 服务文件已存在，跳过"
        return 0
    fi

    cat > "${service_file}" <<EOF
[Unit]
Description=PostgreSQL 16 database server
After=network.target

[Service]
Type=forking
User=${PG_USER}
Group=${PG_USER}
Environment=PGDATA=${PG_DATA_DIR}
ExecStart=${PG_INSTALL_DIR}/bin/pg_ctl start -D ${PG_DATA_DIR} -l ${PG_LOG_DIR}/startup.log -w
ExecStop=${PG_INSTALL_DIR}/bin/pg_ctl stop -D ${PG_DATA_DIR} -m fast
ExecReload=${PG_INSTALL_DIR}/bin/pg_ctl reload -D ${PG_DATA_DIR}
TimeoutSec=120

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    log_info "systemd 服务 postgresql-16 已创建"
}

# === 主流程 ===
main() {
    local start_time
    start_time=$(date +%s)

    echo "============================================"
    echo " AI Corp - PostgreSQL + pgvector 部署"
    echo " PG ${PG_VERSION} + pgvector ${PGVECTOR_VERSION}"
    echo "============================================"
    echo ""

    check_prerequisites
    install_build_deps
    build_postgresql
    build_pgvector
    init_database
    start_postgresql
    setup_database
    apply_schema
    setup_path
    create_systemd_service
    verify_installation
    cleanup

    local elapsed=$(( $(date +%s) - start_time ))
    echo ""
    log_info "全部完成! 用时 ${elapsed} 秒"
    echo ""
    echo "快速命令:"
    echo "  连接数据库:  ${PG_INSTALL_DIR}/bin/psql -d ${DB_NAME}"
    echo "  查看状态:    su - ${PG_USER} -c '${PG_INSTALL_DIR}/bin/pg_ctl -D ${PG_DATA_DIR} status'"
    echo "  重启服务:    su - ${PG_USER} -c '${PG_INSTALL_DIR}/bin/pg_ctl -D ${PG_DATA_DIR} restart'"
    echo ""
}

# 支持单步执行
case "${1:-all}" in
    all)          main ;;
    deps)         install_build_deps ;;
    build-pg)     build_postgresql ;;
    build-pgv)    build_pgvector ;;
    init)         init_database ;;
    start)        start_postgresql ;;
    setup)        setup_database ;;
    schema)       apply_schema ;;
    verify)       verify_installation ;;
    *)
        echo "用法: $0 [all|deps|build-pg|build-pgv|init|start|setup|schema|verify]"
        exit 1
        ;;
esac
