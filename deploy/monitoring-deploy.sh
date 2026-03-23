#!/bin/bash
# AI Corp - Monitoring Stack Deploy Script
# Installs Prometheus + Grafana and configures them to monitor the orchestrator
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROMETHEUS_VERSION="2.51.2"
GRAFANA_VERSION="10.4.1"
INSTALL_DIR="/opt/aicorp-monitoring"

echo "=== AI Corp Monitoring Stack Deploy ==="

mkdir -p "$INSTALL_DIR"/{prometheus,grafana}

# --- Prometheus ---
if [ ! -f "$INSTALL_DIR/prometheus/prometheus" ]; then
    echo "[1/4] Downloading Prometheus ${PROMETHEUS_VERSION}..."
    cd /tmp
    PROM_URL="https://github.com/prometheus/prometheus/releases/download/v${PROMETHEUS_VERSION}/prometheus-${PROMETHEUS_VERSION}.linux-amd64.tar.gz"
    curl -L -o prometheus.tar.gz "$PROM_URL"
    tar xzf prometheus.tar.gz
    cp prometheus-${PROMETHEUS_VERSION}.linux-amd64/{prometheus,promtool} "$INSTALL_DIR/prometheus/"
    rm -rf prometheus-${PROMETHEUS_VERSION}.linux-amd64 prometheus.tar.gz
else
    echo "[1/4] Prometheus already installed, skipping."
fi

echo "[2/4] Configuring Prometheus..."
cp "$PROJECT_ROOT/deploy/prometheus/prometheus.yml" "$INSTALL_DIR/prometheus/"
mkdir -p "$INSTALL_DIR/prometheus/data"

# Start Prometheus
echo "[2/4] Starting Prometheus on :9090..."
pkill -f "$INSTALL_DIR/prometheus/prometheus" 2>/dev/null || true
nohup "$INSTALL_DIR/prometheus/prometheus" \
    --config.file="$INSTALL_DIR/prometheus/prometheus.yml" \
    --storage.tsdb.path="$INSTALL_DIR/prometheus/data" \
    --storage.tsdb.retention.time=30d \
    --web.listen-address=":9090" \
    --web.enable-lifecycle \
    > "$INSTALL_DIR/prometheus/prometheus.log" 2>&1 &
echo "  Prometheus PID: $!"

# --- Grafana ---
if [ ! -f "$INSTALL_DIR/grafana/bin/grafana-server" ]; then
    echo "[3/4] Downloading Grafana ${GRAFANA_VERSION}..."
    cd /tmp
    GRAFANA_URL="https://dl.grafana.com/oss/release/grafana-${GRAFANA_VERSION}.linux-amd64.tar.gz"
    curl -L -o grafana.tar.gz "$GRAFANA_URL"
    tar xzf grafana.tar.gz
    cp -r grafana-v${GRAFANA_VERSION}/* "$INSTALL_DIR/grafana/"
    rm -rf grafana-v${GRAFANA_VERSION} grafana.tar.gz
else
    echo "[3/4] Grafana already installed, skipping."
fi

echo "[4/4] Configuring Grafana..."
# Auto-provision Prometheus datasource
mkdir -p "$INSTALL_DIR/grafana/conf/provisioning/datasources"
cat > "$INSTALL_DIR/grafana/conf/provisioning/datasources/prometheus.yml" << 'DATASOURCE'
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://localhost:9090
    isDefault: true
    editable: true
DATASOURCE

# Auto-provision dashboard
mkdir -p "$INSTALL_DIR/grafana/conf/provisioning/dashboards"
cat > "$INSTALL_DIR/grafana/conf/provisioning/dashboards/aicorp.yml" << DASHPROV
apiVersion: 1
providers:
  - name: AI Corp
    orgId: 1
    folder: AI Corp
    type: file
    options:
      path: $PROJECT_ROOT/deploy/grafana
      foldersFromFilesStructure: false
DASHPROV

# Start Grafana
pkill -f "$INSTALL_DIR/grafana/bin/grafana" 2>/dev/null || true
nohup "$INSTALL_DIR/grafana/bin/grafana-server" \
    --homepath="$INSTALL_DIR/grafana" \
    --config="$INSTALL_DIR/grafana/conf/defaults.ini" \
    > "$INSTALL_DIR/grafana/grafana.log" 2>&1 &
echo "  Grafana PID: $!"

echo ""
echo "=== Deploy Complete ==="
echo "  Prometheus: http://localhost:9090"
echo "  Grafana:    http://localhost:3000 (admin/admin)"
echo "  Orchestrator metrics: http://localhost:8080/metrics"
echo ""
echo "  Logs:"
echo "    Prometheus: $INSTALL_DIR/prometheus/prometheus.log"
echo "    Grafana:    $INSTALL_DIR/grafana/grafana.log"
