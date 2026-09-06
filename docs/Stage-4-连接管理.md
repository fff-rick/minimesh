# Stage 4：连接管理

## 目标

避免 Sidecar 每个请求重新建立上游 gRPC 连接，在不改变 Stage 3 服务发现和负载均衡语义的前提下，引入可复用的连接管理层。

## 实现

- `internal/connectionpool`：与 gRPC 解耦的连接池核心。
- `internal/proxy/connection_pool.go`：`grpc.ClientConn` 适配层。
- 每个上游 Endpoint 独立维护连接集合。
- 空闲连接优先复用；没有空闲连接且未达上限时创建新连接；达到上限后选择最少活跃连接继续复用 HTTP/2 多路复用能力。
- `MaxConnectionsPerTarget` 对“已建立 + 正在建立”的连接同时生效，避免并发 Dial 突破上限。
- `MaxIdleTime` + 后台 Reaper 回收长期空闲连接。
- gRPC Keepalive 可通过参数配置。
- Sidecar 收到 SIGINT/SIGTERM 后先 `grpc.Server.GracefulStop()`，再关闭连接池。

## Sidecar 参数

```text
--connection-pool=true
--max-connections=2
--max-idle=30s
--keepalive-time=30s
--keepalive-timeout=5s
```

关闭连接池可回到 Stage 1~3 的每请求 Dial/Close 基线：

```text
--connection-pool=false
```

## 指标

连接池暴露：

- `ActiveConnections`
- `IdleConnections`
- `ConnectionCreated`
- `Targets`

Stage 4 暂以进程日志和 Benchmark 输出验收；Prometheus 暴露留到 Stage 10。

## Benchmark

```bash
make benchmark-stage4
```

脚本对比 No Pool 与 Pool，输出请求 QPS、P50/P95/P99、Sidecar CPU seconds 和 upstream connection creation total。
