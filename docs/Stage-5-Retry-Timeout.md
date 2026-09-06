# Stage 5：Retry + Timeout

## 1. 阶段目标

在 Stage 4 已具备服务发现、负载均衡和连接复用的基础上，实现基本容错：Request Timeout、Context Deadline、Max Attempts、Exponential Backoff、Retryable Status 与 Retry Budget。

本阶段不实现 Circuit Breaker；熔断状态机完整保留给 Stage 6。

## 2. 请求超时模型

Sidecar 的 `--request-timeout` 是整个上游操作的总预算，包含：

```text
Picker -> Dial/Acquire -> RPC -> Backoff -> Retry -> ...
```

如果调用方已经携带更短的 Deadline，Go Context 会自然取更早到期的那个 Deadline。因此 Sidecar 不会通过 Retry 延长客户端原有 Deadline。

`--request-timeout=0` 表示 Sidecar 不额外设置总超时，仅沿用调用方 Context。

## 3. Retry 策略

默认配置：

```text
max attempts       = 3（包含第一次请求）
initial backoff    = 20ms
max backoff        = 200ms
retryable codes    = Unavailable, ResourceExhausted, Internal
retry budget       = 100 token/s, burst 100
```

Backoff 使用指数增长并封顶：20ms -> 40ms -> 80ms ... -> max-backoff。

只有明确配置为 retryable 的 gRPC Status 才允许重试。`InvalidArgument`、`PermissionDenied` 等客户端/权限错误不会重试。默认也不重试 `DeadlineExceeded`，因为总 Deadline 已经负责终止慢请求，继续重试通常只会放大压力。

## 4. 每次 Retry 重新 Pick

对于 `service://name` 请求，每次 Attempt 都重新执行负载均衡 Picker：

```text
Attempt 1 -> Picker -> instance A -> Internal
Backoff
Attempt 2 -> Picker -> instance B -> OK
```

因此某个实例短暂异常时，可以在下一次尝试中切换到健康实例，而不是持续攻击同一个故障节点。

Least Connections 的 `Done()` 仍然严格绑定单次 Attempt，失败尝试也会归还 in-flight 计数。

## 5. Retry Budget

Retry Budget 是独立 Token Bucket：

- 第一次请求不消耗 token；
- 每一次额外 retry 必须取得 1 个 token；
- token 用尽时立即停止继续 retry，并返回最近一次上游错误；
- 通过 refill rate + burst 限制故障期间的 Retry Storm。

这只是本地 Sidecar Budget。分布式/服务级 Budget 不属于 Stage 5 范围。

## 6. 配置参数

```bash
--request-timeout 2s
--max-attempts 3
--retry-backoff 20ms
--retry-max-backoff 200ms
--retryable-codes unavailable,resource_exhausted,internal
--retry-budget-rate 100
--retry-budget-burst 100
```

## 7. Demo

```bash
make dev
make demo-stage5
```

Demo 覆盖：

1. 第一个实例返回 Internal（模拟 Backend 500），Retry 重新 Pick 到健康实例；
2. 第一个实例返回 Unavailable，Retry 重新 Pick 到健康实例；
3. Backend 慢响应超过 Sidecar Request Timeout，返回 DeadlineExceeded；
4. 所有实例持续失败时，在 Max Attempts / Budget 约束下有限失败，不会无限重试。
