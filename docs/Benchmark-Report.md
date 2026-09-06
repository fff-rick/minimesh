# MiniMesh Benchmark Report

Stage 11 已建立同口径的 direct gRPC 与 Mesh 基线。完整方法、环境、原始指标和 pprof
解释见 [Stage-11-Benchmark-Performance.md](Stage-11-Benchmark-Performance.md) 与
[Stage-11-验收报告.md](Stage-11-验收报告.md)。

当前已记录的本机样本：20,000 请求、并发 100、4 connections；Direct 23,838 QPS，
Mesh 16,047 QPS；Mesh P50/P95/P99 为 4.917/10.403/13.340 ms。该结果仅为一台 WSL2
机器上的样本，不是容量承诺。重新运行 `make benchmark-stage11` 会在
`artifacts/stage11/` 生成原始 JSON、报告和 pprof。
