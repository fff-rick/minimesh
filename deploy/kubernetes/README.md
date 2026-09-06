# Stage 13 Kubernetes deployment

The deployable resources live in `deploy/helm/minimesh`. Helm is the single
source of truth so raw YAML and chart templates cannot drift apart. This
directory contains the local kind cluster configuration used by the demo.

```sh
make demo-stage13
make stage13-status
make stage13-down
```

`demo-stage13` builds the five local images, loads them into kind, installs the
chart, waits for all workloads, and runs the Java → Go → Python Helm test.
