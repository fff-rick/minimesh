#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
COMPOSE="docker compose -f deploy/docker/docker-compose.yml --profile stage9"
CLIENT_COMPOSE="docker compose -f deploy/docker/docker-compose.yml --profile stage9 --profile stage9-client"
cleanup() { $COMPOSE down --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

$COMPOSE up --build -d

# Stage 8's control plane remains the source of truth. Register the typed
# business servers, then let each sidecar receive the snapshot on its stream.
for attempt in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:37070/healthz >/dev/null 2>&1; then break; fi
  sleep 1
done
register() {
  curl -fsS -X POST http://127.0.0.1:37070/v1/registry/register -H 'content-type: application/json' -d "$1" >/dev/null
}
register '{"endpoint":{"service":"inventory","instance_id":"stage9-inventory","address":"stage9-inventory:19091"},"ttl_seconds":120}'
register '{"endpoint":{"service":"recommendation","instance_id":"stage9-recommendation","address":"stage9-recommendation:19092"},"ttl_seconds":120}'
sleep 3

echo '[stage9] Java -> Sidecar -> Go -> Sidecar -> Python'
$CLIENT_COMPOSE run --rm stage9-order-client

echo '[stage9] Python NOT_FOUND remains a gRPC NOT_FOUND at the Java caller'
if $CLIENT_COMPOSE run --rm -e ORDER_SKU=missing stage9-order-client; then
  echo 'expected missing recommendation to fail' >&2
  exit 1
fi

echo '[stage9] Java deadline constrains the full two-sidecar call'
if $CLIENT_COMPOSE run --rm -e ORDER_SKU=slow -e ORDER_TIMEOUT_MS=20 stage9-order-client; then
  echo 'expected slow recommendation to exceed deadline' >&2
  exit 1
fi
