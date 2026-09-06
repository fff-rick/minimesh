# Stage 13 验收报告

## 验收结论

通过。完整 Java → Go → Python 跨语言 Demo 已部署到 Kubernetes；三个业务
Deployment 均采用“业务容器 + MiniMesh Sidecar”双容器 Pod，并通过 Helm Test。

## 验收环境

- kind v0.32.0
- Kubernetes client/server v1.36.1
- Helm v3.18.6
- 单节点本地 kind 集群 `minimesh-stage13`

## 自动化检查

```text
go test ./...                         PASS
go vet ./...                          PASS
helm lint deploy/helm/minimesh        PASS
helm upgrade --install --wait         PASS
helm test minimesh                     Succeeded
```

## 集群状态

```text
minimesh-control-plane       1/1 Ready
minimesh-etcd                1/1 Ready
minimesh-order               2/2 containers ready
minimesh-inventory           2/2 containers ready
minimesh-recommendation      2/2 containers ready
```

etcd 中存在三个由 Sidecar 维护的 Endpoint，实例 ID 为对应 Pod 名，地址为对应
Pod IP 与业务 gRPC 端口。

## 端到端结果

Helm Test Job 实际输出：

```text
order=order-book available=true recommendation=recommended-with-book
request_id=stage9-request
```

这证明请求完成了以下 Kubernetes 内调用链：

```text
Java Client → Order Service → Order Sidecar
            → Inventory Service → Inventory Sidecar
            → Recommendation Service
```

## 已知边界

- etcd 使用 `emptyDir`，Pod 重建后数据不保留；Sidecar 会重新注册并恢复 Endpoint。
- 本阶段是显式 Sidecar 多容器部署，不包含自动注入或 iptables 透明劫持；后者属于
  Stage 14。
- kind 验收证明部署与协议链路正确，不代表生产容量、跨节点网络或高可用结论。
