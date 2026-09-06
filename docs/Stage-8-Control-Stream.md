# Stage 8：Control Stream

## 判断与设计

采用 gRPC 双向流，且由 Control Plane 独占 etcd 访问。这个选择适合当前项目：它保留了 Sidecar 主动注册、心跳和 ACK 的通道，同时避免每个 Sidecar 直接建立 etcd Watch。

不采用“按 ACK 重放增量”的可靠投递队列。它需要持久化每个 Sidecar 的 ACK 游标、处理控制面重启和消息保留期，当前学习项目没有对应的配置规模需求。替代方案是每条流建立时及每次 Endpoint 变更后下发完整快照；快照具有幂等性，配合 etcd revision 可处理 ACK 丢失、重复或乱序消息。

## 协议与一致性

`ControlService.Stream` 使用 `ControlMessage` Envelope：

- Sidecar → Control Plane：`Register`（含 `sidecar_id`、`last_applied_version`）、`Heartbeat`、`Ack`。
- Control Plane → Sidecar：`EndpointUpdate`（完整快照、`full_sync=true`、etcd revision）以及预留的 `RouteUpdate`。
- Sidecar 只有在 `version > cache.revision` 时替换缓存，随后发送 ACK。

单个 gRPC RPC 内的消息有序，但应用层仍以版本防止重连交错、旧快照或重复消息回退缓存。Control Plane 重启或流失败时，Sidecar 使用上限 2 秒的指数退避重连，保留最后有效缓存继续转发；新流收到完整快照后恢复到控制面最新状态。

## 运行

```bash
make dev
make demo-stage8
```

Demo 会注册一个 echo Endpoint，验证流首次同步；然后重启 Control Plane，验证 Sidecar 自动重连和重新全量同步后仍可路由。

## 验收与边界

```bash
make test-stage8
make vet-stage8
go test -race ./internal/controlstream
```

目前 `RouteUpdate` 是兼容性 Envelope，不下发无消费者的路由配置。全局限流、TLS/mTLS、每 Sidecar ACK 持久化与 Prometheus exporter 分别留给其对应的后续阶段。
