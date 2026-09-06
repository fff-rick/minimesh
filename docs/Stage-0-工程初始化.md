# Stage 0：工程初始化交付说明

## 范围

本阶段只建立 MiniMesh 工程骨架和开发基础设施，不实现 Stage 1 及之后的业务能力。

## 已完成

- 建立 `cmd/control-plane` 与 `cmd/sidecar` Go 可执行入口骨架。
- 建立 `internal`、`pkg`、`api/proto`、`api/gen`、`examples`、`deploy`、`tests`、`scripts`、`docs` 目录。
- 初始化 Go Module。
- 提供 Makefile，统一开发、Proto 生成、测试、静态检查命令。
- 提供 Docker Compose 单节点 etcd 开发环境与健康检查。
- 提供 Docker 化 Protobuf 生成流程，并保留本地 `protoc` 生成入口供 CI 使用。
- 提供 GitHub Actions 基础 CI：Proto 生成一致性检查、`go test ./...`、`go vet ./...`。
- 提供 Stage 0 最小单元测试与两个组件骨架运行入口。

## 一键开发环境

```bash
make dev
```

该命令执行：启动 etcd → 轮询 etcd endpoint health → 健康后返回成功。

## 验收命令

```bash
make dev
make etcd-health
make proto
make test
make vet
```

预期：etcd 健康；Proto 能生成 Go 文件；所有 Go 测试与 vet 通过。

## 设计边界

`bootstrap.proto` 只承担 Stage 0 的 Protobuf 工具链验证，不提前固化 Stage 1 RPC Proxy 或 Stage 8 Control Stream 的正式协议。etcd 目前也只作为开发依赖存在，注册、Lease、Watch 等功能留到 Stage 2 实现。
