#!/bin/sh
set -u

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
COMPOSE="docker compose -f deploy/docker/docker-compose.yml -f deploy/docker/docker-compose.stage10.yml -f deploy/docker/docker-compose.stage16.yml --profile stage9 --profile stage16"
CLIENT_COMPOSE="$COMPOSE --profile stage9-client --profile stage16-client"
ARTIFACT_DIR=${STAGE16_ARTIFACT_DIR:-artifacts/stage16}
PUMBA_IMAGE=ghcr.io/alexei-led/pumba:1.1.7@sha256:4458cb4f55b4a29ecaf842b23834318c5f29027a026d7762eb4ec491003b16e8
NETTOOLS_IMAGE=ghcr.io/alexei-led/pumba-alpine-nettools:latest@sha256:bdcfacdd0c42f64cd280319d6dd5d3911f33425ce4a5168bb1b7de9570b72cd1
RESULTS=$(mktemp)
FAILED=0
mkdir -p "$ARTIFACT_DIR"

cleanup() {
  if [ "${KEEP_STAGE16:-0}" != "1" ]; then
    $COMPOSE down --remove-orphans >/dev/null 2>&1 || true
  fi
  rm -f "$RESULTS"
}
trap cleanup EXIT INT TERM

compact() {
  printf '%s' "$1" | tr '\n\t|' '   /' | cut -c1-240
}

record() {
  printf '%s\t%s\t%s\n' "$1" "$2" "$(compact "$3")" >>"$RESULTS"
  printf '[stage16] %-28s %s\n' "$1" "$2"
  [ "$2" = PASS ] || FAILED=1
}

run_case() {
  case_name=$1
  shift
  case_output=$($@ 2>&1)
  case_status=$?
  if [ "$case_status" -eq 0 ]; then
    record "$case_name" PASS "$case_output"
  else
    record "$case_name" FAIL "$case_output"
  fi
  return 0
}

wait_http() {
  endpoint=$1
  attempts=${2:-60}
  count=0
  until curl -fsS "$endpoint" >/dev/null 2>&1; do
    count=$((count + 1))
    [ "$count" -lt "$attempts" ] || return 1
    sleep 1
  done
}

register() {
  service=$1 instance=$2 address=$3
  curl -fsS -X POST http://127.0.0.1:37070/v1/registry/register \
    -H 'content-type: application/json' \
    -d '{"endpoint":{"service":"'"$service"'","instance_id":"'"$instance"'","address":"'"$address"'"},"ttl_seconds":600}' >/dev/null
}

probe() {
  $CLIENT_COMPOSE run --rm --no-deps stage16-probe --sidecar=stage16-sidecar-chaos:18080 "$@"
}

business_call() {
  $CLIENT_COMPOSE run --rm --no-deps stage9-order-client
}

case_kill_backend() {
  $COMPOSE stop stage16-echo-a >/dev/null
  status=0
  result=$(probe --service=chaos --requests=20 --concurrency=1 --min-success=15) || status=$?
  $COMPOSE start stage16-echo-a >/dev/null || return 1
  [ "$status" -eq 0 ] || return "$status"
  printf '%s' "$result"
}

case_kill_sidecar() {
  $COMPOSE stop stage16-sidecar-chaos >/dev/null
  status=0
  failed=$(probe --service=chaos --requests=1 --min-errors=1 --expect-code=Unavailable) || status=$?
  $COMPOSE start stage16-sidecar-chaos >/dev/null || return 1
  wait_http http://127.0.0.1:38084/readyz 30 || return 1
  [ "$status" -eq 0 ] || return "$status"
  recovered=$(probe --service=chaos --requests=2 --min-success=2) || return 1
  printf 'during=%s recovery=%s' "$failed" "$recovered"
}

case_kill_control_plane() {
  $COMPOSE stop stage9-control-plane >/dev/null
  status=0
  cached=$(probe --service=chaos --requests=4 --min-success=4) || status=$?
  $COMPOSE start stage9-control-plane >/dev/null || return 1
  wait_http http://127.0.0.1:37070/healthz 30 || return 1
  sleep 3
  [ "$status" -eq 0 ] || return "$status"
  recovered=$(probe --service=chaos --requests=2 --min-success=2) || return 1
  printf 'cached=%s recovery=%s' "$cached" "$recovered"
}

case_etcd_outage() {
  curl -fsS -X POST http://127.0.0.1:38474/proxies/minimesh_stage16_etcd \
    -H 'content-type: application/json' -d '{"enabled":false}' >/dev/null
  status=0
  cached=$(probe --service=chaos --requests=4 --min-success=4) || status=$?
  curl -fsS -X POST http://127.0.0.1:38474/proxies/minimesh_stage16_etcd \
    -H 'content-type: application/json' -d '{"enabled":true}' >/dev/null || return 1
  [ "$status" -eq 0 ] || return "$status"
  sleep 2
  register recovery recovery-a stage16-echo-b:19090 || return 1
  sleep 3
  recovered=$(probe --service=recovery --requests=2 --min-success=2) || return 1
  printf 'cached=%s config_recovery=%s' "$cached" "$recovered"
}

case_network_delay() {
  curl -fsS -X POST http://127.0.0.1:38475/proxies/minimesh_stage16_backend/toxics \
    -H 'content-type: application/json' \
    -d '{"name":"stage16_latency","type":"latency","stream":"downstream","toxicity":1,"attributes":{"latency":1000,"jitter":0}}' >/dev/null || return 1
  status=0
  delayed=$(probe --service=network --requests=2 --timeout=2s --min-errors=1 --expect-code=DeadlineExceeded) || status=$?
  curl -fsS -X DELETE http://127.0.0.1:38475/proxies/minimesh_stage16_backend/toxics/stage16_latency >/dev/null || return 1
  sleep 2
  [ "$status" -eq 0 ] || return "$status"
  recovered=$(probe --service=network --requests=2 --min-success=2) || return 1
  printf 'delay=%s recovery=%s' "$delayed" "$recovered"
}

case_packet_loss() {
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
    "$PUMBA_IMAGE" --log-level info netem \
    --tc-image "$NETTOOLS_IMAGE" \
    --duration 30s loss --percent 100 minimesh-stage16-network >"$ARTIFACT_DIR/pumba.log" 2>&1 &
  pumba_pid=$!
  # Pumba first creates a short-lived privileged nettools sidecar. Synchronize
  # on its own activation log instead of assuming image/setup latency.
  activated=false
  for attempt in $(seq 1 30); do
    if grep -q 'running netem on container' "$ARTIFACT_DIR/pumba.log"; then
      activated=true
      break
    fi
    if ! kill -0 "$pumba_pid" 2>/dev/null; then
      wait "$pumba_pid"
      return $?
    fi
    sleep 1
  done
  [ "$activated" = true ] || return 1
  # The activation log precedes the helper container applying qdisc by a small,
  # runtime-dependent interval. Prove the impairment itself rather than using
  # another timing assumption.
  impaired=false
  lost=''
  for attempt in $(seq 1 15); do
    if candidate=$(probe --service=network --requests=1 --timeout=2s --min-errors=1 2>&1); then
      impaired=true
      lost="attempt=$attempt $candidate"
      break
    fi
    sleep 1
  done
  wait "$pumba_pid" || return 1
  sleep 2
  [ "$impaired" = true ] || return 1
  recovered=$(probe --service=network --requests=2 --min-success=2) || return 1
  printf 'loss=%s recovery=%s' "$lost" "$recovered"
}

case_backend_80_percent() {
  # Reset the deterministic 1..100 request sequence so the scenario remains
  # repeatable after KEEP_STAGE16 runs and local debugging.
  $COMPOSE restart stage16-echo-faulty >/dev/null || return 1
  sleep 1
  faulty=$(probe --service=faulty80 --requests=12 --concurrency=1 --min-errors=1) || return 1
  retry_metric=$(curl -fsS http://127.0.0.1:38084/metrics | awk '/^minimesh_retry_total\{.*service="faulty80"/ {sum += $NF} END {print sum+0}')
  breaker_metric=$(curl -fsS http://127.0.0.1:38084/metrics | awk '/^minimesh_circuit_breaker_total\{.*service="faulty80"/ {sum += $NF} END {print sum+0}')
  [ "$retry_metric" -gt 0 ] && [ "$breaker_metric" -gt 0 ] || return 1
  printf '%s retries=%s breaker_rejections=%s' "$faulty" "$retry_metric" "$breaker_metric"
}

case_slow_backend() {
  probe --service=slow --requests=2 --timeout=2s --min-errors=2 --expect-code=DeadlineExceeded
}

case_rate_limit() {
  limited=$(probe --service=limited --requests=20 --concurrency=20 --min-errors=1 --expect-code=ResourceExhausted) || return 1
  rejected=$(curl -fsS http://127.0.0.1:38084/metrics | awk '/^minimesh_rate_limit_rejected_total\{.*service="limited"/ {sum += $NF} END {print sum+0}')
  [ "$rejected" -gt 0 ] || return 1
  printf '%s rejected_metric=%s' "$limited" "$rejected"
}

case_observability() {
  sleep 7
  curl -fsS 'http://127.0.0.1:9090/api/v1/query?query=minimesh_request_total' | grep -q '"status":"success"' || return 1
  trace=$(curl -fsS 'http://127.0.0.1:16686/api/traces?service=sidecar-chaos&limit=20') || return 1
  printf '%s' "$trace" | grep -q 'sidecar-chaos' || return 1
  curl -fsS 'http://127.0.0.1:3000/api/dashboards/uid/minimesh-stage10' | grep -q 'MiniMesh Stage 10 Observability' || return 1
  curl -fsS http://127.0.0.1:39100/ | grep -q '"healthy":true' || return 1
  printf 'Prometheus query, Jaeger trace, Grafana dashboard, and Rust validation agent are healthy'
}

write_report() {
  report="$ARTIFACT_DIR/FAILURE_TEST_REPORT.md"
  {
    echo '# MiniMesh Stage 16 Failure Test Report'
    echo
    echo "Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    echo
    echo '| Scenario | Result | Evidence |'
    echo '| --- | --- | --- |'
    while IFS="$(printf '\t')" read -r name status evidence; do
      printf '| %s | %s | %s |\n' "$name" "$status" "$evidence"
    done <"$RESULTS"
    echo
    echo 'Runtime details are available through `docker compose logs` when `KEEP_STAGE16=1`.'
  } >"$report"
  echo "[stage16] report: $report"
}

echo '[stage16] building and starting the final validation topology'
if [ "${MINIMESH_SKIP_BUILD:-0}" != "1" ]; then
  docker build -f deploy/docker/stage9-go.Dockerfile --target stage16-go -t minimesh-stage16-go:latest . || exit 1
  # The Java Dockerfile uses a BuildKit cache mount. Compose provides BuildKit
  # where the standalone docker CLI may not have buildx installed.
  $COMPOSE build stage9-order || exit 1
  docker build -f deploy/docker/stage9-python.Dockerfile -t minimesh-stage16-python:latest . || exit 1
  docker build -f deploy/docker/stage16-rust.Dockerfile -t minimesh-stage16-rust-agent:latest . || exit 1
fi
docker pull "$PUMBA_IMAGE" || exit 1
docker pull "$NETTOOLS_IMAGE" || exit 1
$COMPOSE up --quiet-pull --no-build --force-recreate -d || exit 1
wait_http http://127.0.0.1:37070/healthz 90 || exit 1
wait_http http://127.0.0.1:38084/readyz 90 || exit 1
wait_http http://127.0.0.1:9090/-/ready 90 || exit 1

register inventory stage16-inventory stage9-inventory:19091 || exit 1
register recommendation stage16-recommendation stage9-recommendation:19092 || exit 1
register chaos healthy-a stage16-echo-a:19090 || exit 1
register chaos healthy-b stage16-echo-b:19090 || exit 1
register faulty80 faulty-80 stage16-echo-faulty:19090 || exit 1
register slow slow-a stage16-echo-slow:19090 || exit 1
register network network-a stage16-toxiproxy-network:29090 || exit 1
register limited limited-a stage16-echo-b:19090 || exit 1
sleep 4

run_case 'Final polyglot baseline' business_call
run_case 'Kill Backend / LB / Retry' case_kill_backend
run_case 'Kill Sidecar / recovery' case_kill_sidecar
run_case 'Kill Control Plane / cache' case_kill_control_plane
run_case 'etcd outage / config recovery' case_etcd_outage
run_case 'Network delay / timeout' case_network_delay
run_case 'Packet loss / recovery' case_packet_loss
run_case 'Backend 80% / circuit breaker' case_backend_80_percent
run_case 'Backend slow response' case_slow_backend
run_case 'Rate limit' case_rate_limit
run_case 'Trace / Metrics / Rust agent' case_observability
write_report

if [ "$FAILED" -ne 0 ]; then
  echo '[stage16] one or more scenarios failed' >&2
  exit 1
fi
echo '[stage16] all final validation scenarios passed'
echo '[stage16] Grafana http://localhost:3000/d/minimesh-stage10  Jaeger http://localhost:16686'
