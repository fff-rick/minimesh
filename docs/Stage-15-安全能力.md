# Stage 15：安全能力

## 完成范围

MiniMesh 现在为跨 Pod 数据面提供 Sidecar-to-Sidecar TLS 1.3 双向认证。Java、Go、
Python 业务代码无需读取证书；源 Sidecar 根据目标服务选择客户端身份与预期服务
身份，目标 Sidecar 验证来访工作负载身份并执行 allowlist。

身份格式：

```text
spiffe://minimesh.local/ns/minimesh/sa/order
spiffe://minimesh.local/ns/minimesh/sa/inventory
spiffe://minimesh.local/ns/minimesh/sa/recommendation
```

授权矩阵：

| 调用方 | 目标 | 结果 |
|---|---|---|
| Order | Inventory | 允许 |
| Inventory | Recommendation | 允许 |
| Order | Recommendation | 拒绝 |

## 实现结构

- `internal/meshsecurity`：身份校验、TLS 配置、握手级 RBAC 和 TLS relay；
- `internal/proxy`：按目标服务创建带独立身份验证的 gRPC connection；
- Helm：挂载每个 workload 的 identity Secret，注册 mTLS ingress 地址；
- `scripts/stage15-certs.sh`：生成演示 CA 和 1 天有效期的叶证书；
- `scripts/stage15-demo.sh`：部署、正反向授权测试和无重启叶证书轮换。

运行：

```bash
make demo-stage15
```

该命令复用 Stage 13 kind 集群。首次运行前可执行 `make demo-stage13`。

## 轮换语义

叶证书更新到 Kubernetes Secret 后，volume 投影会更新文件；新 TLS 握手通过 callback
读取新证书，不需要重启 Pod。演示将 Order 序列号从 1000 切换为 2000，并从
Inventory 日志验证两者。

信任根采用不同策略：CA 内容哈希进入 Deployment annotation，CA 变化会触发滚动，
避免运行中客户端和服务端持有不同 CA pool。生产环境应使用 SPIRE 或证书管理系统，
而不是脚本持有 CA 私钥。

## 明确边界

当前只保护跨 Pod Sidecar 链路。本地业务容器与 Sidecar 之间、Sidecar 与 Control
Plane 之间仍为明文。RBAC 是连接级服务身份 allowlist，不是 gRPC method 级授权。
Stage 14 的透明直连 L4 路径也没有在本阶段并入安全数据面；Stage 15 验收使用显式
ProxyService 路径。
