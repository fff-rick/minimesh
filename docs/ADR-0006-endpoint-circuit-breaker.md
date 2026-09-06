# ADR-0006：Circuit Breaker 按 Endpoint 隔离

## 问题

一个服务可能有多个实例。若只有一个实例高错误率，熔断整个服务会连健康实例一起阻断。

## 候选方案

1. Service 级 breaker：实现简单，但故障隔离粒度过粗。
2. Endpoint 级 breaker：每个实例独立统计和转换状态。
3. Service + Endpoint 双层 breaker：能力更完整，但 Stage 6 复杂度过高。

## 选择

Stage 6 采用 Endpoint 级 breaker，由 `CircuitBreakerSet` 以 upstream address 为 key。

## 理由

- 与 Stage 3 Picker 和 Stage 5 retry/re-pick 自然组合。
- 单实例 Open 后，其它实例仍可被调用。
- 能直接演示“部分实例异常不会拖垮整个服务”。

## 代价

- Endpoint churn 会留下少量历史 breaker 对象；当前阶段不做 TTL 清理。
- 暂不支持 service 级整体错误率保护。

## 未来调整条件

若 Benchmark/故障测试证明 breaker map 生命周期或 service 级保护成为真实需求，再引入清理策略或双层 breaker。
