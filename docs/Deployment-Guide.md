# MiniMesh Deployment Guide

## 本地最终验收

要求：Go、Docker Engine/Compose、curl；packet-loss 场景要求 Linux 容器运行时，并会短暂
挂载 Docker Socket 给 Pumba。

```sh
make test-stage16
make vet-stage16
make demo-stage16
```

保留环境进行人工检查：

```sh
KEEP_STAGE16=1 make demo-stage16
docker compose \
  -f deploy/docker/docker-compose.yml \
  -f deploy/docker/docker-compose.stage10.yml \
  -f deploy/docker/docker-compose.stage16.yml \
  --profile stage9 --profile stage16 ps
```

入口：Grafana `http://localhost:3000/d/minimesh-stage10`、Jaeger
`http://localhost:16686`、Prometheus `http://localhost:9090`、Rust Agent
`http://localhost:39100`。

## Kubernetes Demo

```sh
make demo-stage13  # 双容器 Pod 与 Helm Test
make demo-stage14  # iptables 透明代理
make demo-stage15  # Sidecar mTLS、RBAC、证书轮换
```

这些脚本使用本地 kind 集群。`make stage13-down` 删除集群。内置单节点 etcd 使用
`emptyDir`，仅用于演示；生产部署必须使用 3/5 节点 quorum、持久卷、备份和恢复演练。

## 生产差距清单

- Control Plane 多副本与 leader/无状态读路径；
- etcd TLS、auth、quorum、PV、snapshot/restore；
- SPIRE/cert-manager 等工作负载身份和信任根轮换；
- PodDisruptionBudget、拓扑分散、NetworkPolicy、资源与 autoscaling；
- OTel Collector、遥测采样、留存和基于 SLO 的告警；
- 在隔离环境执行混沌实验，限制 Docker Socket/NET_ADMIN 和目标 selector。
