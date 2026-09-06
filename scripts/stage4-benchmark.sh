#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd); cd "$ROOT"
REQUESTS=${REQUESTS:-5000}; CONCURRENCY=${CONCURRENCY:-50}
BIN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/minimesh-stage4.XXXXXX")
PIDS=""
cleanup(){
  kill ${PIDS:-} 2>/dev/null || true
  if [ -n "${PIDS:-}" ]; then wait $PIDS 2>/dev/null || true; fi
  rm -rf "$BIN_DIR"
}
trap cleanup EXIT INT TERM

require_port_free(){
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 4" >&2
    exit 1
  fi
}
for port in 18080 18081 19090; do require_port_free "$port"; done

go build -buildvcs=false -o "$BIN_DIR/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$BIN_DIR/sidecar" ./cmd/sidecar
go build -buildvcs=false -o "$BIN_DIR/load-client" ./examples/go/load-client

"$BIN_DIR/backend" --listen :19090 --id stage4-backend >/tmp/minimesh-stage4-backend.log 2>&1 & BACKEND_PID=$!; PIDS="$PIDS $BACKEND_PID"
sleep 1
clock_ticks(){ awk '{print $14+$15}' "/proc/$1/stat"; }
hz(){ getconf CLK_TCK; }
run_case(){
  name=$1; pool=$2
  log="/tmp/minimesh-stage4-$name-sidecar.log"
  "$BIN_DIR/sidecar" --listen :18080 --connection-pool="$pool" --max-connections=2 --max-idle=30s >"$log" 2>&1 & pid=$!; PIDS="$PIDS $pid"
  sleep 2
  before=$(clock_ticks "$pid" 2>/dev/null || echo 0)
  "$BIN_DIR/load-client" --sidecar 127.0.0.1:18080 --backend 127.0.0.1:19090 --requests "$REQUESTS" --concurrency "$CONCURRENCY" >"/tmp/minimesh-stage4-$name-result.json"
  after=$(clock_ticks "$pid" 2>/dev/null || echo "$before")
  kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true
  ticks=$((after-before)); cpu=$(awk -v t="$ticks" -v h="$(hz)" 'BEGIN{printf "%.3f", t/h}')
  echo "=== $name ==="
  cat "/tmp/minimesh-stage4-$name-result.json"
  echo "sidecar_cpu_seconds=$cpu"
  grep 'created_total' "$log" | tail -1 || true
  PIDS=$(echo "$PIDS" | sed "s/ $pid//")
}
run_case no_pool false
run_case pooled true
