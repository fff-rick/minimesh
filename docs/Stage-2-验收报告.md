# Stage 2 验收报告

## 对照任务

- Control Plane：Register / Deregister / Lease / Heartbeat / Endpoint 管理：已实现。
- Sidecar：Endpoint Cache / Watch / Full Sync / Revision：已实现。
- 实例上线/下线无需重启 Sidecar：由 Watch + Demo 验证路径覆盖。
- Stage 3 负载均衡：未提前实现；当前仅稳定选择首实例。

## 本环境已执行

```text
go test ./internal/discovery ./internal/registry ./internal/etcdhttp
```

结果：通过。

同时执行 `make vet-stage2`，结果：通过。

覆盖：Full Sync、Revision、旧事件忽略、PUT/DELETE、Watch 起始 Revision、Registry 注册/心跳/注销 API。

## 环境限制

当前沙箱无法解析 `proxy.golang.org`，且没有 Stage 1 所需 `grpc-go` / protobuf 模块缓存，因此 `go test ./...` 在读取生成的 gRPC 代码时因缺少 `go.sum` 条目停止，无法在这里完成完整工程依赖解析。Stage 2 新增的 discovery/registry/etcdhttp 包不依赖这些外部模块，已单独完成 test + vet。当前环境也没有 Docker，无法真实启动 etcd 容器执行 Lease/Watch 集成测试。

## 本地最终验收

在具备 Docker 与正常 Go 网络依赖的环境执行：

```bash
make dev
go mod tidy
make test
make vet
make demo-stage2
```

`make demo-stage2` 必须观察到：注册后 `service://echo` 请求成功；注销后无需重启 Sidecar，同一服务请求变为 `Unavailable`。

进一步验证 Lease：注册一个短 TTL Endpoint，不发送 heartbeat，等待 TTL 过期后再次请求，应自动失去该 Endpoint。
