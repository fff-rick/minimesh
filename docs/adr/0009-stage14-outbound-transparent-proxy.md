# ADR-0009：使用 iptables REDIRECT 实现最小出站透明代理

- 状态：Accepted
- 日期：2026-09-05

## 背景

Stage 13 的业务服务通过 `ProxyService.Invoke` 显式调用本地 Sidecar。该接口是
MiniMesh 自定义的 L7 信封协议，iptables 不能把原生 gRPC 请求直接重定向给它：
被重定向的客户端发送的是业务 gRPC Method，而不是 `ProxyService.Invoke`。

## 选择

新增独立的 IPv4 L4 TCP listener。Inventory 直接调用 Kubernetes
Recommendation Service；Pod init container 使用 iptables `OUTPUT`/`REDIRECT`
将目标端口 19092 转到 Sidecar 15001。Sidecar 使用 `SO_ORIGINAL_DST` 恢复原始
ClusterIP 与端口，然后双向转发未经解析的 gRPC/HTTP2 字节流。

仅 Inventory Pod 启用该能力，以一个真实出站调用完成阶段验收。规则按目标端口
做 allowlist，不捕获 Pod 的全部 TCP 流量。

## 防止代理环路

Sidecar 使用专用 UID 1337。iptables 首先跳过该 UID 产生的流量，因此 Sidecar
连接原始目标时不会再次进入自身。Pod-local `127.0.0.0/8` 也被排除。

## 权限边界

只有运行后即退出的 init container 获得 `NET_ADMIN`，且先 drop 全部 capability；
长期运行的 Sidecar 以非 root UID 1337 运行、drop 全部 capability，并启用
`RuntimeDefault` seccomp。没有给 Pod 或 Sidecar `privileged: true`。

## 被否决的方案

- 将原生 gRPC 直接重定向到现有 ProxyService：协议不兼容，无法工作。
- TPROXY + policy routing：能更完整地保留地址语义，但配置、内核依赖和故障面
  对当前单个出站验证过重。
- 入站 + 出站全量捕获：需要完善端口排除、自调用、探针和失败回退，暂不扩大范围。
- eBPF/CNI 或注入 Webhook：更适合规模化和受限安全策略环境，但会引入集群级
  组件，不符合当前学习项目的最小阶段目标。
- 直接接入 Envoy/Linkerd：工程生产价值更高，但会替换 MiniMesh 数据面实现，
  无法达到本阶段理解透明转发机制的学习目标。

## 代价

当前透明 listener 是 L4 passthrough，不解析 gRPC 方法，所以这条直连路径不应用
MiniMesh 现有的逐请求 retry、circuit breaker、rate limit 与 L7 指标。它证明的是
稳定流量劫持，而不是生产级透明 L7 Mesh。仅支持 Linux IPv4；IPv6、UDP、入站
捕获和 CNI 安装不在本阶段范围内。

## 未来调整条件

- 需要透明路径复用现有治理策略时，增加 HTTP/2/gRPC-aware 数据面或采用成熟代理；
- 需要受限 Pod Security 环境时，将规则安装迁移到 CNI；
- 需要入站身份与授权时，再设计独立 inbound listener 和明确的应用端口排除规则。
