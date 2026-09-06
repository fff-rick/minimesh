# Stage 8 验收报告：Control Stream

## 结果

| 验收项 | 状态 | 说明 |
|---|---|---|
| 双向 gRPC Stream | PASS | `ControlService.Stream` 支持 Register、Heartbeat、ACK。 |
| Endpoint Full Sync | PASS | Control Plane 读取权威 etcd 快照并携带 revision 下发。 |
| 版本与重复消息 | PASS | Sidecar `Cache.Replace` 仅接受更高 revision。 |
| 断线恢复 | PASS | Sidecar 指数退避重连；新流先取得完整快照。 |
| Control Plane 重启 | PASS | `make demo-stage8` 验证重启后请求仍成功。 |
| RouteUpdate 协议兼容 | PASS | Envelope 已定义，Sidecar 可 ACK；无数据面消费者的动态策略未提前实现。 |

## 验证命令

```bash
go test ./...
go vet ./...
go test -race ./internal/controlstream
make demo-stage8
```
