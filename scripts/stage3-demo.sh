#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
ALG=${1:-round_robin}
BIN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/minimesh-stage3.XXXXXX")
cleanup() {
  kill ${PIDS:-} 2>/dev/null || true
  if [ -n "${PIDS:-}" ]; then wait $PIDS 2>/dev/null || true; fi
  rm -rf "$BIN_DIR"
}
trap cleanup EXIT INT TERM

require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 3" >&2
    exit 1
  fi
}

for port in 17070 17071 18080 18081 19091 19092 19093; do require_port_free "$port"; done
if ! curl -fsS http://127.0.0.1:2379/health >/dev/null; then
  echo "etcd is not healthy; run 'make dev' first" >&2
  exit 1
fi
go build -buildvcs=false -o "$BIN_DIR/backend" ./examples/go/echo-server
go build -buildvcs=false -o "$BIN_DIR/control-plane" ./cmd/control-plane
go build -buildvcs=false -o "$BIN_DIR/sidecar" ./cmd/sidecar
go build -buildvcs=false -o "$BIN_DIR/client" ./examples/go/client

PIDS=""
for spec in 'echo-1:19091' 'echo-2:19092' 'echo-3:19093'; do
  id=${spec%%:*}; port=${spec##*:}
  "$BIN_DIR/backend" --listen ":$port" --id "$id" >"/tmp/minimesh-stage3-$id.log" 2>&1 & PIDS="$PIDS $!"
done
"$BIN_DIR/control-plane" --listen :17070 >/tmp/minimesh-stage3-control.log 2>&1 & PIDS="$PIDS $!"
"$BIN_DIR/sidecar" --listen :18080 --lb "$ALG" >/tmp/minimesh-stage3-sidecar.log 2>&1 & PIDS="$PIDS $!"
sleep 2

register() {
  id=$1; port=$2; weight=$3
  curl -fsS -X POST http://127.0.0.1:17070/v1/registry/register -H 'content-type: application/json' \
    -d "{\"endpoint\":{\"service\":\"echo\",\"instance_id\":\"$id\",\"address\":\"127.0.0.1:$port\",\"metadata\":{\"minimesh.weight\":\"$weight\"}},\"ttl_seconds\":120}" >/tmp/minimesh-stage3-$id-register.json
}
register echo-1 19091 1
register echo-2 19092 2
register echo-3 19093 7
sleep 1

echo "[stage3] algorithm=$ALG; first 12 requests"
i=0
while [ $i -lt 12 ]; do
  "$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service echo --message "request-$i"
  i=$((i+1))
done

echo "[stage3] removing echo-2; subsequent requests must not hit it"
curl -fsS -X POST http://127.0.0.1:17070/v1/registry/deregister -H 'content-type: application/json' \
  -d '{"service":"echo","instance_id":"echo-2"}' >/dev/null
sleep 1
i=0
while [ $i -lt 6 ]; do
  out=$("$BIN_DIR/client" --sidecar 127.0.0.1:18080 --service echo --message "after-delete-$i")
  echo "$out"
  echo "$out" | grep -q 'echo@echo-2:' && { echo "removed endpoint selected" >&2; exit 1; }
  i=$((i+1))
done
