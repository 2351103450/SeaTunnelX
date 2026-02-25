#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")" && pwd)"
RUNTIME_DIR="$BASE_DIR/runtime"

SEATUNNEL_METRICS_TARGETS="${SEATUNNEL_METRICS_TARGETS:-127.0.0.1:8081}"
SEATUNNEL_CLUSTER_LABEL="${SEATUNNEL_CLUSTER_LABEL:-seatunnel-5801}"
SEATUNNEL_SERVICE_LABEL="${SEATUNNEL_SERVICE_LABEL:-seatunnel-engine}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
GRAFANA_URL="${GRAFANA_URL:-http://127.0.0.1:3000}"
GRAFANA_URL="${GRAFANA_URL%/}"
GRAFANA_DOMAIN="${GRAFANA_DOMAIN:-}"
GRAFANA_PROXY_SUBPATH="${GRAFANA_PROXY_SUBPATH:-/api/v1/monitoring/proxy/grafana}"
GRAFANA_ROOT_URL="${GRAFANA_ROOT_URL:-${GRAFANA_PROXY_SUBPATH%/}/}"
GRAFANA_ADMIN_USER="${GRAFANA_ADMIN_USER:-admin}"
GRAFANA_ADMIN_PASSWORD="${GRAFANA_ADMIN_PASSWORD:-admin}"
ALERTMANAGER_WEBHOOK_URL="${ALERTMANAGER_WEBHOOK_URL:-}"

if [[ -z "$GRAFANA_DOMAIN" ]]; then
  GRAFANA_DOMAIN="${GRAFANA_URL#*://}"
  GRAFANA_DOMAIN="${GRAFANA_DOMAIN%%/*}"
  GRAFANA_DOMAIN="${GRAFANA_DOMAIN%%:*}"
  [[ -z "$GRAFANA_DOMAIN" ]] && GRAFANA_DOMAIN="127.0.0.1"
fi

mkdir -p \
  "$RUNTIME_DIR/prometheus/rules" \
  "$RUNTIME_DIR/prometheus/data" \
  "$RUNTIME_DIR/prometheus/logs" \
  "$RUNTIME_DIR/alertmanager/data" \
  "$RUNTIME_DIR/alertmanager/logs" \
  "$RUNTIME_DIR/grafana/data" \
  "$RUNTIME_DIR/grafana/logs" \
  "$RUNTIME_DIR/grafana/plugins" \
  "$RUNTIME_DIR/grafana/provisioning/datasources" \
  "$RUNTIME_DIR/grafana/provisioning/dashboards" \
  "$RUNTIME_DIR/grafana/provisioning/plugins" \
  "$RUNTIME_DIR/grafana/provisioning/alerting" \
  "$RUNTIME_DIR/grafana/dashboards"

# ---------- Alertmanager ----------
{
  echo "global:"
  echo "  resolve_timeout: 5m"
  echo
  echo "route:"
  echo "  receiver: default"
  echo "  group_by: [alertname, cluster, instance]"
  echo "  group_wait: 30s"
  echo "  group_interval: 5m"
  echo "  repeat_interval: 2h"
  echo
  echo "receivers:"
  echo "  - name: default"
  if [[ -n "$ALERTMANAGER_WEBHOOK_URL" ]]; then
    echo "    webhook_configs:"
    echo "      - url: '$ALERTMANAGER_WEBHOOK_URL'"
    echo "        send_resolved: true"
  fi
} > "$RUNTIME_DIR/alertmanager/alertmanager.yml"

# ---------- Prometheus ----------
{
  echo "global:"
  echo "  scrape_interval: 15s"
  echo "  evaluation_interval: 15s"
  echo
  echo "rule_files:"
  echo "  - $RUNTIME_DIR/prometheus/rules/*.yml"
  echo
  echo "alerting:"
  echo "  alertmanagers:"
  echo "    - static_configs:"
  echo "        - targets: ['127.0.0.1:9093']"
  echo
  echo "scrape_configs:"
  echo "  - job_name: 'prometheus'"
  echo "    static_configs:"
  echo "      - targets: ['127.0.0.1:9090']"
  echo
  echo "  - job_name: 'alertmanager'"
  echo "    static_configs:"
  echo "      - targets: ['127.0.0.1:9093']"
  echo
  echo "  - job_name: 'seatunnel_engine_http'"
  echo "    metrics_path: /metrics"
  echo "    static_configs:"
  echo "      - targets:"
  IFS=',' read -ra TARGETS <<< "$SEATUNNEL_METRICS_TARGETS"
  for target in "${TARGETS[@]}"; do
    target_trimmed="$(echo "$target" | xargs)"
    [[ -z "$target_trimmed" ]] && continue
    echo "          - '$target_trimmed'"
  done
  echo "        labels:"
  echo "          cluster: '$SEATUNNEL_CLUSTER_LABEL'"
  echo "          service: '$SEATUNNEL_SERVICE_LABEL'"
} > "$RUNTIME_DIR/prometheus/prometheus.yml"

# cleanup legacy generated file to avoid duplicated alert names
rm -f "$RUNTIME_DIR/prometheus/rules/seatunnel-alerts.yml"

cat > "$RUNTIME_DIR/prometheus/rules/seatunnel-default-rules.yml" <<'RULES'
groups:
  - name: seatunnel-default-recording
    interval: 30s
    rules:
      - record: seatunnel:job_thread_pool_queue_depth:max
        expr: max by (cluster, instance) (job_thread_pool_queueTaskCount{job="seatunnel_engine_http"})

      - record: seatunnel:job_thread_pool_submit_rate5m
        expr: sum by (cluster, instance) (rate(job_thread_pool_task_total{job="seatunnel_engine_http"}[5m]))

      - record: seatunnel:job_thread_pool_complete_rate5m
        expr: sum by (cluster, instance) (rate(job_thread_pool_completedTask_total{job="seatunnel_engine_http"}[5m]))

      - record: seatunnel:job_thread_pool_reject_rate5m
        expr: sum by (cluster, instance) (rate(job_thread_pool_rejection_total{job="seatunnel_engine_http"}[5m]))

      - record: seatunnel:job_thread_pool_backlog_gap_rate5m
        expr: seatunnel:job_thread_pool_submit_rate5m - seatunnel:job_thread_pool_complete_rate5m

      - record: seatunnel:jvm_heap_usage_percent
        expr: |
          100 * jvm_memory_bytes_used{job="seatunnel_engine_http",area="heap"}
          /
          clamp_min(jvm_memory_bytes_max{job="seatunnel_engine_http",area="heap"}, 1)

      - record: seatunnel:jvm_nonheap_usage_percent
        expr: |
          100 * jvm_memory_bytes_used{job="seatunnel_engine_http",area="nonheap"}
          /
          clamp_min(jvm_memory_bytes_max{job="seatunnel_engine_http",area="nonheap"}, 1)

      - record: seatunnel:gc_time_ratio_percent5m
        expr: 100 * rate(jvm_gc_collection_seconds_sum{job="seatunnel_engine_http"}[5m])

      - record: seatunnel:fd_usage_percent
        expr: 100 * process_open_fds{job="seatunnel_engine_http"} / clamp_min(process_max_fds{job="seatunnel_engine_http"}, 1)

      - record: seatunnel:hazelcast_executor_queue_util_percent
        expr: |
          100 * hazelcast_executor_queueSize{job="seatunnel_engine_http"}
          /
          clamp_min(
            hazelcast_executor_queueSize{job="seatunnel_engine_http"}
            + hazelcast_executor_queueRemainingCapacity{job="seatunnel_engine_http"},
            1
          )

  - name: seatunnel-default-alerts
    interval: 15s
    rules:
      - alert: SeaTunnelMetricsEndpointDown
        expr: up{job="seatunnel_engine_http"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "SeaTunnel metrics endpoint is down"
          description: "Metrics endpoint {{ $labels.instance }} (cluster {{ $labels.cluster }}) has been unreachable for more than 2 minutes."

      - alert: SeaTunnelNodeStateDown
        expr: node_state{job="seatunnel_engine_http"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "SeaTunnel node is down"
          description: "SeaTunnel node {{ $labels.address }} in cluster {{ $labels.cluster }} reports node_state=0."

      - alert: SeaTunnelPartitionUnsafe
        expr: |
          hazelcast_partition_isClusterSafe{job="seatunnel_engine_http"} == 0
          or
          hazelcast_partition_isLocalMemberSafe{job="seatunnel_engine_http"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "SeaTunnel partition safety lost"
          description: "Cluster {{ $labels.cluster }} has unsafe partition state on {{ $labels.instance }}."

      - alert: SeaTunnelJobThreadPoolQueueHigh
        expr: job_thread_pool_queueTaskCount{job="seatunnel_engine_http"} > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "SeaTunnel job thread pool queue is high"
          description: "Queue tasks on {{ $labels.instance }} exceeded 100 for 5 minutes."

      - alert: SeaTunnelCoordinatorBacklogGrowing
        expr: |
          seatunnel:job_thread_pool_queue_depth:max > 50
          and
          seatunnel:job_thread_pool_backlog_gap_rate5m > 0.5
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "SeaTunnel coordinator backlog keeps growing"
          description: "Queue depth remains high and submit rate is higher than complete rate on {{ $labels.instance }}."

      - alert: SeaTunnelCoordinatorRejectingTasks
        expr: seatunnel:job_thread_pool_reject_rate5m > 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "SeaTunnel coordinator is rejecting tasks"
          description: "Task rejections detected on {{ $labels.instance }} in cluster {{ $labels.cluster }}."

      - alert: SeaTunnelJVMHeapUsageHigh
        expr: seatunnel:jvm_heap_usage_percent > 85
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "SeaTunnel JVM heap usage high"
          description: "Heap usage on {{ $labels.instance }} is above 85% for over 10 minutes."

      - alert: SeaTunnelProcessCpuHigh
        expr: rate(process_cpu_seconds_total{job="seatunnel_engine_http"}[5m]) > 0.8
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "SeaTunnel process CPU usage high"
          description: "CPU usage on {{ $labels.instance }} is above 80% for over 10 minutes."

      - alert: SeaTunnelGCTimeHigh
        expr: seatunnel:gc_time_ratio_percent5m > 20
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "SeaTunnel JVM GC time high"
          description: "GC time ratio on {{ $labels.instance }} is high (>20%) for over 10 minutes."

      - alert: SeaTunnelFDUsageHigh
        expr: seatunnel:fd_usage_percent > 80
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "SeaTunnel file descriptor usage high"
          description: "FD usage on {{ $labels.instance }} exceeds 80% for over 10 minutes."

      - alert: SeaTunnelThreadDeadlock
        expr: jvm_threads_deadlocked{job="seatunnel_engine_http"} > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "SeaTunnel JVM thread deadlock detected"
          description: "Deadlocked JVM threads detected on {{ $labels.instance }}."
RULES

# ---------- Grafana ----------
cat > "$RUNTIME_DIR/grafana/grafana.ini" <<EOF2
[paths]
data = $RUNTIME_DIR/grafana/data
logs = $RUNTIME_DIR/grafana/logs
plugins = $RUNTIME_DIR/grafana/plugins
provisioning = $RUNTIME_DIR/grafana/provisioning

[server]
http_addr = 0.0.0.0
http_port = 3000
domain = $GRAFANA_DOMAIN
root_url = $GRAFANA_ROOT_URL
serve_from_sub_path = true
enforce_domain = false

[security]
admin_user = $GRAFANA_ADMIN_USER
admin_password = $GRAFANA_ADMIN_PASSWORD
allow_embedding = true

[users]
allow_sign_up = false

[auth.anonymous]
enabled = true
org_role = Viewer

[plugins]
preinstall =
EOF2

cat > "$RUNTIME_DIR/grafana/provisioning/datasources/prometheus.yml" <<EOF2
apiVersion: 1

datasources:
  - name: Prometheus
    uid: prometheus
    type: prometheus
    access: proxy
    url: $PROMETHEUS_URL
    isDefault: true
    editable: true
EOF2

cat > "$RUNTIME_DIR/grafana/provisioning/dashboards/default.yml" <<EOF2
apiVersion: 1

providers:
  - name: 'SeatunnelX Monitoring'
    orgId: 1
    folder: 'SeatunnelX'
    type: file
    disableDeletion: false
    editable: true
    options:
      path: $RUNTIME_DIR/grafana/dashboards
EOF2

# cleanup legacy dashboard file
rm -f "$RUNTIME_DIR/grafana/dashboards/seatunnel-overview.json"

cat > "$RUNTIME_DIR/grafana/dashboards/seatunnel-overview-en.json" <<'DASH'
{
  "id": null,
  "uid": "seatunnel-overview-en",
  "title": "SeaTunnelX Deep Monitoring",
  "timezone": "browser",
  "schemaVersion": 39,
  "version": 3,
  "refresh": "15s",
  "editable": true,
  "graphTooltip": 0,
  "time": {
    "from": "now-6h",
    "to": "now"
  },
  "templating": {
    "list": [
      {
        "name": "cluster",
        "type": "query",
        "datasource": {
          "type": "prometheus",
          "uid": "prometheus"
        },
        "query": "label_values(node_state{job=\"seatunnel_engine_http\"},cluster)",
        "definition": "label_values(node_state{job=\"seatunnel_engine_http\"},cluster)",
        "refresh": 1,
        "includeAll": true,
        "multi": true,
        "allValue": ".*",
        "current": {
          "text": "All",
          "value": [
            "$__all"
          ]
        }
      },
      {
        "name": "instance",
        "type": "query",
        "datasource": {
          "type": "prometheus",
          "uid": "prometheus"
        },
        "query": "label_values(node_state{job=\"seatunnel_engine_http\",cluster=~\"$cluster\"},instance)",
        "definition": "label_values(node_state{job=\"seatunnel_engine_http\",cluster=~\"$cluster\"},instance)",
        "refresh": 1,
        "includeAll": true,
        "multi": true,
        "allValue": ".*",
        "current": {
          "text": "All",
          "value": [
            "$__all"
          ]
        }
      }
    ]
  },
  "annotations": {
    "list": []
  },
  "panels": [
    {
      "id": 1,
      "type": "stat",
      "title": "Scrape Availability (Up)",
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 0,
        "y": 0
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "min(up{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "none",
          "mappings": [
            {
              "type": "value",
              "options": {
                "0": {
                  "text": "DOWN"
                },
                "1": {
                  "text": "UP"
                }
              }
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": null
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "orientation": "auto",
        "textMode": "auto",
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto"
      }
    },
    {
      "id": 2,
      "type": "stat",
      "title": "Seatunnel Node State",
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 4,
        "y": 0
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "min(node_state{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "none",
          "mappings": [
            {
              "type": "value",
              "options": {
                "0": {
                  "text": "DOWN"
                },
                "1": {
                  "text": "UP"
                }
              }
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": null
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "orientation": "auto",
        "textMode": "auto",
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto"
      }
    },
    {
      "id": 3,
      "type": "stat",
      "title": "Cluster Safe",
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 8,
        "y": 0
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "min(hazelcast_partition_isClusterSafe{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "none",
          "mappings": [
            {
              "type": "value",
              "options": {
                "0": {
                  "text": "UNSAFE"
                },
                "1": {
                  "text": "SAFE"
                }
              }
            }
          ],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "red",
                "value": null
              },
              {
                "color": "green",
                "value": 1
              }
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "orientation": "auto",
        "textMode": "auto",
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto"
      }
    },
    {
      "id": 4,
      "type": "stat",
      "title": "Running Jobs",
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 12,
        "y": 0
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum(job_count{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",type=\"running\"})",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "none"
        },
        "overrides": []
      },
      "options": {
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "orientation": "auto",
        "textMode": "auto",
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto"
      }
    },
    {
      "id": 5,
      "type": "stat",
      "title": "Failing+Failed Jobs",
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 16,
        "y": 0
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum(job_count{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",type=~\"failing|failed\"})",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "none",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "orange",
                "value": 1
              },
              {
                "color": "red",
                "value": 3
              }
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "orientation": "auto",
        "textMode": "auto",
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto"
      }
    },
    {
      "id": 6,
      "type": "stat",
      "title": "Coordinator Queue Depth",
      "gridPos": {
        "h": 4,
        "w": 4,
        "x": 20,
        "y": 0
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "max(job_thread_pool_queueTaskCount{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "none",
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {
                "color": "green",
                "value": null
              },
              {
                "color": "orange",
                "value": 20
              },
              {
                "color": "red",
                "value": 100
              }
            ]
          }
        },
        "overrides": []
      },
      "options": {
        "reduceOptions": {
          "calcs": [
            "lastNotNull"
          ],
          "fields": "",
          "values": false
        },
        "orientation": "auto",
        "textMode": "auto",
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto"
      }
    },
    {
      "id": 7,
      "type": "timeseries",
      "title": "Job Lifecycle State Distribution",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 4
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum by(type) (job_count{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "{{type}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto",
            "stacking": {
              "mode": "normal",
              "group": "A"
            }
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 8,
      "type": "timeseries",
      "title": "Coordinator Throughput (5m)",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 4
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum(rate(job_thread_pool_task_total{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}[5m]))",
          "legendFormat": "submitted/s",
          "refId": "A"
        },
        {
          "expr": "sum(rate(job_thread_pool_completedTask_total{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}[5m]))",
          "legendFormat": "completed/s",
          "refId": "B"
        },
        {
          "expr": "sum(rate(job_thread_pool_rejection_total{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}[5m]))",
          "legendFormat": "rejected/s",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "ops",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 9,
      "type": "timeseries",
      "title": "Coordinator Saturation",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 12
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "max(job_thread_pool_activeCount{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "active threads",
          "refId": "A"
        },
        {
          "expr": "max(job_thread_pool_poolSize{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "pool size",
          "refId": "B"
        },
        {
          "expr": "max(job_thread_pool_queueTaskCount{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "queue depth",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 10,
      "type": "timeseries",
      "title": "Hazelcast Executor Queue by Type",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 12
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum by(type) (hazelcast_executor_queueSize{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "{{type}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto",
            "stacking": {
              "mode": "normal",
              "group": "A"
            }
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 11,
      "type": "timeseries",
      "title": "Hazelcast Queue Utilization %",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 20
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "100 * hazelcast_executor_queueSize{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"} / clamp_min(hazelcast_executor_queueSize{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"} + hazelcast_executor_queueRemainingCapacity{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}, 1)",
          "legendFormat": "{{type}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "percent",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 12,
      "type": "timeseries",
      "title": "JVM Memory Pressure %",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 20
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "100 * jvm_memory_bytes_used{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",area=\"heap\"} / clamp_min(jvm_memory_bytes_max{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",area=\"heap\"}, 1)",
          "legendFormat": "heap {{instance}}",
          "refId": "A"
        },
        {
          "expr": "100 * jvm_memory_bytes_used{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",area=\"nonheap\"} / clamp_min(jvm_memory_bytes_max{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",area=\"nonheap\"}, 1)",
          "legendFormat": "nonheap {{instance}}",
          "refId": "B"
        },
        {
          "expr": "100 * jvm_memory_pool_bytes_used{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",pool=\"G1 Old Gen\"} / clamp_min(jvm_memory_pool_bytes_max{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\",pool=\"G1 Old Gen\"}, 1)",
          "legendFormat": "old-gen {{instance}}",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "percent",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 13,
      "type": "timeseries",
      "title": "GC Time Ratio % (5m)",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 28
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "100 * rate(jvm_gc_collection_seconds_sum{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}[5m])",
          "legendFormat": "{{instance}} {{gc}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "percent",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 14,
      "type": "timeseries",
      "title": "Thread State Breakdown",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 28
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum by(state) (jvm_threads_state{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "{{state}}",
          "refId": "A"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto",
            "stacking": {
              "mode": "normal",
              "group": "A"
            }
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 15,
      "type": "timeseries",
      "title": "Process CPU & FD Pressure",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 36
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "100 * rate(process_cpu_seconds_total{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}[5m])",
          "legendFormat": "cpu %",
          "refId": "A"
        },
        {
          "expr": "100 * process_open_fds{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"} / clamp_min(process_max_fds{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}, 1)",
          "legendFormat": "fd usage %",
          "refId": "B"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "percent",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 16,
      "type": "timeseries",
      "title": "Process Memory Footprint",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 36
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "process_resident_memory_bytes{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}",
          "legendFormat": "RSS {{instance}}",
          "refId": "A"
        },
        {
          "expr": "process_virtual_memory_bytes{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}",
          "legendFormat": "Virtual {{instance}}",
          "refId": "B"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "bytes",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 17,
      "type": "timeseries",
      "title": "Critical Anomaly Signals",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 0,
        "y": 44
      },
      "datasource": {
        "type": "prometheus",
        "uid": "prometheus"
      },
      "targets": [
        {
          "expr": "sum(rate(job_thread_pool_rejection_total{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"}[5m]))",
          "legendFormat": "rejection/s",
          "refId": "A"
        },
        {
          "expr": "max(jvm_threads_deadlocked{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "deadlocked threads",
          "refId": "B"
        },
        {
          "expr": "1 - min(hazelcast_partition_isLocalMemberSafe{job=\"seatunnel_engine_http\",cluster=~\"$cluster\",instance=~\"$instance\"})",
          "legendFormat": "partition unsafe flag (1=unsafe)",
          "refId": "C"
        }
      ],
      "fieldConfig": {
        "defaults": {
          "unit": "short",
          "custom": {
            "drawStyle": "line",
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "fillOpacity": 10,
            "showPoints": "never",
            "spanNulls": true,
            "axisPlacement": "auto"
          }
        },
        "overrides": []
      },
      "options": {
        "legend": {
          "displayMode": "table",
          "placement": "bottom",
          "calcs": [
            "lastNotNull",
            "max"
          ]
        },
        "tooltip": {
          "mode": "multi",
          "sort": "desc"
        }
      }
    },
    {
      "id": 18,
      "type": "text",
      "title": "Deep Troubleshooting Playbook",
      "gridPos": {
        "h": 8,
        "w": 12,
        "x": 12,
        "y": 44
      },
      "options": {
        "mode": "markdown",
        "content": "### Deep Troubleshooting Path\n1. **Jobs stuck / latency rising**: check `Coordinator Queue Depth` and `Coordinator Throughput` first. If queue keeps rising while completed/s stays low, focus on scheduler bottlenecks.\n2. **Cluster up but jobs not progressing**: inspect `Cluster Safe` and `Hazelcast Executor Queue by Type`; high queue with unsafe partition usually means coordination-layer blockage.\n3. **Frequent failures/flapping**: correlate `Failing+Failed Jobs` with rejection/s in `Critical Anomaly Signals`.\n4. **Slow memory degradation**: track old-gen in `JVM Memory Pressure %`; if old-gen remains high and GC ratio rises, investigate state accumulation / job leakage.\n5. **High CPU but no throughput gain**: compare `Process CPU & FD Pressure` vs `Coordinator Throughput`; high CPU + low completed/s often indicates busy-waiting or downstream blocking.\n6. **Resource exhaustion risk**: monitor fd usage % and RSS growth trend for proactive scaling / connection lifecycle optimization."
      }
    }
  ]
}
DASH

RUNTIME_DIR="$RUNTIME_DIR" python3 - <<'PY'
import json
import os
from pathlib import Path

runtime_dir = Path(os.environ["RUNTIME_DIR"]) / "grafana" / "dashboards"
en_file = runtime_dir / "seatunnel-overview-en.json"
zh_file = runtime_dir / "seatunnel-overview-zh.json"

dashboard = json.loads(en_file.read_text())
dashboard["uid"] = "seatunnel-overview-zh"
dashboard["title"] = "SeaTunnelX 深度监控"

title_map = {
    "Scrape Availability (Up)": "抓取可用性 (Up)",
    "Seatunnel Node State": "SeaTunnel 节点状态",
    "Cluster Safe": "集群分区安全",
    "Running Jobs": "运行中作业",
    "Failing+Failed Jobs": "失败/失败中作业",
    "Coordinator Queue Depth": "协调器队列深度",
    "Job Lifecycle State Distribution": "作业生命周期状态分布",
    "Coordinator Throughput (5m)": "协调器吞吐 (5分钟)",
    "Coordinator Saturation": "协调器饱和度",
    "Hazelcast Executor Queue by Type": "Hazelcast 执行器队列（按类型）",
    "Hazelcast Queue Utilization %": "Hazelcast 队列利用率 %",
    "JVM Memory Pressure %": "JVM 内存压力 %",
    "GC Time Ratio % (5m)": "GC 时间占比 % (5分钟)",
    "Thread State Breakdown": "线程状态分布",
    "Process CPU & FD Pressure": "进程 CPU 与 FD 压力",
    "Process Memory Footprint": "进程内存占用",
    "Critical Anomaly Signals": "关键异常信号",
    "Deep Troubleshooting Playbook": "深度排障手册",
}

playbook_zh = (
    "### 深度排障路径\\n"
    "1. **任务卡住/延迟升高**：先看 `Coordinator Queue Depth` 与 `Coordinator Throughput`，若队列持续升、completed/s 低，优先定位调度瓶颈。\\n"
    "2. **集群可用但任务不推进**：看 `Cluster Safe`、`Hazelcast Executor Queue by Type`，若 queue 高且分区不安全，多为协调层阻塞。\\n"
    "3. **频繁失败或抖动**：联动 `Failing+Failed Jobs` 与 `Critical Anomaly Signals` 中 rejection/s。\\n"
    "4. **内存慢性退化**：关注 `JVM Memory Pressure %` 的 old-gen，若长期高位且 GC 占比升高，排查状态累积/作业泄漏。\\n"
    "5. **CPU 高但吞吐不升**：对比 `Process CPU & FD Pressure` 与 `Coordinator Throughput`，CPU 高+completed/s 低通常是忙等或下游阻塞。\\n"
    "6. **资源耗尽风险**：持续观察 fd usage % 与 RSS 增长趋势，提前扩容或优化连接生命周期。"
)

for panel in dashboard.get("panels", []):
    title = panel.get("title")
    if title in title_map:
        panel["title"] = title_map[title]
    if panel.get("type") == "text" and panel.get("id") == 18:
        panel.setdefault("options", {})["content"] = playbook_zh

zh_file.write_text(json.dumps(dashboard, ensure_ascii=False, indent=2) + "\n")
PY

echo "Default observability config generated:"
echo "  - target metrics: $SEATUNNEL_METRICS_TARGETS"
echo "  - cluster label : $SEATUNNEL_CLUSTER_LABEL"
echo "  - prometheus   : $PROMETHEUS_URL"
echo "  - grafana      : $GRAFANA_URL"
