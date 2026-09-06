#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

CLUSTER_NAME=${MINIMESH_KIND_CLUSTER:-minimesh-stage13}
HELM_IMAGE=${MINIMESH_HELM_IMAGE:-alpine/helm:3.18.6}
STAGE14_IMAGES="minimesh/sidecar:stage14 minimesh/inventory:stage14 minimesh/iptables-init:stage14"

if ! kind get clusters 2>/dev/null | awk -v name="$CLUSTER_NAME" '$0 == name { found=1 } END { exit !found }'; then
  echo "Stage 13 cluster $CLUSTER_NAME is required; run make demo-stage13 first." >&2
  exit 1
fi

if [ "${MINIMESH_SKIP_BUILD:-0}" != "1" ]; then
  docker build -f deploy/docker/stage9-go.Dockerfile --target sidecar -t minimesh/sidecar:stage14 .
  docker build -f deploy/docker/stage9-go.Dockerfile --target inventory -t minimesh/inventory:stage14 .
  docker build -f deploy/docker/stage14-init.Dockerfile -t minimesh/iptables-init:stage14 .
else
  for image in $STAGE14_IMAGES; do
    docker image inspect "$image" >/dev/null
  done
fi
kind load docker-image --name "$CLUSTER_NAME" $STAGE14_IMAGES

if command -v helm >/dev/null 2>&1; then
  helm upgrade --install minimesh deploy/helm/minimesh \
    --values deploy/helm/minimesh/values-stage14.yaml \
    --namespace minimesh --create-namespace --wait --timeout 3m
  helm test minimesh --namespace minimesh --timeout 2m
else
  DEMO_TMP=$(mktemp -d)
  trap 'rm -rf "$DEMO_TMP"' EXIT INT TERM
  kind get kubeconfig --name "$CLUSTER_NAME" >"$DEMO_TMP/kubeconfig"
  docker run --rm --network host \
    -v "$DEMO_TMP/kubeconfig:/root/.kube/config:ro" \
    -v "$ROOT:/workspace:ro" -w /workspace "$HELM_IMAGE" \
    upgrade --install minimesh deploy/helm/minimesh \
    --values deploy/helm/minimesh/values-stage14.yaml \
    --namespace minimesh --create-namespace --wait --timeout 3m
  docker run --rm --network host \
    -v "$DEMO_TMP/kubeconfig:/root/.kube/config:ro" \
    -v "$ROOT:/workspace:ro" -w /workspace "$HELM_IMAGE" \
    test minimesh --namespace minimesh --timeout 2m
fi

KUBECTL="docker exec $CLUSTER_NAME-control-plane kubectl --kubeconfig=/etc/kubernetes/admin.conf"
INVENTORY_POD=$($KUBECTL get pods --namespace minimesh \
  --selector app.kubernetes.io/component=inventory --output jsonpath='{.items[0].metadata.name}')
TEST_POD=$($KUBECTL get pods --namespace minimesh --selector job-name=minimesh-test \
  --sort-by=.metadata.creationTimestamp --output name | tail -n 1)
$KUBECTL logs --namespace minimesh "$TEST_POD"
$KUBECTL logs --namespace minimesh "$INVENTORY_POD" --container transparent-init
TRANSPARENT_LOGS=$($KUBECTL logs --namespace minimesh "$INVENTORY_POD" --container sidecar)
printf '%s\n' "$TRANSPARENT_LOGS" | grep 'transparent connection'

echo "[stage14] outbound request reached the sidecar through iptables REDIRECT and SO_ORIGINAL_DST"
