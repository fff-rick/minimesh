# Stage 13：Kubernetes Sidecar

## 判断与边界

推荐使用 Helm 作为 Kubernetes 资源的单一事实来源，并以 kind 完成本地验收。
不同时维护一份手写 raw YAML，避免 Deployment、Service 与探针配置产生漂移。

本阶段不接入 Kubernetes API、CRD 或 Service Mesh 注入 Webhook。它们会把部署
验证扩展成新的控制面项目。服务发现仍沿用 etcd + Control Stream；每个 Sidecar
只负责注册与维护同 Pod 业务容器的 Endpoint Lease。

## Pod 模型

```text
Order Pod          Inventory Pod        Recommendation Pod
├── Java Service   ├── Go Service       ├── Python Service
└── Go Sidecar     └── Go Sidecar       └── Go Sidecar
        │                  │                     │
        └──────── Control Stream / Registry ────┘
```

同 Pod 容器通过 `127.0.0.1` 通信。Sidecar 使用 Downward API 获取 Pod 名和 Pod
IP，以 Pod 名作为唯一实例 ID，并向注册中心发布 `PodIP:业务端口`。注册失败或
Heartbeat 失败会重试；Pod 退出时执行尽力注销。

## 健康检查

- 业务容器：TCP startup/readiness/liveness probe，验证 gRPC 监听端口。
- Sidecar `/livez`：只判断进程存活，失败后允许 kubelet 重启容器。
- Sidecar `/readyz`：要求收到首个 Control Stream 快照、完成自身业务 Endpoint
  注册，并发现调用链所需的下游服务。
- Control Plane：HTTP `/healthz`。
- etcd：`etcdctl endpoint health`。

这种区分避免把 Control Plane 的短暂断连当成 Sidecar 死锁；Sidecar 已获得快照
后仍可依靠本地缓存转发。Kubernetes 官方同样区分 readiness 的摘流语义和
liveness 的重启语义：
[Liveness、Readiness 与 Startup Probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/)。

## Helm Chart

Chart 位于 `deploy/helm/minimesh`，包括：

- etcd 与 Control Plane Deployment/Service；
- Java、Go、Python 三个业务 Deployment/Service；
- 每个业务 Pod 的 MiniMesh Sidecar；
- ConfigMap、资源 requests/limits 和全部探针；
- Java → Go → Python 的 Helm Test Job；
- `values.schema.json` 基础配置校验。

Helm 将模板集中放在 `templates/`、由 `values.yaml` 提供默认值的组织方式符合
[Helm Chart 官方结构](https://helm.sh/docs/topics/charts/)。内置 etcd 使用
`emptyDir`，只服务于自包含 Demo；生产环境应替换为持久化、备份完善的外部 etcd。

## 运行

```sh
make test-stage13
make vet-stage13
make demo-stage13
make stage13-status
```

`demo-stage13` 会创建/复用 `minimesh-stage13` kind 集群、构建并加载本地镜像、
执行 `helm upgrade --install --wait`，最后运行 Helm Test Job。

保留集群用于检查；结束后显式删除：

```sh
make stage13-down
```

当镜像已由外部流程构建时，可使用：

```sh
MINIMESH_SKIP_BUILD=1 make demo-stage13
```
