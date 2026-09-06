# Stage 6：Circuit Breaker

## 目标

在 Stage 5 Retry + Timeout 之后加入 endpoint 级熔断保护，使持续高错误率的上游实例被快速隔离，并能在冷却后通过有限探测自动恢复。

## 状态机

```text
Closed
  | failure rate >= threshold
  v
Open
  | cooldown elapsed
  v
Half-Open
  | all probes succeed  -> Closed
  | any probe fails     -> Open
```

## 实现

`internal/resilience/circuitbreaker.go` 提供线程安全状态机：

- Sliding Window：固定大小环形窗口，只保留最近 N 次 Closed 状态请求结果。
- Minimum Requests：样本不足时不计算熔断阈值。
- Failure Rate：`failures / requests >= threshold` 时 Open。
- Cooldown：Open 期间请求快速失败；冷却到期后由下一请求切换 Half-Open。
- Probe Requests：Half-Open 只允许固定数量探测请求；任一失败立即重新 Open，全部成功后 Closed。
- Generation：状态代际编号，避免并发旧 Probe 在熔断器重新 Open 后用迟到结果错误关闭新状态。
- Endpoint Isolation：`CircuitBreakerSet` 按 upstream address 建立独立 breaker，一个坏实例不会直接熔断同服务其它健康实例。

## 与 Retry / LB 的组合

一次 `service://` Attempt 的顺序：

```text
Picker -> endpoint circuit Allow -> connection acquire/dial -> upstream Invoke -> circuit Done
```

若 endpoint 已 Open，Sidecar 返回 `Unavailable`。如果 Stage 5 Retry 策略允许该状态，下一 Attempt 会重新 Pick，因此可以绕开已熔断实例。

熔断器只统计配置的 gRPC failure codes，默认：

```text
Unavailable
ResourceExhausted
Internal
DeadlineExceeded
```

业务侧非故障类错误不会默认污染 failure rate。

## Sidecar 参数

```text
--circuit-breaker=true
--circuit-window=20
--circuit-min-requests=10
--circuit-failure-rate=0.5
--circuit-cooldown=5s
--circuit-half-open-probes=2
--circuit-failure-codes=unavailable,resource_exhausted,internal,deadline_exceeded
```

## Demo

```bash
make dev
make demo-stage6
```

Demo 使用确定性的 80% 错误 Backend，验证 Open -> 快速失败 -> 替换健康 Backend -> Cooldown -> Half-Open probes -> Closed。

## 阶段边界

Stage 6 不实现 Token Bucket / Service Rule / Route Rule。Stage 7 Rate Limit 保持独立。
