#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNTIME_DIR="$BASE_DIR/runtime"

for svc in alertmanager prometheus grafana; do
  pidfile="$RUNTIME_DIR/$svc/$svc.pid"
  [[ "$svc" == "grafana" ]] && pidfile="$RUNTIME_DIR/grafana/grafana.pid"
  if [[ -f "$pidfile" ]]; then
    pid="$(cat "$pidfile" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      echo "$svc: running (pid=$pid)"
    else
      echo "$svc: pidfile exists but process not running"
    fi
  else
    echo "$svc: stopped"
  fi
done

echo
echo "Ports:"
ss -lntp | grep -E ':9090|:9093|:3000' || true
