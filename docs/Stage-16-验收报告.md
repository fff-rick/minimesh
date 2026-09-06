# Stage 16 验收报告：故障注入与最终验收

## 结论

通过。2026-09-06 在本机 Docker 环境完成 11 项最终场景，机器生成报告全部为 PASS。
最终拓扑包含 Go Control Plane、etcd、4 个 Go Sidecar、Java Order、Go Inventory、
Python Recommendation、Rust Validation Agent、Prometheus、Grafana 和 Jaeger。

## 自动检查

```text
go test ./...                                      PASS
go vet ./...                                       PASS
docker compose ... config                         PASS
Rust cargo test --release（镜像构建内）            PASS
MINIMESH_SKIP_BUILD=1 KEEP_STAGE16=1 make demo-stage16 PASS
```

## 故障矩阵实测

| 场景 | 结果 | 关键证据 |
| --- | --- | --- |
| 跨语言基线 | PASS | Java → Go → Python 返回 `recommended-with-book` |
| Kill Backend | PASS | 两实例停一台时 20 次调用成功 16 次，随后实例恢复 |
| Kill Sidecar | PASS | 停机为 `Unavailable`，重启并同步后恢复 |
| Kill Control Plane | PASS | 4/4 cached calls 成功，重启后继续成功 |
| etcd 短暂不可用 | PASS | 4/4 cached calls 成功；恢复后新 service 被发现 |
| Network Delay | PASS | 1 秒单向延迟触发 `DeadlineExceeded`，移除后 2/2 成功 |
| Packet Loss | PASS | netem 100% loss 触发 `DeadlineExceeded`，qdisc 清理后 2/2 成功 |
| Backend 80% Error | PASS | 12/12 失败；retry=24，breaker rejection=32 |
| Backend Slow | PASS | 3 秒 Backend 被 750ms Sidecar deadline 截断 |
| Rate Limit | PASS | 并发 20 次中 18 次 `ResourceExhausted`，counter=18 |
| Trace / Metrics / Agent | PASS | Prometheus、Jaeger、Grafana 与 Rust Agent 均通过 |

原始证据位于 `artifacts/stage16/FAILURE_TEST_REPORT.md` 和
`artifacts/stage16/pumba.log`。Pumba 日志同时记录 qdisc 的开始与停止，避免把连接失败
误报为 packet loss。

## 验收中修复的问题

第一次运行发现多步 shell function 的最终恢复命令会覆盖中间断言失败码，造成 false
positive；已改为显式保存并传播每个注入/恢复断言状态。Packet-loss 场景改为等待 Pumba
activation log 后持续探测实际 impairment，不再依赖固定 sleep。80% Backend 在每次场景前
重启，以重置确定性请求序列并保证可重复。

## 未宣称能力

- 单节点 etcd 只验证数据面 cache，不代表 quorum 或灾难恢复；
- Docker 故障实验不代表多节点 Kubernetes、跨 AZ 或网络设备故障；
- Stage 12 Rust 数据面仍为 Deferred；Rust Agent 只读验证最终拓扑健康；
- Pumba/netem 需要 Docker Socket 高权限，只能在可信隔离环境短时运行。
