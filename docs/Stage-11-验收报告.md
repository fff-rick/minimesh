# Stage 11 验收报告：Benchmark & Performance

## 验收项

| 项目 | 验证方式 |
| --- | --- |
| 等价基线 | 同一 ghz 版本和参数分别请求 EchoService 与 ProxyService。 |
| 延迟与吞吐 | 原始 JSON 和报告包含 QPS、P50、P95、P99。 |
| 资源开销 | 记录 Backend/Sidecar CPU、RSS、Goroutine、GC 与连接数。 |
| 性能定位 | Sidecar 输出 CPU、heap、goroutine pprof 及 top 摘要。 |
| 可重复运行 | `make benchmark-stage11` 构建二进制、运行两条路径并生成报告。 |

## 本地校验

```sh
make test-stage11
make vet-stage11
REQUESTS=5000 CONCURRENCY=50 PROFILE_SECONDS=3 make benchmark-stage11
```

实测结果位于 `artifacts/stage11/REPORT.md`。该报告只代表当前本机环境，不作为跨机器性能承诺。

## 本机实测摘要

环境：WSL2 Linux、8 CPU、Go 1.25.4、ghz v0.121.0；20,000 请求、并发 100、4 条客户端连接、跳过前 100 个 warm-up 样本。

| 路径 | QPS | P50 | P95 | P99 | Backend CPU | Sidecar CPU | Sidecar RSS |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Direct | 23,838.14 | 2.018 ms | 7.597 ms | 10.951 ms | 1.20 s | — | — |
| Mesh | 16,047.42 | 4.917 ms | 10.403 ms | 13.340 ms | 1.12 s | 2.80 s | 25,572 KiB |

本轮 Mesh 相比直连 QPS 低 32.68%，P50/P95/P99 分别增加 2.899/2.806/2.389 ms。负载结束时 Sidecar 有 28 个 goroutine、178 次 GC，连接池为 active/idle/created = 0/2/2。所有计分请求以及 10 秒 profile 负载请求均为 `OK`。

## 瓶颈与优化判断

CPU profile 的最大单点是系统调用，随后是 Go runtime 的分配、扫描和同步；allocation profile 主要由 gRPC/HTTP2 header 解析、client stream、context 和 metadata 构成。这说明主要成本来自额外一跳的 gRPC 收发与逐请求流对象，而不是连接建立——20 万级 profile 请求仅创建了 2 条上游连接。

Profile 同时发现代理对 incoming metadata 做了重复深拷贝：grpc-go 的 `metadata.FromIncomingContext` 已返回深拷贝，代码又执行 `MD.Copy` 并逐 slice 复制。删除重复复制后，在相近 10 秒负载下：

- 总 allocation 从约 3,303.87 MiB 降至 3,074.27 MiB，折算每请求约下降 5.8%。
- `metadata.MD.Copy` allocation 从约 189.05 MiB 降至 109.03 MiB，折算每请求约下降 41.6%。

单轮吞吐变化受共享主机噪声影响，不据此宣称确定的 QPS 优化。当前不加入 Buffer Pool 或 `sync.Pool`：热点主要在 grpc-go/runtime 内部，自建池会增加所有权和生命周期复杂度，却没有足够的应用层热点证据。
