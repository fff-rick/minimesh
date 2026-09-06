# Stage 14 验收报告

## 结论

通过。Inventory 不再显式调用 `ProxyService`，而是直接创建标准
`RecommendationService` gRPC Client 访问 Kubernetes Service。该 TCP 连接被
Pod 内 iptables 自动送入 MiniMesh Sidecar，再由 Sidecar 恢复原目标并转发。

## 自动检查

```text
go test ./internal/transparentproxy ...         PASS
go vet ./internal/transparentproxy ...          PASS
helm lint -f values-stage14.yaml                 PASS
helm upgrade --install --wait                    PASS
helm test minimesh                               Succeeded
```

透明代理单元测试使用可注入目标解析器验证双向 TCP copy；真实 kind 验收覆盖 Linux
`SO_ORIGINAL_DST` 和 iptables，而不是只依赖 mock。

## 集群证据

Inventory init container 实际安装的关键规则：

```text
-A OUTPUT -p tcp -j MINIMESH_OUTPUT
-A MINIMESH_OUTPUT -m owner --uid-owner 1337 -j RETURN
-A MINIMESH_OUTPUT -d 127.0.0.0/8 -j RETURN
-A MINIMESH_OUTPUT -p tcp -m multiport --dports 19092 \
  -j REDIRECT --to-ports 15001
```

Inventory 启动模式：

```text
upstream=minimesh-recommendation:19092 transparent_direct=true
```

Sidecar 观测到的真实连接：

```text
transparent connection original_destination=10.96.4.185:19092
```

ClusterIP 会随集群变化；验收要点是恢复出的端口为 19092，且地址不是本地代理
监听地址。

## 端到端结果

```text
order=order-book available=true recommendation=recommended-with-book
request_id=stage9-request
```

Inventory Pod 为 `2/2 Ready`，init container 正常 Completed，业务与 Sidecar
均无重启。

## 未宣称能力

- 未实现 inbound interception、IPv6、UDP、TPROXY 或自动注入；
- 未让透明 L4 路径继承逐请求重试、熔断、限流和 L7 指标；
- 未证明 Restricted Pod Security、跨节点性能或生产高可用。
