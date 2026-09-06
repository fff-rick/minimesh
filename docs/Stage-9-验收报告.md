# Stage 9 验收报告：跨语言接入

| 验收项 | 状态 | 证据 |
|---|---|---|
| Proto 兼容 | PASS | Java、Go、Python 都从 `commerce.proto` 生成业务类型；两次 Sidecar 转发分别由下一语言解析。 |
| Metadata | PASS | `x-request-id` 从 Java 入口到 Python 响应回显。 |
| Deadline | PASS | Java 的入站 deadline 限制到 Inventory 的调用，Go 使用同一 context 完成第二跳调用。 |
| Trace Context | PASS | `traceparent` 作为标准 metadata 逐跳透传，并由 Python 回显。 |
| Error Mapping | PASS | Python `NOT_FOUND` / `DEADLINE_EXCEEDED` 以原生 gRPC status 回到 Java。 |
| 完整链路 | PASS | `make demo-stage9` 启动 Java Order → Sidecar → Go Inventory → Sidecar → Python Recommendation。 |

## 验证命令

```bash
make proto-local
go test ./...
go vet ./...
make demo-stage9
```
