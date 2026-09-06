# Stage 1 验收报告

## 对照任务

阶段目标：实现最小可用 Go Sidecar，形成 `Client -> Sidecar -> Go Backend` 链路。

| 要求 | 实现位置 | 状态 |
|---|---|---|
| 基础 RPC 协议 | `api/proto/minimesh/v1/proxy.proto` | 已完成 |
| Sidecar 接收请求 | `cmd/sidecar` + `internal/proxy` | 已完成 |
| Sidecar 转发请求 | `internal/proxy/server.go` | 已完成 |
| Context / Deadline 传递 | 入站 Context 直接派生上游调用 | 已完成 |
| Metadata 传递 | 自定义 Metadata 上行，Header/Trailer 下行 | 已完成 |
| 基础错误映射 | InvalidArgument / Unavailable / DeadlineExceeded / Canceled + 上游 Status 透传 | 已完成 |
| 正常请求测试 | `TestProxyNormalRequestAndMetadata` | 已编写 |
| Backend 不存在 | `TestProxyBackendUnavailable` | 已编写 |
| Backend 超时 / Deadline | `TestProxyDeadlinePropagatesToBackend` | 已编写 |
| 并发请求 | `TestProxy1000ConcurrentRequests` | 已编写 |
| 可独立运行 Demo | `make demo` | 已完成 |

## 本环境执行结果

已通过的静态/本地检查：

- 所有 Go 源文件 `gofmt` 完成；
- `scripts/proto-generate.sh`、`scripts/wait-etcd.sh` Shell 语法检查通过；
- Makefile 的 `run-backend` / `run-sidecar` / `run-client` 命令展开正确；
- 手工随交付包附带 Stage 1 生成代码，以便源码结构完整。

未能在当前沙箱实际执行 `go test ./...`：当前环境没有 `google.golang.org/grpc` / protobuf 模块缓存，同时网络 DNS/Go Proxy 访问被禁用，因此 Go 无法生成 `go.sum` 和下载新依赖。失败发生在依赖解析阶段，不是测试用例运行失败。

在正常联网的开发机上执行：

```bash
go mod tidy
make proto
make test
make vet
make demo
```

最终 Stage 1 验收以以上命令真实通过为准，尤其需要确认 1000 并发测试和 goroutine 泄漏检查。

## 阶段边界

本阶段刻意没有实现：etcd 服务发现、Endpoint Cache、负载均衡、连接池、Retry、熔断、限流和正式 Control Stream。这些能力保留给后续 Stage。
