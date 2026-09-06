# Stage 10：Observability

## 判断与边界

推荐在这个阶段使用 Prometheus + OpenTelemetry + Jaeger + Grafana，但只作为开发演示栈。Prometheus 以 pull 模式采集 Sidecar 的稳定低基数指标；Trace 使用 W3C `traceparent` 和 OTLP，在 Jaeger 查看调用树；Grafana 提供版本化、开箱即用的定位面板。直接向 Jaeger 导出能保持 Demo 简洁；生产环境应改为 `workload -> OpenTelemetry Collector -> backend`，以获得重试、鉴权、采样和后端切换能力。

Grafana 仅用于查询和展示，Prometheus 与 Jaeger 仍分别是指标和 Trace 的事实来源。Dashboard 与数据源均通过 provisioning 文件固化，避免手工配置导致 Demo 不可复现。

## 指标

每个 Sidecar 的 `:18081/metrics` 暴露：

- `minimesh_request_total{service,method,code}`
- `minimesh_request_duration_seconds{service,method,code}`
- `minimesh_active_connections`
- `minimesh_idle_connections`
- `minimesh_connection_created_total`
- `minimesh_retry_total{service,method}`
- `minimesh_circuit_breaker_total{service,event="open_rejected"}`
- `minimesh_rate_limit_rejected_total{service,method}`

标签不包含 instance ID、请求 ID 或 trace ID，避免造成 Prometheus 高基数问题。

## Trace

`make demo-stage10` 会生成如下调用树：

```text
order.place (Java)
└── proxy.invoke (sidecar-order)
    └── inventory.check (Go)
        └── proxy.invoke (sidecar-inventory)
            └── recommendation.get (Python)
```

Sidecar 从 gRPC metadata 提取 `traceparent`，建立 span 后向上游注入新的上下文；这样两个 Sidecar 都是服务 span 的子节点而不是彼此断开的根 span。

## 运行和定位

```sh
make test-stage10
make vet-stage10
make demo-stage10
```

- Dashboard：访问 Grafana 的 `MiniMesh Stage 10 Observability`，查看请求速率/错误、P95 延迟、Retry、熔断、限流与连接数。
- 慢请求：在 Jaeger 选择 `recommendation`，按 duration 排序，使用 `ORDER_SKU=slow` 重跑客户端。
- 错误服务：使用 `ORDER_SKU=missing`，在 trace 中查看 `recommendation.get` 的 NOT_FOUND 及上游传播。
- Retry / 熔断 / 限流：在 Prometheus 查询 `minimesh_retry_total`、`minimesh_circuit_breaker_total`、`minimesh_rate_limit_rejected_total`。这些是 Sidecar 数据面决策的事实来源。
- 调用链：访问 Jaeger 的 `order` 服务，打开 `order.place` trace。

Grafana: <http://localhost:3000/d/minimesh-stage10>；Prometheus: <http://localhost:9090>；Jaeger: <http://localhost:16686>。
