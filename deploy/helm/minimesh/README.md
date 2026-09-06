# MiniMesh Helm Chart

```sh
helm upgrade --install minimesh deploy/helm/minimesh \
  --namespace minimesh --create-namespace --wait
helm test minimesh --namespace minimesh --logs
```

The chart deploys etcd and the MiniMesh control plane for a self-contained
demo. Each Java, Go, and Python workload Pod contains its business container
and a Go MiniMesh sidecar. The sidecar registers the colocated Pod IP through
the existing control-plane API and maintains its lease.

The bundled etcd uses `emptyDir`; it is intentionally non-durable and suitable
only for the Stage 13 demo. Use an external durable etcd cluster for production.

Stage 14 enables the allowlisted outbound transparent proxy:

```sh
helm upgrade --install minimesh deploy/helm/minimesh \
  --values deploy/helm/minimesh/values-stage14.yaml \
  --namespace minimesh --wait
```

This mode grants only `NET_ADMIN` to the short-lived Inventory init container.
The long-running sidecar is non-root and has all capabilities dropped.

Stage 15 enables sidecar-to-sidecar mTLS identity and service-level RBAC. Create
the three `minimesh-identity-*` Secrets first, then use
`values-stage15.yaml`; `make demo-stage15` automates the complete local flow,
including an unauthorized call and leaf-certificate rotation. The local CA is
for demonstration only. Pod-local and control-plane channels remain plaintext.
