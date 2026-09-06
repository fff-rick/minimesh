# Stage 15 验收报告

## 结论

通过。授权链路 Java Order → Go Inventory → Python Recommendation 在两轮 Helm
测试中成功；Order 绕过 Inventory 直接访问 Recommendation 时，在目标 Sidecar 的
TLS 握手阶段被 RBAC 拒绝。Order 叶证书原地轮换后，Pod 未重启且新连接使用了新
序列号。

## 自动检查

```text
go test ./...                                      PASS
go test -race ./internal/meshsecurity ./internal/proxy PASS
go vet ./...                                       PASS
helm lint -f values-stage15.yaml                    PASS
helm test minimesh（轮换前）                       Succeeded / Succeeded
helm test minimesh（轮换后）                       Succeeded / Succeeded
```

两个 Helm Test 分别验证授权业务链路和未授权直连。

## 集群证据

授权调用：

```text
order=order-book available=true recommendation=recommended-with-book
request_id=stage9-request
```

Inventory Sidecar 在同一 Pod 生命周期中接受轮换前后的 Order 身份：

```text
peer_identity=spiffe://minimesh.local/ns/minimesh/sa/order peer_serial=1000
peer_identity=spiffe://minimesh.local/ns/minimesh/sa/order peer_serial=2000
```

Recommendation Sidecar 拒绝越权身份：

```text
mTLS handshake rejected: mesh RBAC denied peer identity
"spiffe://minimesh.local/ns/minimesh/sa/order"
```

三个业务 Pod 最终均为 `2/2 Running`、`RESTARTS=0`。叶证书轮换通过 Secret volume
和 TLS callback 完成，没有通过重启伪造轮换成功。

## 未宣称能力

- 未实现 SPIRE、Workload API、自动 workload attestation 或生产 CA 托管；
- 未加密 Pod 内 loopback 和 Control Plane 通道；
- 未实现 method/path 级授权、动态策略分发或旧连接强制撤销；
- 未覆盖 CA 无中断交叉轮换、吊销、OCSP/CRL、多集群信任或硬件密钥；
- 证书脚本和 1 天有效期只用于本地可重复验收。
