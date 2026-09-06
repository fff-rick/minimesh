# Stage 14：透明代理

## 判断

有条件推荐 `iptables REDIRECT + SO_ORIGINAL_DST + L4 TCP passthrough`，仅实现一个
受端口 allowlist 限制的 IPv4 出站方案。不建议此时实现 TPROXY、eBPF/CNI、自动
注入或全量入站捕获。

成熟 Sidecar Mesh 同样通过 init container/CNI 安装流量规则，并按代理 UID 排除
Sidecar 自身连接。Linkerd 的官方 iptables 说明明确描述了 `OUTPUT` 重定向、
代理 UID bypass 与 `SO_ORIGINAL_DST` 恢复目标：
[Linkerd IPTables Reference](https://linkerd.io/2-edge/reference/iptables/)。

## 实现链路

```text
Go Inventory (direct gRPC to minimesh-recommendation:19092)
    ↓ Pod OUTPUT
MINIMESH_OUTPUT
    ├── uid 1337 → RETURN
    ├── 127.0.0.0/8 → RETURN
    └── dport 19092 → REDIRECT :15001
                              ↓
                    MiniMesh L4 listener
                    SO_ORIGINAL_DST
                              ↓
              Recommendation ClusterIP:19092
                              ↓
                    Python Recommendation
```

代码位置：

- `internal/transparentproxy`：TCP accept、原始目标恢复和双向 copy；
- `deploy/transparent/iptables-init.sh`：幂等创建 Pod NAT chain；
- `deploy/docker/stage14-init.Dockerfile`：固定 iptables 运行环境；
- `deploy/helm/minimesh/values-stage14.yaml`：启用 Stage 14；
- `examples/go/inventory-server`：支持不引用 Sidecar API 的 direct gRPC 模式。

## 安全取舍

iptables 初始化需要修改 Pod network namespace。Kubernetes 官方建议避免整个容器
进入 privileged 模式，只授予所需 capability：
[Linux kernel security constraints](https://kubernetes.io/docs/concepts/security/linux-kernel-security-constraints/)。
本实现只给短生命周期 init container `NET_ADMIN`；Sidecar 长期以非 root 运行。

这仍不符合 Kubernetes `Restricted` Pod Security Standard，因为该标准不允许增加
`NET_ADMIN`。生产环境更适合 CNI 安装规则；Linkerd 也将 CNI 作为避免 Pod 内
`CAP_NET_ADMIN` 的替代路径：
[Linkerd CNI Plugin](https://linkerd.io/docs/features/cni/)。

## 运行

Stage 14 复用已经验收的 Stage 13 kind 集群：

```sh
make test-stage14
make vet-stage14
make demo-stage14
```

脚本会执行 Helm upgrade/test，并强制检查：

1. Java → Go → Python 业务结果成功；
2. init container 输出预期 `MINIMESH_OUTPUT` NAT 规则；
3. Inventory 明确运行在 `transparent_direct=true`；
4. Sidecar 日志包含 `transparent connection original_destination=...:19092`。

## 阶段边界

透明路径当前只做 L4 转发，无法执行原有 L7 治理能力。工程上如果目标是直接获得
完整生产能力，应选择 Envoy、Linkerd 等成熟数据面；MiniMesh 自行实现这一最小
路径的价值在于验证 Linux netfilter、代理防环和 original destination 机制。
