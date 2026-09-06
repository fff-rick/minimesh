# Stage 1：基础 gRPC Proxy

## 1. 阶段目标

建立最小可用 Go Sidecar，使请求能够经过：

```text
Client -> Sidecar -> Go Backend
```

本阶段只验证代理链路正确性，不引入服务发现、负载均衡、连接池、Retry、熔断或限流。

## 2. 协议

`ProxyService.Invoke` 接收三个核心字段：

- `target`：Stage 1 显式指定的后端 `host:port`。
- `full_method`：规范 gRPC FullMethod，例如 `/minimesh.v1.EchoService/Echo`。
- `payload`：最小字节载荷。

Sidecar 使用 `grpc.ClientConn.Invoke` 对目标方法重新发起 gRPC 调用。

## 3. Deadline

Sidecar 的上游调用直接派生自入站 RPC 的 `context.Context`，因此客户端 Deadline 会约束：

1. Sidecar 连接 Backend 的时间；
2. Sidecar 调用 Backend 的执行时间。

客户端取消时也会向后传播。

## 4. Metadata

Sidecar 将客户端自定义 gRPC Metadata 复制到 Backend 请求，并过滤 `content-type`、`te`、`user-agent`、`grpc-*` 等传输层保留字段。

Backend 返回的 Header / Trailer 会再写回 Sidecar 的响应上下文。

## 5. 错误映射

- 参数错误：`InvalidArgument`
- Backend 连接失败：`Unavailable`
- Deadline：`DeadlineExceeded`
- Cancel：`Canceled`
- Backend 本身返回的 gRPC Status：原样向客户端传播

## 6. 连接策略边界

Stage 1 每次请求创建并关闭一个 Backend `ClientConn`。这是刻意保留的基线实现，用于先验证链路正确性；Stage 4 再实现连接复用、Keepalive、Idle/Max Connection 与 Graceful Close，并通过 Benchmark 对比收益。

## 7. 测试覆盖

`internal/proxy/server_test.go` 覆盖：

- 正常请求
- Metadata 双向传递
- Backend 不存在
- Backend Deadline
- Backend gRPC 错误透传
- 非法请求
- 1000 并发请求
- 并发结束后的粗粒度 goroutine 泄漏检测

完整测试使用 `bufconn` 建立真实 gRPC `Client -> Sidecar -> Backend` 内存链路，不通过直接函数调用绕过 gRPC Transport。
