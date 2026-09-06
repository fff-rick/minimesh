# Stage 7：Rate Limit

## 目标

在 Sidecar 本地实现 Token Bucket，保护后端服务，并支持 default / service / route 三层规则。

## 请求链路

```text
Client
  |
  v
Sidecar Rate Limit
  | allowed
  v
Retry -> Picker -> Circuit Breaker -> Connection Pool -> Backend
```

限流发生在逻辑请求最外层。被拒绝的请求不会创建上游连接，也不会进入 Retry 或 Circuit Breaker。

## 规则优先级

```text
route > service > default
```

CLI 示例：

```bash
--rate-limit=true \
--rate-limit-rate=2000 \
--rate-limit-burst=2000 \
--service-rate-limits='echo=1000:1000;payment=500:500' \
--route-rate-limits='echo|/minimesh.v1.EchoService/Echo=100:200'
```

格式：`rate:burst`。service 规则使用 `service=rate:burst`；route 规则使用 `service|/full/method=rate:burst`。

## 行为

- Token 按 `rate` 持续补充，最多累计到 `burst`。
- 每个通过请求消费 1 token。
- 无 token 时立即以 gRPC `ResourceExhausted` 拒绝。
- 每条规则拥有独立 Bucket，互不影响。
- 显式地址调用没有 service 名，因此只可能命中 default rule。
- 当前为单 Sidecar 本地限流，不保证多 Sidecar 全局配额一致。

## 统计

每条规则记录：

- `Allowed`
- `Rejected`

Stage 7 暴露轻量 `http://<sidecar>:18081/metrics`，仅包含 `minimesh_rate_limit_allowed_total` 与 `minimesh_rate_limit_rejected_total`。完整 Prometheus/Trace/Grafana 仍留在 Stage 10。

## 测试

```bash
make test-stage7
make vet-stage7
go test -race ./internal/resilience -run RateLimit
```

端到端：

```bash
make dev
make demo-stage7
```

Demo 使用 service rule `1000 req/s, burst=1000`，向 Sidecar 输入 5000 个并发请求并打印 success / rate_limited / other_errors。
