#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

CLUSTER_NAME=${MINIMESH_KIND_CLUSTER:-minimesh-stage13}
HELM_IMAGE=${MINIMESH_HELM_IMAGE:-alpine/helm:3.18.6}
IMAGES="minimesh/control-plane:stage13 minimesh/sidecar:stage13 minimesh/inventory:stage13 minimesh/order:stage13 minimesh/recommendation:stage13"

if ! kind get clusters 2>/dev/null | awk -v name="$CLUSTER_NAME" '$0 == name { found=1 } END { exit !found }'; then
  kind create cluster --name "$CLUSTER_NAME" --config deploy/kubernetes/kind.yaml --wait 120s
fi

if [ "${MINIMESH_SKIP_BUILD:-0}" != "1" ]; then
  docker build -f deploy/docker/stage9-go.Dockerfile --target control-plane -t minimesh/control-plane:stage13 .
  docker build -f deploy/docker/stage9-go.Dockerfile --target sidecar -t minimesh/sidecar:stage13 .
  docker build -f deploy/docker/stage9-go.Dockerfile --target inventory -t minimesh/inventory:stage13 .
  docker build -f deploy/docker/stage9-java.Dockerfile -t minimesh/order:stage13 .
  docker build -f deploy/docker/stage9-python.Dockerfile -t minimesh/recommendation:stage13 .
else
  for image in $IMAGES; do
    docker image inspect "$image" >/dev/null
  done
fi
kind load docker-image --name "$CLUSTER_NAME" $IMAGES

if command -v helm >/dev/null 2>&1; then
  helm upgrade --install minimesh deploy/helm/minimesh --namespace minimesh --create-namespace --wait --timeout 3m
  helm test minimesh --namespace minimesh --timeout 2m
else
  DEMO_TMP=$(mktemp -d)
  trap 'rm -rf "$DEMO_TMP"' EXIT INT TERM
  kind get kubeconfig --name "$CLUSTER_NAME" >"$DEMO_TMP/kubeconfig"
  docker run --rm --network host \
    -v "$DEMO_TMP/kubeconfig:/root/.kube/config:ro" \
    -v "$ROOT:/workspace:ro" -w /workspace "$HELM_IMAGE" \
    upgrade --install minimesh deploy/helm/minimesh --namespace minimesh --create-namespace --wait --timeout 3m
  docker run --rm --network host \
    -v "$DEMO_TMP/kubeconfig:/root/.kube/config:ro" \
    -v "$ROOT:/workspace:ro" -w /workspace "$HELM_IMAGE" \
    test minimesh --namespace minimesh --timeout 2m
fi

TEST_POD=$(docker exec "$CLUSTER_NAME-control-plane" kubectl --kubeconfig=/etc/kubernetes/admin.conf \
  get pods --namespace minimesh --selector job-name=minimesh-test \
  --sort-by=.metadata.creationTimestamp --output name | tail -n 1)
docker exec "$CLUSTER_NAME-control-plane" kubectl --kubeconfig=/etc/kubernetes/admin.conf \
  logs --namespace minimesh "$TEST_POD"
echo "[stage13] Kubernetes sidecar demo passed in kind cluster $CLUSTER_NAME"
