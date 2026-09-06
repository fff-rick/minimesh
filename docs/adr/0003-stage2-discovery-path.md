# ADR-0003: Stage 2 服务发现链路

## 问题
Stage 2 需要实现注册、Lease、Watch、Full Sync 与 Revision，但正式的 Control Plane ↔ Sidecar 双向配置流被安排在 Stage 8。

## 候选方案
1. Stage 2 就提前实现 gRPC Control Stream。
2. Sidecar 轮询 Control Plane HTTP API。
3. Control Plane 写 etcd，Sidecar 在 Stage 2 直接 Full Sync + Watch etcd；Stage 8 再收口为正式 Control Stream。

## 选择
采用方案 3。

## 理由
它能够完整练习 etcd Lease/Watch/Revision，且不提前侵占 Stage 8；Sidecar 的缓存与事件模型未来可以复用于 Control Stream，只替换 Source。

## 代价
Stage 2 的 Sidecar 暂时需要知道 etcd 地址，因此还不是最终控制面边界。

## 未来调整条件
Stage 8 开始后，新增 gRPC 双向流 Source，Sidecar 不再直接连接 etcd；Endpoint Cache 与 Revision 语义保持不变。
