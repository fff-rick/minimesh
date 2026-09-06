# ADR-0001：Stage 0 本地开发与 Protobuf 工具链

## 问题

Stage 0 需要保证 etcd 可一键启动，并让 Protobuf 生成过程尽量不依赖开发者本机安装特定版本的 `protoc` 和插件。

## 候选方案

1. 所有工具均要求开发者本机安装。
2. 使用 Docker Compose 管理 etcd，同时用 Docker 镜像固定 Protobuf 编译工具链。
3. 在项目中提交平台相关的工具二进制。

## 选择

采用方案 2，并保留 `make proto-local` 给 CI 或已安装 `protoc` 的开发环境使用。

## 理由

- etcd 本身天然适合容器化，Stage 0 无需把安装方式绑定到宿主机。
- Docker 化 Proto 工具链可以固定 `protoc-gen-go` 版本，降低开发环境差异。
- CI 直接安装 `protoc` 后走同一生成脚本，避免维护两套生成参数。

## 代价

- 本地完整开发流程依赖 Docker Compose。
- 首次构建 Proto 工具镜像需要下载基础镜像和 Go 插件。

## 未来调整条件

如果后续 Proto 数量显著增多、需要跨语言代码生成或 lint/breaking-change 检查，可评估迁移到 Buf，并通过新的 ADR 记录决策。
