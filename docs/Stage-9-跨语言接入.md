# Stage 9：跨语言接入

## 判断与设计

推荐以**共享业务 Proto + 通用 Sidecar Proxy**完成跨语言链路，而不是为 Java、Go、Python 各自设计 MiniMesh SDK。Java、Go、Python 分别从同一 `commerce.proto` 生成业务 stub；业务服务只知道自己的本地 Sidecar，Sidecar 只负责把序列化后的 Protobuf 负载转发到标准 gRPC 方法。

这是当前规模最合适的边界：gRPC 的 Java 与 Python 官方指引都将 Proto 代码生成作为跨语言服务接口的标准做法；Proto3 的 wire format 让 `bytes` 负载可由任意生成实现解析。过早设计 SDK、HTTP Gateway 或专用 trace 协议会把 Stage 9 从“验证语言无关”变成另一个控制面项目。

## 链路

```text
Java Order :19090
  -> order Sidecar :18080
  -> Go Inventory :19091
  -> inventory Sidecar :18080
  -> Python Recommendation :19092
```

两个 Sidecar 都通过 Stage 8 `ControlService.Stream` 获取 `inventory` 与 `recommendation` Endpoint。Demo 仍由 Control Plane 的 Registry API 注册 Endpoint，因此没有绕过服务发现。

## 兼容与语义

- `commerce.proto` 为 Java 指定 `java_package=io.minimesh.v1`，Go 保持已有 `go_package`；Python 在容器构建时从同一文件生成代码。
- Java Order 将 `InventoryRequest` 序列化进 `ProxyRequest.payload`；Go Inventory 将 `RecommendationRequest` 再序列化进第二跳 `ProxyRequest.payload`。每个目标服务以自身生成的类型反序列化，验证 wire compatibility。
- `x-request-id` 与 W3C `traceparent` 由 Java 入口写入 metadata，Sidecar 过滤保留的 gRPC transport headers 后逐跳复制。Python 将二者回显到业务响应，最终由 Java client 打印。
- Java 收到的 deadline 会限制第一跳 Proxy 调用；Go 将入站 context（含 deadline）用于第二跳。Python `missing` 返回 `NOT_FOUND`，该 gRPC status 不会被包装为语言特定异常。

## 运行与验收

```bash
make proto-local
make test-stage9
make vet-stage9
make demo-stage9
```

`make demo-stage9` 使用 Docker 构建 Java 21、Go 1.23 与 Python 3.13 运行时，注册 Endpoint 后运行 Java client。成功输出应含：

```text
order=order-book available=true recommendation=recommended-with-book
trace_id=00-0123456789abcdef0123456789abcdef-0123456789abcdef-01
request_id=stage9-request
```

Stage 9 不把 trace 采集或 metrics exporter 混入实现；这些在 Stage 10 接入 OpenTelemetry 与 Prometheus 后再统一处理。
