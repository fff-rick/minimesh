# Stage 4 验收报告

## 对照任务

Stage 4 要求实现 Connection Pool、Idle Connection、Max Connection、Connection Reuse、Keepalive、Graceful Close，并记录 active/idle/connection creation total；通过 No Pool vs Connection Pool Benchmark 比较 QPS、P99、CPU 与连接数量。

## 当前环境实际通过

```bash
go test ./internal/connectionpool -count=10
go test ./internal/loadbalance
```

已验证：

- 空闲连接复用，同一 Target 连续请求只创建一次连接。
- 池会扩容到 `MaxConnectionsPerTarget`，达到上限后复用现有连接。
- 1000 并发 Acquire 下连接创建不会突破上限。
- 上述并发上限测试额外连续运行 20 轮通过。
- Idle Reap 会关闭过期连接并从池中删除。
- Close 后拒绝新的 Acquire。
- Stage 3 load-balance 测试仍通过。

## 当前环境限制

本执行环境仍缺少 `grpc-go/protobuf` 的 `go.sum` 条目且无法访问外部 Go Proxy，因此涉及生成代码、`internal/proxy`、Sidecar 和真实 gRPC Benchmark 的完整 `go test ./...` 无法在这里完成依赖解析。

该限制不是以“通过”记录。请在正常联网开发环境执行：

```bash
go mod tidy
make test
make vet
make benchmark-stage4
```

## 本地 Benchmark 验收

`make benchmark-stage4` 会分别运行：

```text
No Pool: --connection-pool=false
Pool:    --connection-pool=true --max-connections=2
```

并输出：

- QPS
- P50 / P95 / P99
- Sidecar CPU seconds
- upstream connection creation total

验收重点不是要求固定提升百分比，而是必须通过数据证明 Connection Pool 显著减少连接创建，并观察 QPS/P99/CPU 是否得到正向改善。

## 阶段边界

Stage 4 不实现 Retry Budget、Retryable Status、Backoff 或新的 Timeout 策略；这些完整留给 Stage 5。
