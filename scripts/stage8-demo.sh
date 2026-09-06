#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
TMP=/tmp/minimesh-stage8
mkdir -p "$TMP"
require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 8" >&2
    exit 1
  fi
}
for port in 27070 27071 28080 28081 29090; do require_port_free "$port"; done
if ! curl -fsS http://127.0.0.1:2379/health >/dev/null; then
  echo "etcd is not healthy; run 'make dev' first" >&2
  exit 1
fi
cleanup() {
  kill ${PIDS:-} 2>/dev/null || true
  if [ -n "${PIDS:-}" ]; then wait $PIDS 2>/dev/null || true; fi
}
trap cleanup EXIT INT TERM

wait_http() {
  url=$1
  name=$2
  for _ in $(seq 1 100); do
    if curl -fsS "$url" >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  echo "$name did not become ready" >&2
  return 1
}

wait_port_free() {
  port=$1
  for _ in $(seq 1 100); do
    if ! ss -ltnH "sport = :$port" | grep -q .; then return 0; fi
    sleep 0.1
  done
  echo "port $port was not released by the stopped control plane" >&2
  return 1
}

wait_for_call() {
  message=$1
  for _ in $(seq 1 100); do
    if output=$("$TMP/client" --sidecar 127.0.0.1:28080 --service echo --message "$message" 2>&1); then
      printf '%s\n' "$output"
      return 0
    fi
    sleep 0.1
  done
  echo "sidecar did not recover its route after control-plane restart" >&2
  printf '%s\n' "$output" >&2
  return 1
}

go build -buildvcs=false -o "$TMP/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$TMP/control-plane" ./cmd/control-plane
go build -buildvcs=false -o "$TMP/sidecar" ./cmd/sidecar
go build -buildvcs=false -o "$TMP/client" ./examples/go/client

PIDS=""
"$TMP/backend" --listen :29090 --id control-stream-backend >"$TMP/backend.log" 2>&1 & PIDS="$PIDS $!"
"$TMP/control-plane" --listen :27070 --control-listen :27071 >"$TMP/control.log" 2>&1 & PIDS="$PIDS $!"
"$TMP/sidecar" --listen :28080 --metrics-listen :28081 --control-plane 127.0.0.1:27071 --sidecar-id stage8-demo --max-attempts=1 >"$TMP/sidecar.log" 2>&1 & PIDS="$PIDS $!"
wait_http http://127.0.0.1:27070/healthz "control plane"
wait_http http://127.0.0.1:28081/readyz "sidecar control stream"

curl -fsS -X POST http://127.0.0.1:27070/v1/registry/register -H 'content-type: application/json' \
  -d '{"endpoint":{"service":"echo","instance_id":"echo-1","address":"127.0.0.1:29090"},"ttl_seconds":120}' >/dev/null
echo '[stage8] initial control-stream snapshot should route successfully'
wait_for_call initial

CONTROL_PID=$(printf '%s' "$PIDS" | awk '{print $2}')
kill "$CONTROL_PID"
wait_port_free 27070
wait_port_free 27071
"$TMP/control-plane" --listen :27070 --control-listen :27071 >"$TMP/control-restarted.log" 2>&1 & PIDS="$PIDS $!"
wait_http http://127.0.0.1:27070/healthz "restarted control plane"
echo '[stage8] after control-plane restart, sidecar reconnects and full-syncs'
wait_for_call recovered
