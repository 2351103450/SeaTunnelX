#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNTIME_DIR="$BASE_DIR/runtime"

stop_one() {
  local name="$1"
  local pidfile="$2"
  if [[ ! -f "$pidfile" ]]; then
    echo "$name not running (no pidfile)"
    return
  fi
  local pid
  pid="$(cat "$pidfile" 2>/dev/null || true)"
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" || true
    sleep 1
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" || true
    fi
    echo "stopped $name (pid $pid)"
  else
    echo "$name already stopped"
  fi
  rm -f "$pidfile"
}

stop_one "grafana" "$RUNTIME_DIR/grafana/grafana.pid"
stop_one "prometheus" "$RUNTIME_DIR/prometheus/prometheus.pid"
stop_one "alertmanager" "$RUNTIME_DIR/alertmanager/alertmanager.pid"
