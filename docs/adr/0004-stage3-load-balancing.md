# ADR-0004：Stage 3 负载均衡接口与状态管理

## 问题

Stage 2 已能动态获得 Endpoint，但只能稳定选取第一个实例。Stage 3 需要支持 RR、Weighted RR、Least Connections，同时 Endpoint 会通过 Watch 动态变化。

## 候选方案

1. 在 Proxy 内直接写 switch 分支实现三个算法。
2. 为每个请求临时根据 Endpoint 列表计算目标。
3. 抽象统一 Picker，Resolver 为每个 service 持有算法状态并在 Endpoint 变化时更新。

## 选择

选择方案 3。

## 理由

算法状态与代理逻辑解耦；Stage 4 连接池、Stage 5 Retry 后仍可复用 Picker；RR 可以使用原子快照降低读锁竞争；Least Connections 可以通过 `Done()` 把计数生命周期绑定到真正的上游请求。

## 代价

Weighted RR 和 Least Connections 内部仍存在小粒度同步；Resolver 需要维护 per-service Picker 状态与 Endpoint 指纹。

## 未来调整条件

如果 Stage 11 Benchmark 显示 Picker 锁竞争成为明确热点，再考虑更复杂的无锁结构、P2C（Power of Two Choices）或按连接池统计的 Least Requests；没有 Benchmark 证据前不提前复杂化。
