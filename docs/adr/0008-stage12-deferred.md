# ADR-0008：延期 Stage 12 Rust，引入 Kubernetes 优先

- 状态：Accepted
- 日期：2026-09-05

## 背景

Stage 12 原计划增加 Rust gRPC Service，并进一步探索 Rust/eBPF Network
Agent。Stage 11 的 profile 表明，当前主要成本来自额外一跳的 gRPC/HTTP2
收发、系统调用和运行时分配，没有定位到适合通过 Rust 重写消除的独立 Go
应用层热点。Stage 9 也已经通过 Java、Go、Python 和共享 Proto 证明语言无关性。

## 选择

将 Stage 12 标记为 `Deferred`，不视为完成；先执行 Stage 13 Kubernetes
Sidecar。Stage 13 继续使用现有 Go 数据面，不增加 Rust 工具链、镜像和运维路径。

## 被否决的方案

- 立即增加 Rust gRPC Service：能展示 Rust/tonic，但重复 Stage 9 的跨语言结论，
  对 Stage 11 已识别的主要开销没有直接改善。
- 立即实现 Rust/eBPF Agent：工程与面试展示价值较高，但在没有 Kubernetes
  运行环境和明确内核观测需求时属于提前设计。
- 删除 Stage 12：不采用。Rust/eBPF 仍可在出现真实证据后成为有边界的增强项。

## 恢复条件

满足任一条件时重新评估 Stage 12：

1. 固定 Linux 环境的 profile 证明某个 Go 应用层模块是稳定、显著的瓶颈；
2. Kubernetes 场景明确需要 RTT、TCP 状态或重传等内核级观测；
3. 项目目标明确转为 Rust/eBPF 专项学习，并能给模块定义独立验收指标。

## 影响

阶段主线改为 Stage 11 → Stage 13。最终文档必须继续显示 Stage 12 为延期，
避免把未实现能力包装为已完成能力。
