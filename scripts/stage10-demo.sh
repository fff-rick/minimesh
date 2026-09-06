#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
COMPOSE="docker compose -f deploy/docker/docker-compose.yml -f deploy/docker/docker-compose.stage10.yml --profile stage9"
CLIENT_COMPOSE="$COMPOSE --profile stage9-client"
cleanup() { $COMPOSE down --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

build_topology() {
  attempt=1
  while [ "$attempt" -le 3 ]; do
    if $COMPOSE build; then
      return 0
    fi
    echo "[stage10] image build failed (attempt $attempt/3); retrying in 3 seconds" >&2
    attempt=$((attempt + 1))
    sleep 3
  done
  return 1
}

build_topology
$COMPOSE up --quiet-pull --no-build -d
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:37070/healthz >/dev/null 2>&1; then break; fi
  sleep 1
done
register() { curl -fsS -X POST http://127.0.0.1:37070/v1/registry/register -H 'content-type: application/json' -d "$1" >/dev/null; }
register '{"endpoint":{"service":"inventory","instance_id":"stage10-inventory","address":"stage9-inventory:19091"},"ttl_seconds":120}'
register '{"endpoint":{"service":"recommendation","instance_id":"stage10-recommendation","address":"stage9-recommendation:19092"},"ttl_seconds":120}'
sleep 3

echo '[stage10] Java -> Sidecar -> Go -> Sidecar -> Python'
$CLIENT_COMPOSE run --rm stage9-order-client
if $CLIENT_COMPOSE run --rm -e ORDER_SKU=missing stage9-order-client; then exit 1; fi
# Batch exporters use a five-second schedule by default; give all four
# processes time to flush before querying Jaeger.
sleep 7
curl -fsS http://127.0.0.1:38081/metrics | grep -q 'minimesh_request_total'
curl -fsS http://127.0.0.1:9090/api/v1/query?query=minimesh_request_total | grep -q '"status":"success"'
trace_json=$(curl -fsS 'http://127.0.0.1:16686/api/traces?service=order&limit=20')
for service in order sidecar-order inventory sidecar-inventory recommendation; do
  printf '%s' "$trace_json" | grep -q '"serviceName":"'"$service"'"'
done
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:3000/api/health >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS 'http://127.0.0.1:3000/api/dashboards/uid/minimesh-stage10' | grep -q 'MiniMesh Stage 10 Observability'
echo '[stage10] dashboard: http://localhost:3000/d/minimesh-stage10  traces: http://localhost:16686'
