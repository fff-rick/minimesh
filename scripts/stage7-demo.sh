#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
TMP=/tmp/minimesh-stage7
mkdir -p "$TMP"
require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 7" >&2
    exit 1
  fi
}
for port in 17070 17071 18080 18081 19090; do require_port_free "$port"; done
if ! curl -fsS http://127.0.0.1:2379/health >/dev/null; then
  echo "etcd is not healthy; run 'make dev' first" >&2
  exit 1
fi
cleanup() {
  kill ${PIDS:-} 2>/dev/null || true
  if [ -n "${PIDS:-}" ]; then wait $PIDS 2>/dev/null || true; fi
}
trap cleanup EXIT INT TERM

go build -buildvcs=false -o "$TMP/load-client" ./examples/go/load-client
go build -buildvcs=false -o "$TMP/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$TMP/control-plane" ./cmd/control-plane
go build -buildvcs=false -o "$TMP/sidecar" ./cmd/sidecar

PIDS=""
"$TMP/backend" --listen :19090 --id rate-limit-backend >"$TMP/backend.log" 2>&1 & PIDS="$PIDS $!"
"$TMP/control-plane" --listen :17070 >"$TMP/control.log" 2>&1 & PIDS="$PIDS $!"
"$TMP/sidecar" --listen :18080 --lb round_robin --connection-pool=true --max-attempts=1 --circuit-breaker=false \
  --rate-limit=true --service-rate-limits='rate-demo=1000:1000' >"$TMP/sidecar.log" 2>&1 & PIDS="$PIDS $!"
sleep 2

curl -fsS -X POST http://127.0.0.1:17070/v1/registry/register -H 'content-type: application/json' \
  -d '{"endpoint":{"service":"rate-demo","instance_id":"rate-1","address":"127.0.0.1:19090"},"ttl_seconds":120}' >/dev/null
sleep 1

echo '[stage7] burst test: input 5000 requests against service rule rate=1000/s burst=1000'
"$TMP/load-client" --sidecar 127.0.0.1:18080 --service rate-demo --requests 5000 --concurrency 500 --timeout 3s

echo '[stage7] exported rate-limit metrics:'
curl -fsS http://127.0.0.1:18081/metrics | grep 'minimesh_rate_limit_.*service="rate-demo"'
echo '[stage7] PASS when rate_limited > 0, other_errors = 0, and rejected_total is exported. Exact admitted count depends on elapsed wall time because tokens refill during the run.'
