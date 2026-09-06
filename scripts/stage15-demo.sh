#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

CLUSTER_NAME=${MINIMESH_KIND_CLUSTER:-minimesh-stage13}
HELM_IMAGE=${MINIMESH_HELM_IMAGE:-alpine/helm:3.18.6}
NODE="$CLUSTER_NAME-control-plane"
STAGE15_IMAGES="minimesh/sidecar:stage15 minimesh/inventory:stage15 minimesh/client:stage15"
CERT_DIR=$(mktemp -d)
HELM_TMP=$(mktemp -d)
trap 'rm -rf "$CERT_DIR" "$HELM_TMP"' EXIT INT TERM

if ! kind get clusters 2>/dev/null | awk -v name="$CLUSTER_NAME" '$0 == name { found=1 } END { exit !found }'; then
  echo "Stage 13 cluster $CLUSTER_NAME is required; run make demo-stage13 first." >&2
  exit 1
fi

kctl() {
  docker exec "$NODE" kubectl --kubeconfig=/etc/kubernetes/admin.conf "$@"
}

apply_identity_secret() {
  service=$1
  source_dir=$2
  # kind mounts /tmp as tmpfs; docker cp cannot reliably extract into that
  # mount, so use the node container's persistent writable layer.
  node_dir="/var/local/minimesh-stage15-$service"
  docker exec "$NODE" mkdir -p "$node_dir"
  docker cp "$source_dir/tls.crt" "$NODE:$node_dir/tls.crt"
  docker cp "$source_dir/tls.key" "$NODE:$node_dir/tls.key"
  docker cp "$source_dir/ca.crt" "$NODE:$node_dir/ca.crt"
  docker exec "$NODE" sh -ec "
    kubectl --kubeconfig=/etc/kubernetes/admin.conf -n minimesh create secret generic minimesh-identity-$service \
      --from-file=tls.crt=$node_dir/tls.crt --from-file=tls.key=$node_dir/tls.key --from-file=ca.crt=$node_dir/ca.crt \
      --dry-run=client -o yaml | kubectl --kubeconfig=/etc/kubernetes/admin.conf apply -f -
  "
}

helm_cmd() {
  if command -v helm >/dev/null 2>&1; then
    helm "$@"
  else
    if [ ! -s "$HELM_TMP/kubeconfig" ]; then
      kind get kubeconfig --name "$CLUSTER_NAME" >"$HELM_TMP/kubeconfig"
    fi
    docker run --rm --network host \
      -v "$HELM_TMP/kubeconfig:/root/.kube/config:ro" \
      -v "$ROOT:/workspace:ro" -w /workspace "$HELM_IMAGE" "$@"
  fi
}

if [ "${MINIMESH_SKIP_BUILD:-0}" != "1" ]; then
  docker build -f deploy/docker/stage9-go.Dockerfile --target sidecar -t minimesh/sidecar:stage15 .
  docker build -f deploy/docker/stage9-go.Dockerfile --target inventory -t minimesh/inventory:stage15 .
  docker build -f deploy/docker/stage9-go.Dockerfile --target client -t minimesh/client:stage15 .
else
  for image in $STAGE15_IMAGES; do
    docker image inspect "$image" >/dev/null
  done
fi
kind load docker-image --name "$CLUSTER_NAME" $STAGE15_IMAGES

scripts/stage15-certs.sh "$CERT_DIR" 1000
trust_revision=$(sha256sum "$CERT_DIR/ca.crt" | awk '{print $1}')
for service in order inventory recommendation; do
  apply_identity_secret "$service" "$CERT_DIR/$service"
done

helm_cmd upgrade --install minimesh deploy/helm/minimesh \
  --values deploy/helm/minimesh/values-stage15.yaml \
  --set-string security.revision="$trust_revision" \
  --namespace minimesh --create-namespace --wait --timeout 3m
helm_cmd test minimesh --namespace minimesh --timeout 2m

# Rotate only the Order leaf certificate. The CA stays unchanged. Secret
# projection plus TLS callbacks must pick up serial 2000 without a Pod restart.
scripts/stage15-certs.sh "$CERT_DIR" 2000 order
apply_identity_secret order "$CERT_DIR/order"
expected_hash=$(sha256sum "$CERT_DIR/order/tls.crt" | awk '{print $1}')
order_pod=$(kctl get pod -n minimesh -l app.kubernetes.io/component=order -o jsonpath='{.items[0].metadata.name}')
updated=false
for attempt in $(seq 1 60); do
  mounted_hash=$(kctl exec -n minimesh "$order_pod" -c sidecar -- sha256sum /var/run/minimesh/tls/tls.crt 2>/dev/null | awk '{print $1}')
  if [ "$mounted_hash" = "$expected_hash" ]; then
    updated=true
    break
  fi
  sleep 2
done
if [ "$updated" != "true" ]; then
  echo "rotated Order certificate was not projected into the running Pod" >&2
  exit 1
fi

sleep 4
helm_cmd test minimesh --namespace minimesh --timeout 2m

inventory_pod=$(kctl get pod -n minimesh -l app.kubernetes.io/component=inventory -o jsonpath='{.items[0].metadata.name}')
recommendation_pod=$(kctl get pod -n minimesh -l app.kubernetes.io/component=recommendation -o jsonpath='{.items[0].metadata.name}')
inventory_logs=$(kctl logs -n minimesh "$inventory_pod" -c sidecar)
recommendation_logs=$(kctl logs -n minimesh "$recommendation_pod" -c sidecar)
printf '%s\n' "$inventory_logs" | grep 'peer_identity=spiffe://minimesh.local/ns/minimesh/sa/order' | grep 'peer_serial=1000'
printf '%s\n' "$inventory_logs" | grep 'peer_identity=spiffe://minimesh.local/ns/minimesh/sa/order' | grep 'peer_serial=2000'
printf '%s\n' "$recommendation_logs" | grep 'RBAC denied peer identity.*sa/order'

authorized_test=$(kctl get pods -n minimesh -l job-name=minimesh-test --sort-by=.metadata.creationTimestamp -o name | tail -n 1)
unauthorized_test=$(kctl get pods -n minimesh -l job-name=minimesh-unauthorized-test --sort-by=.metadata.creationTimestamp -o name | tail -n 1)
kctl logs -n minimesh "$authorized_test"
kctl logs -n minimesh "$unauthorized_test"
kctl get pods -n minimesh -l 'app.kubernetes.io/component in (order,inventory,recommendation)'
echo "[stage15] mTLS identity, RBAC denial, and leaf certificate rotation passed"
