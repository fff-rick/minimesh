# ADR-0005：Stage 4 上游连接管理

## 问题

Stage 1~3 每个请求都会 Dial Backend 并在请求结束后 Close。该方式简单，但 TCP/TLS/HTTP2 连接建立成本会直接进入请求延迟，且高并发下连接创建数量与请求量近似线性增长。

## 候选方案

1. 每个请求新建连接。
2. 每个 Endpoint 固定单个 `grpc.ClientConn`。
3. 每个 Endpoint 维护有上限的可复用连接集合。

## 选择

采用方案 3，并默认每 Endpoint 最大 2 个连接。

## 理由

`grpc.ClientConn` 本身支持并发和 HTTP/2 多路复用，因此单连接已经可以承载多个 RPC；保留一个小规模连接集合，则能为后续连接级故障、连接压力和调优留下空间，同时通过上限避免连接爆炸。

## 代价

需要维护活跃计数、空闲回收、并发建连容量和关闭生命周期；连接数并不等于请求并发数，因此指标必须区分 active connection 与 in-flight request。

## 未来调整条件

Stage 11 Benchmark 若证明单连接/双连接成为瓶颈，再根据 P99、CPU、HTTP/2 stream 并发等数据调整连接策略，而不是提前扩大池。
