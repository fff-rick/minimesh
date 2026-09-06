#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

protoc \
  -I api/proto \
  --go_out=. \
  --go_opt=module=github.com/minimesh/minimesh \
  --go-grpc_out=. \
  --go-grpc_opt=module=github.com/minimesh/minimesh \
  api/proto/minimesh/v1/bootstrap.proto \
  api/proto/minimesh/v1/commerce.proto \
  api/proto/minimesh/v1/control.proto \
  api/proto/minimesh/v1/proxy.proto

echo "protobuf + gRPC generation complete"
