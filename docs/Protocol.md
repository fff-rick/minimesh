# MiniMesh Protocol

协议源文件位于 `api/proto/minimesh/v1/`，生成物位于 `api/gen/minimesh/v1/`。

## 数据面

`ProxyService.Invoke(ProxyRequest) -> ProxyResponse` 是显式 Sidecar API：

- `target`：`host:port` 或 `service://name`；
- `full_method`：标准 gRPC fully-qualified method；
- `payload`：普通 Echo 请求或开启 `passthrough_payload` 后的原始 protobuf bytes；
- incoming metadata、deadline、header/trailer 和 gRPC status 保持跨代理传播。

`commerce.proto` 定义最终 Demo 的 Order、Inventory、Recommendation typed RPC。Java、Go、
Python 使用同一份 schema，各语言不依赖 MiniMesh 内部实现。

## 控制面

`ControlService.Stream(stream ControlMessage) returns (stream ControlMessage)`：

- Sidecar 首帧发送 `Register(sidecar_id, last_applied_version)`；
- Control Plane 返回 `EndpointUpdate(full_sync=true)` 与单调 `version`；
- Sidecar 应用后发送 `Ack(version)`，并周期发送 `Heartbeat`；
- 断线后自动重连，每条新 stream 先获取全量 snapshot；
- 低于或等于本地 revision 的重复/乱序版本不改变 cache。

## 兼容性规则

- Proto 字段号不得复用；新增字段必须保持旧消费者可忽略；
- Control Message 采用 `oneof`，未知消息由旧版本忽略或明确 ACK；
- gRPC status 是跨语言错误契约，不能改写为字符串成功响应；
- Trace 使用 W3C `traceparent`，指标名称以 `minimesh_` 为前缀。
