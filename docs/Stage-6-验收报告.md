# Stage 6 验收报告

## 验收项

| 项目 | 状态 | 说明 |
|---|---|---|
| Closed / Open / Half-Open | 完成 | 并发安全状态机 |
| Sliding Window | 完成 | 固定大小环形窗口 |
| Failure Rate | 完成 | 达阈值自动 Open |
| Minimum Requests | 完成 | 样本不足不触发 |
| Cooldown | 完成 | 到期后进入 Half-Open |
| Probe Requests | 完成 | 有并发上限；全部成功才 Closed |
| Open 快速失败 | 完成 | 不建立上游连接，直接 Unavailable |
| Endpoint 隔离 | 完成 | 每个 upstream address 独立 breaker |
| 80% 错误率场景 | 单测通过 / Demo 已提供 | 8/10 failure window |
| 并发安全 | Race 通过 | Half-Open 100 goroutine 争抢 probe permit |
| 恢复 Closed | 单测通过 / Demo 已提供 | 健康 probe 后恢复 |

## 当前环境实际执行结果

已实际执行：

```bash
go test ./internal/resilience
go test -race ./internal/resilience
go vet ./internal/resilience
go test ./internal/loadbalance
go test ./internal/connectionpool
```

Circuit Breaker 测试覆盖：80% 失败触发 Open、Sliding Window 淘汰旧失败、Open 快速拒绝、Cooldown -> Half-Open、Probe 上限、Probe 失败重新 Open、全部 Probe 成功 Closed、stale completion generation 防护、Endpoint 隔离。

当前沙箱仍缺少 `google.golang.org/grpc` / `google.golang.org/protobuf` 模块缓存且无法联网，因此依赖 gRPC 的完整工程测试与 `make demo-stage6` 无法在本环境实际运行。未把未执行项标记为通过。

## 本地最终验收

```bash
go mod tidy
make dev
make test
make vet
make demo-stage6
```

验收期望：制造 80% 错误率后 breaker 自动 Open；Open 状态快速失败；冷却后进入 Half-Open；Backend 恢复后通过探测自动 Closed。

## 本次构建环境的完整测试状态

`go test ./...` 已尝试执行，但在编译前的 module 校验阶段因缺少 gRPC / protobuf 的 `go.sum` entry 被阻断；错误与前几个阶段一致。Stage 6 无外部新增依赖的状态机测试、Race Detector、vet，以及 Stage 3~5 核心回归均已实际通过。
