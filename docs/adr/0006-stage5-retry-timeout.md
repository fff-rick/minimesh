# ADR-0006：Stage 5 Retry 与 Timeout 采用总 Deadline + 有界重试

## 问题

Sidecar 如何在临时故障下提高成功率，同时避免无限重试、Retry Storm 和超时被不断延长？

## 候选方案

1. 任意错误都固定次数立即重试；
2. 仅对配置状态码进行重试，但每次继续访问同一实例；
3. 总 Deadline + Retryable Status + Exponential Backoff + Retry Budget，并在 service 请求的每次 Attempt 重新 Pick。

## 选择

选择方案 3。

## 理由

- 总 Deadline 保证一次逻辑请求的时间上界；
- 状态码白名单避免对明显不可恢复错误重试；
- Backoff 降低瞬时故障期间的额外压力；
- Retry Budget 为整个 Sidecar 提供 Retry Storm 上限；
- 每次重新 Pick 才能利用 Stage 3 的多实例能力绕开异常节点。

## 代价

- Retry 会增加流量和尾延迟；
- 对非幂等方法开启 retry 仍存在重复副作用风险；Stage 5 只提供机制，不自动判断业务幂等性；
- 本地 Budget 无法约束整个集群的全局重试量。

## 未来调整条件

- Stage 8 可由 Control Plane 动态下发不同服务/路由的 Retry Policy；
- Stage 10 增加 retry_total 等 Metrics；
- 如果 Benchmark 证明 Backoff/Budget 锁竞争明显，再进行数据驱动优化。
