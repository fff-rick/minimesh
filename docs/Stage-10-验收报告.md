# Stage 10 验收报告：Observability

## 验收项

| 项目 | 验证方式 |
| --- | --- |
| Prometheus 指标 | 两个 Sidecar 的 `/metrics` 由 Prometheus 每 2 秒抓取。 |
| 请求与延迟 | `minimesh_request_total` 和 `minimesh_request_duration_seconds` 以 service、method、gRPC code 聚合。 |
| 韧性事件 | 重试、熔断拒绝和限流拒绝分别由独立 Counter 记录。 |
| 调用链 | Java Order、两个 Sidecar、Go Inventory、Python Recommendation 都导出 OTLP span。 |
| Dashboard | Grafana 自动 provision Prometheus、Jaeger 数据源和 Stage 10 定位面板。 |
| 可独立运行 | `make demo-stage10` 构建并运行完整 Docker Demo。 |

## 本地校验

```sh
make test-stage10
make vet-stage10
make demo-stage10
```

`demo-stage10` 会检查 Sidecar 指标、Prometheus 查询 API、Grafana Dashboard，并断言同一条 `order` Trace 包含 `order`、`sidecar-order`、`inventory`、`sidecar-inventory`、`recommendation` 五个服务。
