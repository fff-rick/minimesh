# Stage 11：Benchmark & Performance

## 判断

有条件推荐 `ghz + pprof + Prometheus runtime metrics`。`ghz` 同时覆盖直连与 Mesh 的原生 gRPC 调用，保证协议、负载模型和统计口径一致；`pprof` 用于定位 CPU 与分配热点。Vegeta 是 HTTP 压测工具，不纳入 gRPC 数据面开销对比，否则工具和协议差异会污染结论；只有压测 Control Plane HTTP API 时才使用它。

不预先加入 Buffer Pool 或 `sync.Pool`。阶段目标是先建立基线，只有 allocation profile 显示稳定且显著的应用层分配热点时才优化。

## 运行

```sh
make test-stage11
make vet-stage11
make benchmark-stage11
```

默认执行 20,000 次请求、并发 100、4 条客户端连接，并跳过首批 100 个 warm-up 样本：

```sh
REQUESTS=50000 CONCURRENCY=200 CONNECTIONS=8 make benchmark-stage11
```

若本机没有 `ghz`，脚本会将固定版本安装到临时目录。也可通过 `GHZ=/path/to/ghz` 使用已有二进制。

## 产物

结果写入 `artifacts/stage11/`：

- `REPORT.md`：环境、方法、QPS、P50/P95/P99、CPU、RSS、Goroutine、GC 和连接数。
- `direct.json` / `mesh.json`：ghz 原始数据。
- `profile-load.json`：与 CPU profile 等长的独立持续负载结果，不计入基线表格。
- `profiles/sidecar-cpu.pprof` / `sidecar-heap.pprof`：原始 profile。
- `profiles/*-top.txt`：无需 UI 即可审阅的热点摘要。

`artifacts/` 是本机测量结果，不提交版本库。正式验收报告记录一次实测摘要；性能回归门槛应在固定硬件上重复采样后另行确定。

## pprof 安全边界

Sidecar 的 `--pprof` 默认关闭。开启后，profile 与命令行等调试信息会暴露在 `--metrics-listen`，因此只能绑定到可信调试网络，不能直接暴露到公网。
