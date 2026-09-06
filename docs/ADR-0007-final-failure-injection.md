# ADR-0007：最终故障注入采用分层工具

## 状态

Accepted（仅用于本地/CI 验收，不进入生产数据面）。

## 判断

有条件推荐 `Docker Compose + Toxiproxy + Pumba/netem`。Stage 16 需要的是可重复、可判定
成功或失败的最终验收，不是建设通用混沌工程平台。

## 候选方案

| 方案 | 优点 | 代价 | 结论 |
| --- | --- | --- | --- |
| Chaos Mesh | Kubernetes 原生，故障类型与编排能力完整 | 增加 CRD、controller、daemon 和集群权限；本阶段只有固定场景 | 暂不引入 |
| LitmusChaos | 工作流、实验库、报告能力完整 | 学习和运维成本最高，超出单机验收边界 | 暂不引入 |
| 只用 Toxiproxy | API 简单、确定性好、无需特权 | 2.12.0 可做 latency/reset，但不能注入真实 L3 packet loss | 不完整 |
| Toxiproxy + Pumba | 确定性链路故障和真实 netem packet loss 兼得 | Pumba 短暂访问 Docker Socket，权限高 | 采用，并限制生命周期 |

## 决策

- Toxiproxy 固定为 2.12.0，分别隔离 Control/etcd 与 Backend 故障域；
- Pumba 固定为 1.1.7 且镜像使用 digest；nettools 也锁定本次验证的 digest，仅在
  packet-loss 场景启动，结束后自动清理 qdisc；
- 容器 kill/start 使用 Compose，应用错误率和慢响应使用项目自己的确定性 faulty backend；
- 每个场景必须包含稳态假设、注入、自动断言和恢复断言，结果写入机器生成报告。

## 安全与维护边界

Pumba 需要读取 Docker Socket，等价于主机级高权限。它不能作为常驻服务，也不能在
不受信任的共享 Runner 上运行。若未来故障实验成为持续的 Kubernetes 能力，再迁移到
Chaos Mesh，并用 RBAC、namespace selector 和实验审批限制 blast radius。

Rust Stage 12 仍保持 Deferred。最终拓扑加入的是只读 Rust Validation Agent，而不是
第二套 Sidecar 数据面；这满足异构最终 Demo，同时避免为展示目的复制治理逻辑。
