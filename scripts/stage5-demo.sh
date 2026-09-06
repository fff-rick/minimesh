#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
BIN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/minimesh-stage5.XXXXXX")

cleanup() {
  kill ${PIDS:-} 2>/dev/null || true
  if [ -n "${PIDS:-}" ]; then wait $PIDS 2>/dev/null || true; fi
  rm -rf "$BIN_DIR"
}
trap cleanup EXIT INT TERM

require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 5" >&2
    exit 1
  fi
}

for port in 17070 17071 18080 18081 19090 19091 19092 19093; do require_port_free "$port"; done
if ! curl -fsS http://127.0.0.1:2379/health >/dev/null; then
  echo "etcd is not healthy; run 'make dev' first" >&2
  exit 1
fi

go build -buildvcs=false -o "$BIN_DIR/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$BIN_DIR/faulty-backend" ./examples/go/faulty-echo-server
go build -buildvcs=false -o "$BIN_DIR/control-plane" ./cmd/control-plane
go build -buildvcs=false -o "$BIN_DIR/sidecar" ./cmd/sidecar
go build -buildvcs=false -o "$BIN_DIR/client" ./examples/go/client

PIDS=""
"$BIN_DIR/backend" --listen :19090 --id healthy >/tmp/minimesh-stage5-healthy.log 2>&1 & PIDS="$PIDS $!"
"$BIN_DIR/faulty-backend" --listen :19091 --id internal --mode internal >/tmp/minimesh-stage5-internal.log 2>&1 & PIDS="$PIDS $!"
"$BIN_DIR/faulty-backend" --listen :19092 --id unavailable --mode unavailable >/tmp/minimesh-stage5-unavailable.log 2>&1 & PIDS="$PIDS $!"
"$BIN_DIR/faulty-backend" --listen :19093 --id slow --mode timeout --delay 2s >/tmp/minimesh-stage5-slow.log 2>&1 & PIDS="$PIDS $!"
"$BIN_DIR/control-plane" --listen :17070 >/tmp/minimesh-stage5-control.log 2>&1 & PIDS="$PIDS $!"
"$BIN_DIR/sidecar" --listen :18080 --lb round_robin --request-timeout 350ms --max-attempts 3 --retry-backoff 20ms --retry-max-backoff 80ms --retryable-codes unavailable,internal --retry-budget-rate 100 --retry-budget-burst 100 >/tmp/minimesh-stage5-sidecar.log 2>&1 & PIDS="$PIDS $!"
sleep 2

register() {
  service=$1; id=$2; port=$3
  curl -fsS -X POST http://127.0.0.1:17070/v1/registry/register -H 'content-type: application/json' \
    -d "{\"endpoint\":{\"service\":\"$service\",\"instance_id\":\"$id\",\"address\":\"127.0.0.1:$port\"},\"ttl_seconds\":120}" >/dev/null
}

# The lexicographically first endpoint is faulty, so the first RR attempt fails
# and the retry must re-pick the healthy endpoint.
register internal-demo a-internal 19091
register internal-demo b-healthy 19090
register unavailable-demo a-unavailable 19092
register unavailable-demo b-healthy 19090
register timeout-demo a-slow 19093
register all-bad a-internal 19091
sleep 1

echo "[stage5] retry an Internal/500-like backend failure and fail over to healthy instance"
"$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service internal-demo --message internal-retry --timeout 2s

echo "[stage5] retry a temporary Unavailable backend and fail over to healthy instance"
"$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service unavailable-demo --message unavailable-retry --timeout 2s

echo "[stage5] request timeout must cap the complete upstream operation"
if "$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service timeout-demo --message slow --timeout 2s >/tmp/minimesh-stage5-timeout.out 2>&1; then
  echo "unexpected timeout-demo success" >&2
  exit 1
fi
cat /tmp/minimesh-stage5-timeout.out

echo "[stage5] all retry attempts failing must return instead of retrying forever"
if "$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service all-bad --message bounded-failure --timeout 2s >/tmp/minimesh-stage5-all-bad.out 2>&1; then
  echo "unexpected all-bad success" >&2
  exit 1
fi
cat /tmp/minimesh-stage5-all-bad.out
