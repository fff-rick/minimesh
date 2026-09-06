# Stage 7 验收报告

## 阶段范围

已实现：Token Bucket、rate、burst、service rule、route rule、本地 Sidecar 限流、Allowed/Rejected 统计，以及 Stage 7 限流专用 `/metrics` 文本端点。

未实现：分布式限流、动态配置下发和完整可观测栈。正式 Control Stream 属于 Stage 8；Stage 10 再统一接入完整 Prometheus/Trace/Grafana。

## 当前环境实际执行

已通过：

```text
make test-stage7
go test -race ./internal/resilience -run RateLimit
make test-stage6
make test-stage5
make test-stage4
make test-stage3
```

核心 5000 请求测试使用可控时钟：`rate=1000/s, burst=1000` 时首批精确放行 1000、拒绝 4000；推进一秒后补充 1000 token。并发 1000 goroutine 竞争 `burst=100` 时不会超过 100 个放行请求。

## 环境限制

当前沙箱仍无法解析既有 `grpc-go/protobuf` 依赖的缺失 `go.sum`，因此 `go test ./...`、Sidecar 端到端 gRPC Demo 无法在此环境完成。Stage 7 核心限流包无新增第三方依赖，已实际测试和 Race 检测。

## 本地最终验收

```bash
go mod tidy
make dev
make test
make vet
make demo-stage7
```

验收关注：

1. 高于规则速率的输入产生明确 `ResourceExhausted`。
2. 放行量受 rate / burst 约束。
3. Burst 可被消费并按时间恢复。
4. route rule 覆盖 service rule，service rule 覆盖 default rule。
5. 限流拒绝不会触发上游请求。
6. `/metrics` 暴露 `minimesh_rate_limit_rejected_total`。
