# ADR-0002：Stage 1 Proxy Contract

## 问题

在尚未引入服务发现和正式控制面的 Stage 1，Sidecar 如何确定 Backend，并如何实现足够通用的 gRPC 转发？

## 候选方案

1. 每个业务 RPC 都在 Sidecar 中生成并调用强类型 Stub。
2. Stage 1 使用 `target + full_method + payload` 统一 Envelope，通过 `ClientConn.Invoke` 动态调用。
3. 直接做 HTTP/2 帧级透明代理。

## 选择

采用方案 2。

## 理由

它能够在较小实现量下验证 Sidecar 代理、Deadline、Metadata、错误传播和跨进程 gRPC 链路，同时不把 Sidecar 与具体业务 Stub 强绑定；也不会提前承担透明代理的复杂度。

## 代价

Stage 1 的 Payload 是教学/验证性质的统一 bytes Envelope，并且 Backend 地址由客户端显式提供；这不是最终服务发现模型。

## 未来调整条件

- Stage 2：`target` 由服务发现结果替代。
- Stage 4：每请求 Dial 改为正式连接管理。
- Stage 9：根据跨语言业务 Proto 确定更完整的业务调用协议边界。
- Stage 14：透明代理阶段重新评估是否需要更底层的 L4/L7 转发模型。
