# Stage 3：负载均衡

## 目标

在 Stage 2 动态服务发现基础上，为 `service://<name>` 请求增加多实例负载均衡能力，并保持 Endpoint 动态更新期间请求稳定。

## 设计

新增 `internal/loadbalance`：

- `Picker`：统一算法接口，包含 `Update([]Endpoint)` 与 `Pick()`。
- `RoundRobinPicker`：原子快照 + 原子序号，读路径无锁。
- `WeightedRoundRobinPicker`：平滑加权轮询（Smooth WRR），权重来自 Endpoint metadata `minimesh.weight`，缺省为 1。
- `LeastConnectionsPicker`：选择当前 in-flight 最少的实例；`PickResult.Done()` 在请求结束后归还活跃计数。
- `Resolver`：按 service 保存 Picker，并在 Endpoint 指纹变化时热更新 Picker。

Sidecar 新增 `--lb` 参数：

- `round_robin`
- `weighted_round_robin`
- `least_connections`

Proxy 在 resolver 支持 Stage 3 `Pick` 合约时，会把 `Done()` 生命周期绑定到完整的上游调用；这保证 Least Connections 统计的是实际正在执行的请求，而不是瞬时选择次数。

## 阶段边界

Stage 3 不实现 Connection Pool。每次请求仍沿用 Stage 1 的 Dial/Close 行为，正式连接复用留给 Stage 4。

Stage 3 不修改 Stage 2 的 etcd Full Sync + Watch 模型，也不提前实现 Stage 8 Control Stream。

## 使用

默认 RR：

```bash
make demo-stage3
```

手动切换算法：

```bash
go run ./cmd/sidecar --listen :18080 --lb weighted_round_robin
go run ./cmd/sidecar --listen :18080 --lb least_connections
```

Weighted RR 的实例注册示例：

```json
{
  "endpoint": {
    "service": "echo",
    "instance_id": "echo-3",
    "address": "127.0.0.1:19093",
    "metadata": {
      "minimesh.weight": "7"
    }
  }
}
```
