# Stage 3 验收报告

## 对照阶段任务

- [x] 统一 Picker 接口
- [x] Round Robin
- [x] Weighted Round Robin
- [x] Least Connections
- [x] Endpoint 动态加入/删除
- [x] 10,000 次请求级算法分布测试
- [x] Endpoint 更新并发测试
- [x] README/docs 更新
- [x] 独立 Stage 3 Demo 脚本

## 已在当前环境实际执行

```bash
make test-stage3
make vet-stage3
sh -n scripts/stage3-demo.sh
```

结果：通过。

### 10,000 次分布

`TestRoundRobinDistribution10000`：三个节点的请求数差值不超过 1。

`TestWeightedRoundRobinDistribution10000`：权重 1:2:7 时，10,000 次选择结果精确为 1,000 / 2,000 / 7,000。

`TestLeastConnectionsTracksInflight`：验证选择最少 in-flight 节点，以及 `Done()` 后计数正确回收。

`TestEndpointDynamicUpdate`：删除节点后不再选择旧节点，加入新节点后能够被立即选择。

`TestRoundRobinConcurrentPickAndUpdate`：16 个并发 Pick worker 与 Endpoint 快照反复更新并行运行通过。

## 当前沙箱限制

完整 `go test ./...` / `go vet ./...` 仍会在依赖解析阶段失败，因为 Stage 1 引入的 `grpc-go` / protobuf module 没有 `go.sum`，当前沙箱无法访问 Go module proxy。该失败发生在编译 Stage 3 的 gRPC 集成路径之前。

当前环境也没有 Docker，因此无法在这里真实启动 etcd + 三 Backend 的端到端 Demo。

## 本地最终验收

在你已经验证 Stage 1/2 的正常联网开发机上执行：

```bash
go mod tidy
make dev
make test
make vet
make demo-stage3
```

也可分别测试：

```bash
./scripts/stage3-demo.sh round_robin
./scripts/stage3-demo.sh weighted_round_robin
./scripts/stage3-demo.sh least_connections
```

验收重点：

1. RR 三实例轮询分布符合预期。
2. WRR 按 `minimesh.weight` 比例分配。
3. Least Connections 在存在长请求时倾向低 in-flight 实例。
4. 注销实例后 Sidecar 无需重启且不再向该节点发请求。
5. Endpoint 动态变化过程中代理链路保持稳定。
