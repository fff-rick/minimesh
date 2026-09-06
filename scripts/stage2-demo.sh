#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
BIN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/minimesh-stage2.XXXXXX")

cleanup() {
  kill "${SIDECAR_PID:-}" "${CONTROL_PID:-}" "${BACKEND_PID:-}" 2>/dev/null || true
  wait "${SIDECAR_PID:-}" "${CONTROL_PID:-}" "${BACKEND_PID:-}" 2>/dev/null || true
  rm -rf "$BIN_DIR"
}
trap cleanup EXIT INT TERM

require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 2" >&2
    exit 1
  fi
}

for port in 17070 17071 18080 18081 19090; do require_port_free "$port"; done
if ! curl -fsS http://127.0.0.1:2379/health >/dev/null; then
  echo "etcd is not healthy; run 'make dev' first" >&2
  exit 1
fi

go build -buildvcs=false -o "$BIN_DIR/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$BIN_DIR/control-plane" ./cmd/control-plane
go build -buildvcs=false -o "$BIN_DIR/sidecar" ./cmd/sidecar
go build -buildvcs=false -o "$BIN_DIR/client" ./examples/go/client

wait_http() {
  url=$1
  name=$2
  for _ in $(seq 1 100); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  echo "$name did not become ready" >&2
  return 1
}

call_service() {
  "$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service echo --message "$1"
}

wait_for_discovery() {
  for _ in $(seq 1 50); do
    if output=$(call_service discovered 2>&1); then
      printf '%s\n' "$output"
      return 0
    fi
    sleep 0.1
  done
  echo "sidecar did not receive the echo endpoint" >&2
  printf '%s\n' "$output" >&2
  return 1
}

"$BIN_DIR/backend" --listen :19090 > /tmp/minimesh-stage2-backend.log 2>&1 & BACKEND_PID=$!
"$BIN_DIR/control-plane" --listen :17070 > /tmp/minimesh-stage2-control.log 2>&1 & CONTROL_PID=$!
"$BIN_DIR/sidecar" --listen :18080 > /tmp/minimesh-stage2-sidecar.log 2>&1 & SIDECAR_PID=$!

wait_http http://127.0.0.1:17070/healthz "control plane"
wait_http http://127.0.0.1:18081/readyz "sidecar control stream"
RESP=$(curl -fsS -X POST http://127.0.0.1:17070/v1/registry/register \
  -H 'content-type: application/json' \
  -d '{"endpoint":{"service":"echo","instance_id":"echo-1","address":"127.0.0.1:19090"},"ttl_seconds":30}')
LEASE=$(printf '%s' "$RESP" | python3 -c 'import json,sys; print(json.load(sys.stdin)["lease_id"])')

echo "[stage2] discovered request should succeed"
wait_for_discovery

curl -fsS -X POST http://127.0.0.1:17070/v1/registry/deregister \
  -H 'content-type: application/json' \
  -d "{\"lease_id\":$LEASE,\"service\":\"echo\",\"instance_id\":\"echo-1\"}" >/dev/null

echo "[stage2] after deregistration request should fail without restarting sidecar"
for _ in $(seq 1 50); do
  if ! call_service removed >/tmp/minimesh-stage2-after-delete.log 2>&1; then
    cat /tmp/minimesh-stage2-after-delete.log
    exit 0
  fi
  sleep 0.1
done
echo "unexpected success after deregistration" >&2
cat /tmp/minimesh-stage2-after-delete.log
exit 1
