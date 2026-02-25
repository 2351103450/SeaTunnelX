#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUN_DIR="$BASE_DIR/run"
FRONTEND_PORT="${FRONTEND_PORT:-80}"

status_one() {
  local name="$1"
  local pidfile="$2"
  if [[ ! -f "$pidfile" ]]; then
    echo "$name: stopped"
    return
  fi
  local pid
  pid="$(cat "$pidfile" 2>/dev/null || true)"
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    echo "$name: running (pid=$pid)"
  else
    echo "$name: pidfile exists but process not running"
  fi
}

status_one "backend" "$RUN_DIR/backend.pid"
status_one "frontend" "$RUN_DIR/frontend.pid"

echo
echo "ports:"
ss -lntp | grep -E ":8000|:${FRONTEND_PORT}\\b|:9090|:9093|:3000" || true

if [[ -x "$BASE_DIR/deps/status-observability.sh" ]]; then
  echo
  echo "observability:"
  (cd "$BASE_DIR" && "$BASE_DIR/deps/status-observability.sh") || true
fi
