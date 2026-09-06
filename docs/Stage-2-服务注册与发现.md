# Stage 2：服务注册与发现

## 目标

引入 etcd，使业务调用能够通过 `service://<service>` 使用动态 Endpoint，而不是固定 Backend 地址。

## 数据模型

etcd Key：`/minimesh/services/<service>/<instance_id>`。

Value 为 Endpoint JSON：`service`、`instance_id`、`address`、可选 `metadata`。Key 绑定 Lease；Lease 过期后 etcd 自动删除 Endpoint，并由 Watch 通知 Sidecar。

## Control Plane

Stage 2 暂用 HTTP Registry API：

- `POST /v1/registry/register`：申请 Lease + Put Endpoint。
- `POST /v1/registry/heartbeat`：KeepAlive Lease。
- `POST /v1/registry/deregister`：Delete Endpoint + Revoke Lease。
- `GET /v1/registry/services/<service>`：列出 Endpoint，并通过 `X-MiniMesh-Revision` 返回 Revision。

正式 Control Plane ↔ Sidecar gRPC 双向流仍保留到 Stage 8。

## Sidecar

Sidecar 启动后：

1. 对 `/minimesh/services/` 做 Full Sync，得到 Snapshot 与 Revision。
2. 原子替换本地 Endpoint Cache。
3. 从 `snapshot.revision + 1` 开始 Watch，避免 Full Sync 和 Watch 之间遗漏事件。
4. PUT/DELETE 事件按 Revision 更新缓存；旧 Revision 会被忽略。
5. Watch 断开后重新 Full Sync，再建立 Watch，保证最终一致。

Stage 2 暂时按 `instance_id` 排序后选择首个 Endpoint，仅用于证明动态发现；Round Robin / Weighted / Least Connections 属于 Stage 3。

## 调用方式

直接地址仍兼容：`--backend 127.0.0.1:19090`。

动态发现：`--service echo`，客户端实际向 Sidecar 发送 `service://echo`。

## Demo

先执行 `make dev` 启动 etcd，然后运行 `make demo-stage2`。脚本会启动 Backend、Control Plane、Sidecar，注册 `echo-1`，验证发现调用成功，再注销实例并验证 Sidecar 无需重启即可感知下线。
