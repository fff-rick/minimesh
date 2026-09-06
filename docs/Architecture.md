# MiniMesh Architecture

## 系统边界

MiniMesh 是教学和面试用途的轻量 RPC Mesh。业务进程只调用本地 Sidecar；Control Plane
以 etcd 为事实源，通过长连接向 Sidecar 下发带 revision 的全量 Endpoint snapshot。
Sidecar 在进程内完成发现缓存、负载均衡、连接复用、超时重试、Endpoint 级熔断、限流
与可观测性。

```text
Java Order -> Sidecar(order) -> Go Inventory -> Sidecar(inventory) -> Python Recommendation
                    ^                    ^                 ^
                    +------ Control Stream snapshots ------+
                                      |
                             Go Control Plane -> etcd

Sidecars -> Prometheus/Grafana
Services + Sidecars -> OTLP -> Jaeger
Rust Validation Agent -> health/readiness endpoints (read only)
```

## 关键数据流

1. Endpoint 注册到 Control Plane，并以 lease 写入 etcd。
2. Control Plane watch etcd，建立/重建流时发送带 revision 的全量 snapshot。
3. Sidecar 只应用更高 revision，控制面短暂不可用时保留最后一次有效 cache。
4. 请求先通过限流，再在总 deadline 内执行 retry；每次 attempt 重新 pick Endpoint。
5. Endpoint breaker 在选中实例维度隔离失败；connection pool 复用到该实例的 gRPC 连接。
6. 指标由各 Sidecar 暴露，W3C trace context 沿业务和代理链传播。

## 一致性与故障模型

Endpoint 配置是最终一致：Sidecar 不执行增量 patch，而以单调 revision 的 snapshot 收敛。
这允许重复消息、ACK 丢失和短暂断连，但意味着控制面宕机期间不能获知新 Endpoint。数据面
cache 提供的是短时可用性，不是无限期正确性；过期 Endpoint 由调用失败、retry 和 breaker
临时隔离，控制面恢复后再由新 snapshot 修正。

## 部署边界

Docker Compose 是跨语言、观测和故障注入的最终可重复 Demo；Helm/kind 验证 Pod 双容器、
透明代理和 mTLS。生产化仍需要多副本 Control Plane、etcd quorum/PV、证书管理、Network
Policy、资源容量与 SLO 驱动的告警。
