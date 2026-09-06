# MiniMesh Final Demo

一条命令运行 Stage 16：

```sh
make demo-stage16
```

Demo 先验证 Java → Go → Python 业务链和 4 个 Go Sidecar，再依次注入 Backend、Sidecar、
Control Plane、etcd、延迟、丢包、80% 错误和慢响应故障。Rust Validation Agent 只读检查
Control Plane、Prometheus 和 Jaeger。每个故障都有自动断言和恢复断言；任一不满足，命令
返回非零。

产物：`artifacts/stage16/FAILURE_TEST_REPORT.md`。使用 `KEEP_STAGE16=1` 保留环境，完成
检查后可运行：

```sh
docker compose \
  -f deploy/docker/docker-compose.yml \
  -f deploy/docker/docker-compose.stage10.yml \
  -f deploy/docker/docker-compose.stage16.yml \
  --profile stage9 --profile stage16 down --remove-orphans
```

注意：packet-loss 使用 Pumba/netem，需访问 Docker Socket；只能在可信、隔离的开发或 CI
宿主机运行。
