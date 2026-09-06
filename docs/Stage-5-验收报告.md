# Stage 5 验收报告

## 验收项

| 项目 | 状态 | 说明 |
|---|---|---|
| Request Timeout | 完成 | Sidecar 总请求时间预算 |
| Context Deadline | 完成 | 调用方更短 Deadline 优先 |
| Max Attempts | 完成 | 包含首次请求，默认 3 |
| Backoff | 完成 | 指数退避 + 最大值 |
| Retryable Status | 完成 | gRPC code 白名单 |
| Retry Budget | 完成 | 本地 token bucket，只约束额外 retry |
| Retry 重新选择实例 | 完成 | service:// 每次 Attempt 重新 Picker |
| Backend 500/Internal 场景 | Demo 已提供 | faulty backend + healthy backend |
| Backend timeout 场景 | Demo 已提供 | slow backend + request timeout |
| Backend 短暂不可用 | Demo 已提供 | Unavailable backend + healthy backend |
| 部分实例异常 | Demo 已提供 | Retry 可切换实例 |
| 无限重试保护 | 完成 | Max Attempts + Deadline + Budget 三层约束 |

## 当前环境实际执行结果

已实际执行：

```bash
go test ./internal/resilience
go test -race ./internal/resilience
go vet ./internal/resilience
go test ./internal/connectionpool
go test ./internal/loadbalance
```

Stage 5 Retry 核心的 Backoff、Context Deadline、Max Attempts、非重试错误、Budget Burst/Refill/Budget Exhausted 均通过；1000 goroutine 并发竞争 Retry Budget 的 race 测试也通过，未突破 Burst 上限。

当前沙箱仍没有 `google.golang.org/grpc` 与 `google.golang.org/protobuf` 模块缓存，同时无法联网生成 `go.sum`，因此依赖 gRPC 的完整工程测试与 `make demo-stage5` 无法在本环境实际运行。该限制与 Stage 1~4 相同，不将未运行项目标记为通过。

## 本地最终验收

```bash
go mod tidy
make dev
make test
make vet
make demo-stage5
```

验收期望：临时失败可通过有限 Retry 恢复；慢请求受 Deadline 约束；持续失败最终有限返回，不存在无限 Retry。
