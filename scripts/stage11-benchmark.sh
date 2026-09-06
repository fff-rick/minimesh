#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

REQUESTS=${REQUESTS:-20000}
CONCURRENCY=${CONCURRENCY:-100}
CONNECTIONS=${CONNECTIONS:-4}
PROFILE_SECONDS=${PROFILE_SECONDS:-10}
GHZ_VERSION=${GHZ_VERSION:-v0.121.0}
OUTPUT_DIR=${OUTPUT_DIR:-artifacts/stage11}
BACKEND_PORT=${BACKEND_PORT:-29090}
SIDECAR_PORT=${SIDECAR_PORT:-28080}
METRICS_PORT=${METRICS_PORT:-28081}
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/minimesh-stage11.XXXXXX")
PIDS=""

cleanup() {
  if [ -n "$PIDS" ]; then
    kill $PIDS >/dev/null 2>&1 || true
    wait $PIDS 2>/dev/null || true
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT INT TERM

for tool in go jq curl awk getconf; do
  command -v "$tool" >/dev/null 2>&1 || { echo "stage11: missing required tool: $tool" >&2; exit 2; }
done

GHZ=${GHZ:-}
if [ -z "$GHZ" ]; then
  if command -v ghz >/dev/null 2>&1; then
    GHZ=$(command -v ghz)
  else
    echo "[stage11] installing ghz $GHZ_VERSION into temporary workspace"
    GOBIN="$WORK_DIR/bin" go install "github.com/bojand/ghz/cmd/ghz@$GHZ_VERSION"
    GHZ="$WORK_DIR/bin/ghz"
  fi
fi

mkdir -p "$OUTPUT_DIR/profiles"
go build -buildvcs=false -o "$WORK_DIR/echo-server" ./examples/go/echo-server
go build -buildvcs=false -o "$WORK_DIR/sidecar" ./cmd/sidecar

clock_ticks() { awk '{print $14+$15}' "/proc/$1/stat"; }
rss_kib() { awk '/VmRSS:/ {print $2}' "/proc/$1/status"; }
metric() { curl -fsS "http://127.0.0.1:$METRICS_PORT/metrics" | awk -v key="$1" '$1 == key {print $2; exit}'; }
wait_port() {
  address=$1
  for attempt in $(seq 1 100); do
    if curl -fsS "$address" >/dev/null 2>&1; then return 0; fi
    sleep 0.05
  done
  return 1
}

start_backend() {
  "$WORK_DIR/echo-server" --listen ":$BACKEND_PORT" --id stage11 >"$WORK_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  PIDS="$PIDS $BACKEND_PID"
  sleep 0.3
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    cat "$WORK_DIR/backend.log" >&2
    return 1
  fi
}

stop_pid() {
  target_pid=$1
  kill -TERM "$target_pid" >/dev/null 2>&1 || true
  wait "$target_pid" 2>/dev/null || true
  PIDS=$(printf '%s' "$PIDS" | sed "s/ $target_pid//")
}

run_direct() {
  start_backend
  backend_before=$(clock_ticks "$BACKEND_PID")
  "$GHZ" --insecure --proto api/proto/minimesh/v1/proxy.proto \
    --call minimesh.v1.EchoService.Echo --data '{"data":"aGVsbG8="}' \
    --total "$REQUESTS" --concurrency "$CONCURRENCY" --connections "$CONNECTIONS" \
    --skipFirst "$CONCURRENCY" --format json --output "$OUTPUT_DIR/direct.json" "127.0.0.1:$BACKEND_PORT"
  backend_after=$(clock_ticks "$BACKEND_PID")
  direct_rss=$(rss_kib "$BACKEND_PID")
  ticks=$((backend_after-backend_before))
  direct_cpu=$(awk -v ticks="$ticks" -v hz="$(getconf CLK_TCK)" 'BEGIN {printf "%.3f", ticks/hz}')
  printf '{"backend_cpu_seconds":%s,"backend_rss_kib":%s}\n' "$direct_cpu" "$direct_rss" >"$OUTPUT_DIR/direct-resources.json"
  stop_pid "$BACKEND_PID"
}

run_mesh() {
  start_backend
  "$WORK_DIR/sidecar" --listen ":$SIDECAR_PORT" --metrics-listen ":$METRICS_PORT" --pprof \
    --control-plane 127.0.0.1:1 --max-attempts 1 --circuit-breaker=false --rate-limit=false \
    --otel-endpoint '' >"$WORK_DIR/sidecar.log" 2>&1 &
  SIDECAR_PID=$!
  PIDS="$PIDS $SIDECAR_PID"
  wait_port "http://127.0.0.1:$METRICS_PORT/metrics"

  backend_before=$(clock_ticks "$BACKEND_PID")
  sidecar_before=$(clock_ticks "$SIDECAR_PID")
  gc_before=$(metric go_gc_duration_seconds_count)
  "$GHZ" --insecure --proto api/proto/minimesh/v1/proxy.proto \
    --call minimesh.v1.ProxyService.Invoke \
    --data '{"target":"127.0.0.1:'"$BACKEND_PORT"'","fullMethod":"/minimesh.v1.EchoService/Echo","payload":{"data":"aGVsbG8="}}' \
    --total "$REQUESTS" --concurrency "$CONCURRENCY" --connections "$CONNECTIONS" \
    --skipFirst "$CONCURRENCY" --format json --output "$OUTPUT_DIR/mesh.json" "127.0.0.1:$SIDECAR_PORT"
  backend_after=$(clock_ticks "$BACKEND_PID")
  sidecar_after=$(clock_ticks "$SIDECAR_PID")
  gc_after=$(metric go_gc_duration_seconds_count)
  goroutines=$(metric go_goroutines)
  active_connections=$(metric minimesh_active_connections)
  idle_connections=$(metric minimesh_idle_connections)
  connections_created=$(metric minimesh_connection_created_total)
  sidecar_rss=$(rss_kib "$SIDECAR_PID")
  backend_rss=$(rss_kib "$BACKEND_PID")
  hz=$(getconf CLK_TCK)
  backend_cpu=$(awk -v ticks="$((backend_after-backend_before))" -v hz="$hz" 'BEGIN {printf "%.3f", ticks/hz}')
  sidecar_cpu=$(awk -v ticks="$((sidecar_after-sidecar_before))" -v hz="$hz" 'BEGIN {printf "%.3f", ticks/hz}')
  gc_delta=$(awk -v before="$gc_before" -v after="$gc_after" 'BEGIN {printf "%.0f", after-before}')
  printf '{"backend_cpu_seconds":%s,"sidecar_cpu_seconds":%s,"backend_rss_kib":%s,"sidecar_rss_kib":%s,"goroutines":%s,"gc_cycles":%s,"active_connections":%s,"idle_connections":%s,"connections_created":%s}\n' \
    "$backend_cpu" "$sidecar_cpu" "$backend_rss" "$sidecar_rss" "$goroutines" "$gc_delta" "$active_connections" "$idle_connections" "$connections_created" >"$OUTPUT_DIR/mesh-resources.json"

  curl -fsS "http://127.0.0.1:$METRICS_PORT/debug/pprof/profile?seconds=$PROFILE_SECONDS" -o "$OUTPUT_DIR/profiles/sidecar-cpu.pprof" &
  PROFILE_PID=$!
  PIDS="$PIDS $PROFILE_PID"
  "$GHZ" --insecure --proto api/proto/minimesh/v1/proxy.proto \
    --call minimesh.v1.ProxyService.Invoke \
    --data '{"target":"127.0.0.1:'"$BACKEND_PORT"'","fullMethod":"/minimesh.v1.EchoService/Echo","payload":{"data":"aGVsbG8="}}' \
    --duration "${PROFILE_SECONDS}s" --duration-stop wait --concurrency "$CONCURRENCY" --connections "$CONNECTIONS" \
    --skipFirst "$CONCURRENCY" --format json --output "$OUTPUT_DIR/profile-load.json" "127.0.0.1:$SIDECAR_PORT"
  wait "$PROFILE_PID"
  PIDS=$(printf '%s' "$PIDS" | sed "s/ $PROFILE_PID//")

  curl -fsS "http://127.0.0.1:$METRICS_PORT/debug/pprof/heap" -o "$OUTPUT_DIR/profiles/sidecar-heap.pprof"
  curl -fsS "http://127.0.0.1:$METRICS_PORT/debug/pprof/goroutine?debug=1" -o "$OUTPUT_DIR/profiles/sidecar-goroutines.txt"
  go tool pprof -top "$WORK_DIR/sidecar" "$OUTPUT_DIR/profiles/sidecar-cpu.pprof" >"$OUTPUT_DIR/profiles/sidecar-cpu-top.txt"
  go tool pprof -top -alloc_space "$WORK_DIR/sidecar" "$OUTPUT_DIR/profiles/sidecar-heap.pprof" >"$OUTPUT_DIR/profiles/sidecar-heap-top.txt"
  stop_pid "$SIDECAR_PID"
  stop_pid "$BACKEND_PID"
}

latency_ms() {
  jq -r --argjson percentile "$2" '[.latencyDistribution[] | select(.percentage == $percentile)][0].latency / 1000000' "$1"
}

write_report() {
  direct_qps=$(jq -r '.rps' "$OUTPUT_DIR/direct.json")
  mesh_qps=$(jq -r '.rps' "$OUTPUT_DIR/mesh.json")
  qps_delta=$(awk -v direct="$direct_qps" -v mesh="$mesh_qps" 'BEGIN {printf "%.2f", (mesh/direct-1)*100}')
  p50_delta=$(awk -v direct="$(latency_ms "$OUTPUT_DIR/direct.json" 50)" -v mesh="$(latency_ms "$OUTPUT_DIR/mesh.json" 50)" 'BEGIN {printf "%.3f", mesh-direct}')
  p95_delta=$(awk -v direct="$(latency_ms "$OUTPUT_DIR/direct.json" 95)" -v mesh="$(latency_ms "$OUTPUT_DIR/mesh.json" 95)" 'BEGIN {printf "%.3f", mesh-direct}')
  p99_delta=$(awk -v direct="$(latency_ms "$OUTPUT_DIR/direct.json" 99)" -v mesh="$(latency_ms "$OUTPUT_DIR/mesh.json" 99)" 'BEGIN {printf "%.3f", mesh-direct}')
  generated=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  os=$(uname -srmo)
  go_version=$(go version)
  ghz_reported_version=$($GHZ --version 2>&1 | head -1)
  ghz_version="$GHZ_VERSION ($ghz_reported_version)"
  direct_cpu=$(jq -r '.backend_cpu_seconds' "$OUTPUT_DIR/direct-resources.json")
  mesh_backend_cpu=$(jq -r '.backend_cpu_seconds' "$OUTPUT_DIR/mesh-resources.json")
  sidecar_cpu=$(jq -r '.sidecar_cpu_seconds' "$OUTPUT_DIR/mesh-resources.json")
  direct_rss=$(jq -r '.backend_rss_kib' "$OUTPUT_DIR/direct-resources.json")
  mesh_backend_rss=$(jq -r '.backend_rss_kib' "$OUTPUT_DIR/mesh-resources.json")
  sidecar_rss=$(jq -r '.sidecar_rss_kib' "$OUTPUT_DIR/mesh-resources.json")
  cat >"$OUTPUT_DIR/REPORT.md" <<EOF
# MiniMesh Stage 11 Benchmark Report

Generated: $generated

## Method

- Host: $os; CPUs: $(nproc); $go_version; $ghz_version.
- Workload: $REQUESTS requests, concurrency $CONCURRENCY, client connections $CONNECTIONS, first $CONCURRENCY samples excluded as warm-up.
- Direct: \`ghz -> EchoService\`; Mesh: \`ghz -> ProxyService -> pooled connection -> EchoService\`.
- Telemetry export, retry, circuit breaker and rate limiting were disabled to isolate steady-state proxy overhead.
- CPU is process CPU consumed during each scored run; RSS is sampled after that run. Profiles use a separate sustained Mesh load for the full $PROFILE_SECONDS-second window, so profiling overhead does not contaminate the scored comparison.

## Results

| Path | QPS | P50 ms | P95 ms | P99 ms | Backend CPU s | Sidecar CPU s | Backend RSS KiB | Sidecar RSS KiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Direct | $(printf '%.2f' "$direct_qps") | $(latency_ms "$OUTPUT_DIR/direct.json" 50) | $(latency_ms "$OUTPUT_DIR/direct.json" 95) | $(latency_ms "$OUTPUT_DIR/direct.json" 99) | $direct_cpu | — | $direct_rss | — |
| Mesh | $(printf '%.2f' "$mesh_qps") | $(latency_ms "$OUTPUT_DIR/mesh.json" 50) | $(latency_ms "$OUTPUT_DIR/mesh.json" 95) | $(latency_ms "$OUTPUT_DIR/mesh.json" 99) | $mesh_backend_cpu | $sidecar_cpu | $mesh_backend_rss | $sidecar_rss |

Mesh delta: QPS **$qps_delta%**, P50 **+$p50_delta ms**, P95 **+$p95_delta ms**, P99 **+$p99_delta ms**.

Runtime snapshot: $(jq -r '"goroutines=\(.goroutines), GC cycles=\(.gc_cycles), connections active/idle/created=\(.active_connections)/\(.idle_connections)/\(.connections_created)"' "$OUTPUT_DIR/mesh-resources.json").

## Profile evidence

- CPU: [profiles/sidecar-cpu-top.txt](profiles/sidecar-cpu-top.txt)
- Allocation: [profiles/sidecar-heap-top.txt](profiles/sidecar-heap-top.txt)
- Goroutines: [profiles/sidecar-goroutines.txt](profiles/sidecar-goroutines.txt)
- Raw ghz results: [direct.json](direct.json), [mesh.json](mesh.json), [profile-load.json](profile-load.json)

## Interpretation guardrails

This is a local single-host microbenchmark, not a production capacity claim. Repeat at least three times on an idle, CPU-pinned host before using the deltas as a regression threshold. Do not add buffer pools or \`sync.Pool\` unless the allocation profile identifies a stable application-level hotspot; transport/runtime costs should not be optimized speculatively.
EOF
}

echo "[stage11] direct baseline"
run_direct
echo "[stage11] mesh path with pprof"
run_mesh
write_report
echo "[stage11] report: $OUTPUT_DIR/REPORT.md"
