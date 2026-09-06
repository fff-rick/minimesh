#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

TMP=/tmp/minimesh-stage6
mkdir -p "$TMP"

require_port_free() {
  if ss -ltnH "sport = :$1" | grep -q .; then
    echo "port $1 is already in use; stop the conflicting service before running Stage 6" >&2
    exit 1
  fi
}
for port in 17070 17071 18080 18081 19094; do require_port_free "$port"; done
if ! curl -fsS http://127.0.0.1:2379/health >/dev/null; then
  echo "etcd is not healthy; run 'make dev' first" >&2
  exit 1
fi

cleanup() {
  kill ${PIDS:-} 2>/dev/null || true
  if [ -n "${PIDS:-}" ]; then wait $PIDS 2>/dev/null || true; fi
}
trap cleanup EXIT INT TERM

# Build once so the breaker timing is not distorted by repeated `go run` compiles.
go build -buildvcs=false -o "$TMP/client" ./examples/go/client
go build -buildvcs=false -o "$TMP/faulty" ./examples/go/faulty-echo-server
go build -buildvcs=false -o "$TMP/healthy" ./examples/go/echo-server
go build -buildvcs=false -o "$TMP/control-plane" ./cmd/control-plane
go build -buildvcs=false -o "$TMP/sidecar" ./cmd/sidecar

PIDS=""
"$TMP/faulty" --listen :19094 --id flaky80 --mode percentage --failure-percent 80 >"$TMP/flaky.log" 2>&1 &
FLAKY_PID=$!; PIDS="$PIDS $FLAKY_PID"
"$TMP/control-plane" --listen :17070 >"$TMP/control.log" 2>&1 & PIDS="$PIDS $!"
"$TMP/sidecar" --listen :18080 --lb round_robin --connection-pool=false --request-timeout 1s --max-attempts 1 \
  --circuit-breaker=true --circuit-window=10 --circuit-min-requests=10 --circuit-failure-rate=0.5 \
  --circuit-cooldown=2s --circuit-half-open-probes=2 --circuit-failure-codes=internal,unavailable,deadline_exceeded \
  >"$TMP/sidecar.log" 2>&1 & PIDS="$PIDS $!"
sleep 2

curl -fsS -X POST http://127.0.0.1:17070/v1/registry/register -H 'content-type: application/json' \
  -d '{"endpoint":{"service":"breaker-demo","instance_id":"flaky80","address":"127.0.0.1:19094"},"ttl_seconds":120}' >/dev/null
sleep 1

echo "[stage6] feed a deterministic 80% failure window (8 failures / 10 requests)"
failures=0
successes=0
i=1
while [ "$i" -le 10 ]; do
  if "$TMP/client" --sidecar 127.0.0.1:18080 --service breaker-demo --message "sample-$i" --timeout 2s >"$TMP/sample-$i.out" 2>&1; then
    successes=$((successes + 1))
  else
    failures=$((failures + 1))
  fi
  i=$((i + 1))
done
printf '[stage6] observed failures=%s successes=%s\n' "$failures" "$successes"
if [ "$failures" -ne 8 ] || [ "$successes" -ne 2 ]; then
  echo "unexpected deterministic 80% sample result" >&2
  exit 1
fi

echo "[stage6] circuit must now be Open and reject immediately without calling upstream"
if "$TMP/client" --sidecar 127.0.0.1:18080 --service breaker-demo --message open-check --timeout 2s >"$TMP/open.out" 2>&1; then
  echo "unexpected request success while circuit should be open" >&2
  exit 1
fi
cat "$TMP/open.out"
if ! grep -q "circuit breaker open" "$TMP/open.out"; then
  echo "request failed, but not because the circuit was open" >&2
  exit 1
fi

echo "[stage6] replace the failed backend with a healthy backend on the same endpoint"
kill "$FLAKY_PID" 2>/dev/null || true
wait "$FLAKY_PID" 2>/dev/null || true
"$TMP/healthy" --listen :19094 --id recovered >"$TMP/recovered.log" 2>&1 & PIDS="$PIDS $!"

sleep 3

echo "[stage6] cooldown elapsed: two successful Half-Open probes should close the circuit"
"$TMP/client" --sidecar 127.0.0.1:18080 --service breaker-demo --message probe-1 --timeout 2s
"$TMP/client" --sidecar 127.0.0.1:18080 --service breaker-demo --message probe-2 --timeout 2s

echo "[stage6] circuit should be Closed again"
"$TMP/client" --sidecar 127.0.0.1:18080 --service breaker-demo --message recovered --timeout 2s

echo "[stage6] PASS"
