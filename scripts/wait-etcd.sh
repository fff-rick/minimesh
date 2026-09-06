#!/usr/bin/env sh
set -eu

COMPOSE_FILE="deploy/docker/docker-compose.yml"
MAX_ATTEMPTS="${MAX_ATTEMPTS:-30}"
SLEEP_SECONDS="${SLEEP_SECONDS:-1}"

attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
  if docker compose -f "$COMPOSE_FILE" exec -T etcd \
    etcdctl --endpoints=http://127.0.0.1:2379 endpoint health >/dev/null 2>&1; then
    echo "etcd is healthy at http://127.0.0.1:2379"
    exit 0
  fi

  echo "waiting for etcd ($attempt/$MAX_ATTEMPTS)..."
  attempt=$((attempt + 1))
  sleep "$SLEEP_SECONDS"
done

echo "etcd did not become healthy" >&2
exit 1
