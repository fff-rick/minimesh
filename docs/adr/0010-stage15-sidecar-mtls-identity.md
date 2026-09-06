# ADR-0010：Sidecar 终止 mTLS，并以 URI SAN 承载工作负载身份

- 状态：Accepted
- 日期：2026-09-06

## 背景

Stage 15 需要同时证明服务身份、双向认证、证书轮换和最小 RBAC。业务容器已有
Java、Go、Python 三种实现；若让每种语言分别管理证书，会把安全能力泄漏到业务
代码，并扩大改造与轮换故障面。

## 选择

跨 Pod 链路由源 Sidecar 发起 TLS 1.3，目标 Sidecar 在独立 15443 listener 终止
mTLS，再把解密后的 HTTP/2 字节流转给同 Pod 业务端口。证书 URI SAN 使用：

```text
spiffe://minimesh.local/ns/minimesh/sa/<service>
```

客户端同时验证 CA、目标服务的 DNS SAN `<service>.mesh` 和精确 URI SAN；服务端
验证 CA、精确 URI SAN，并在 TLS 握手中执行 allowlist RBAC：Order 可访问
Inventory，Inventory 可访问 Recommendation。连接协商显式提供 `h2` ALPN。

叶证书由 TLS callback 在新握手时从 Secret volume 重读；连接池在 Stage 15 使用
2 秒 idle timeout、关闭 keepalive，保证轮换演示会建立新连接。CA pool 不能安全地
只替换文件，因此 Helm 将 CA 哈希写入 Pod template annotation，信任根变化触发
显式滚动。

## 比较与取舍

- 业务 SDK 直接启用 mTLS：端到端边界更接近应用，但三种语言都要改造，违背
  Sidecar 统一承载基础设施能力的目标。
- 引入 SPIRE：能提供自动证明、签发和轮换，是生产化方向；当前单集群学习阶段会
  新增 server、agent、attestation 和运维面，超出最小验收闭环。
- 只用 Kubernetes ServiceAccount token：可做应用层鉴权，但不能替代传输加密和
  双向握手身份。
- 方法级授权：粒度更细，但当前 relay 是 L4，不解析 gRPC method；本阶段只声明
  服务到服务的连接级授权。

因此选择本地 CA + Kubernetes Secret 作为可验证的教学实现，不将其描述为生产
证书控制面。身份命名遵循 SPIFFE ID 的 URI 形式，但项目没有实现完整 SPIFFE
Workload API 或 SPIRE。

## 安全边界

- 受保护：跨 Pod 的 Sidecar → Sidecar 数据链路。
- 未加密：业务容器 → 同 Pod Sidecar、目标 Sidecar → 同 Pod 业务容器。
- 未加密：Sidecar → Control Plane 的注册与控制流。
- 本地 CA 私钥由演示脚本持有，证书有效期 1 天；不适用于生产。
- RBAC 只在新连接握手时求值，策略变更需要使旧连接退出。

## 依据

- [SPIFFE Concepts](https://spiffe.io/docs/latest/spiffe/concepts/)
- [SPIFFE ID specification](https://spiffe.io/docs/latest/spiffe-specs/spiffe-id/)
- [X.509-SVID specification](https://spiffe.io/docs/latest/spiffe-specs/x509-svid/)
- [gRPC authentication guide](https://grpc.io/docs/guides/auth/)
- [Go crypto/tls](https://pkg.go.dev/crypto/tls)
