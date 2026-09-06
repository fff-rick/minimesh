# ADR-0007：Stage 7 使用 Sidecar 本地 Token Bucket

## 问题

Stage 7 需要在不引入分布式协调的前提下保护 Backend，并支持服务级和路由级规则。

## 候选方案

1. Fixed Window
2. Sliding Window Counter
3. Token Bucket
4. Redis/集中式分布式限流

## 选择

使用每个 Sidecar 本地 Token Bucket，并采用 `route > service > default` 的匹配顺序。

## 理由

Token Bucket 同时表达稳定速率和瞬时 Burst，状态简单、锁粒度小，适合在数据面请求入口快速判定。Stage 7 目标明确是本地限流，因此不为全局配额提前引入 Redis 或协调服务。

## 代价

多 Sidecar 时每个实例拥有独立 Bucket，服务总放行量可能约等于单 Sidecar 配额乘以 Sidecar 数量；规则目前由启动参数提供，尚未动态下发。

## 未来调整条件

Stage 8 Control Stream 可承载动态规则；若后续明确需要全局配额，再设计集中式或分片式 Rate Limit Service。Stage 10 将 Allowed/Rejected 统计暴露为 Prometheus Metrics。
