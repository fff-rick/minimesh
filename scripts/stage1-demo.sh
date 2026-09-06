#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

backend_log="${TMPDIR:-/tmp}/minimesh-stage1-backend.log"
sidecar_log="${TMPDIR:-/tmp}/minimesh-stage1-sidecar.log"
bin_dir=$(mktemp -d "${TMPDIR:-/tmp}/minimesh-stage1.XXXXXX")

require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 1" >&2
    exit 1
  fi
}

require_port_free 18080
require_port_free 19090

go build -buildvcs=false -o "$bin_dir/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$bin_dir/sidecar" ./cmd/sidecar
go build -buildvcs=false -o "$bin_dir/client" ./examples/go/client

"$bin_dir/backend" --listen 127.0.0.1:19090 >"$backend_log" 2>&1 &
backend_pid=$!
"$bin_dir/sidecar" --listen 127.0.0.1:18080 >"$sidecar_log" 2>&1 &
sidecar_pid=$!

cleanup() {
  kill "$sidecar_pid" "$backend_pid" 2>/dev/null || true
  wait "$sidecar_pid" "$backend_pid" 2>/dev/null || true
  rm -rf "$bin_dir"
}
trap cleanup EXIT

wait_port() {
  local host=$1 port=$2 name=$3
  for _ in $(seq 1 80); do
    if (echo >"/dev/tcp/$host/$port") >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$backend_pid" 2>/dev/null || ! kill -0 "$sidecar_pid" 2>/dev/null; then
      echo "Stage 1 demo process exited unexpectedly" >&2
      echo "--- backend ---" >&2
      cat "$backend_log" >&2 || true
      echo "--- sidecar ---" >&2
      cat "$sidecar_log" >&2 || true
      return 1
    fi
    sleep 0.1
  done
  echo "$name did not become ready at $host:$port" >&2
  return 1
}

wait_port 127.0.0.1 19090 backend
wait_port 127.0.0.1 18080 sidecar

"$bin_dir/client" \
  --sidecar 127.0.0.1:18080 \
  --backend 127.0.0.1:19090 \
  --message hello
