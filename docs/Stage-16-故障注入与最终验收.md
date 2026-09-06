# Stage 16：故障注入与最终验收

## 完成范围

Stage 16 将已有阶段组合成一个可重复的最终验收，而不新增生产治理机制：

- Java Order → Go Inventory → Python Recommendation 的业务基线；
- Order、Inventory、Recommendation 和 Chaos Probe 共 4 个 Go Sidecar；
- Go Control Plane、etcd、Prometheus、Grafana、Jaeger；
- Rust Validation Agent，持续检查控制面和观测后端；
- Backend、Sidecar、Control Plane、etcd、delay、packet loss、80% error、slow response；
- 对 discovery/cache、retry、LB、circuit breaker、rate limit、config recovery、trace、metrics
  做自动断言。

运行：

```sh
make test-stage16
make vet-stage16
make demo-stage16
```

报告生成到 `artifacts/stage16/FAILURE_TEST_REPORT.md`。设置 `KEEP_STAGE16=1` 可在验收后
保留容器，便于查看 Grafana 和 Jaeger；默认会回收环境。

## 验收口径

| 场景 | 稳态/恢复断言 | 治理证据 |
| --- | --- | --- |
| Kill Backend | 双实例中删除一台后仍有至少 75% 请求成功，恢复后重新加入 | LB、retry |
| Kill Sidecar | 调用失败为 `Unavailable`，Sidecar 重启并完成 snapshot 后恢复 | discovery、control stream |
| Kill Control Plane | 已有 Sidecar 使用本地 cache 继续调用，控制面恢复后重连 | cache、config recovery |
| etcd unavailable | 数据面使用 cache；恢复后注册新 service 并被发现 | etcd isolation、full sync |
| Network Delay | 1 秒单向延迟触发总请求 timeout，移除 toxic 后恢复 | timeout、metrics |
| Packet Loss | 独立 Backend relay 上注入 100% egress loss，结束后恢复 | netem、retry |
| Backend 80% Error | 出现失败、retry 与 breaker rejection 指标 | retry、circuit breaker |
| Backend Slow | 3 秒响应被 750ms Sidecar deadline 截断 | timeout |
| Rate Limit | 并发突发出现 `ResourceExhausted` 且 counter 增长 | token bucket |
| Observability | Prometheus query、Jaeger trace、Grafana API、Rust Agent 全部健康 | metrics、trace |

## 明确边界

- 这是一套本地 Docker 验收，不代表多节点、跨 AZ 或生产灾难恢复演练；
- etcd 是单节点 Demo，验证的是 Sidecar cache 行为，不宣称 etcd quorum 容错；
- Packet loss 依赖 Linux netem 和 Docker Socket；Docker Desktop/WSL2 由 Linux VM 执行；
- Stage 14 L4 透明代理与 Stage 15 mTLS 已由各自阶段验收，本脚本使用显式 ProxyService
  路径，避免把两个独立实验面的权限和故障变量混在一次测试中；
- Rust Agent 是只读最终验收组件，Stage 12 的 Rust 数据面重写仍然延期。
