SHELL := /bin/sh
COMPOSE := docker compose -f deploy/docker/docker-compose.yml

.PHONY: help dev dev-up dev-down dev-status etcd-health proto proto-local test test-stage2 test-stage3 test-stage4 test-stage5 test-stage6 test-stage7 test-stage8 test-stage9 test-stage10 test-stage11 test-stage13 test-stage14 test-stage15 test-stage16 vet vet-stage2 vet-stage3 vet-stage4 vet-stage5 vet-stage6 vet-stage7 vet-stage8 vet-stage9 vet-stage10 vet-stage11 vet-stage13 vet-stage14 vet-stage15 vet-stage16 check run-control-plane run-sidecar run-backend run-client run-client-service demo demo-stage1 demo-stage2 demo-stage3 demo-stage4 demo-stage5 demo-stage6 demo-stage7 demo-stage8 demo-stage9 demo-stage10 demo-stage13 demo-stage14 demo-stage15 demo-stage16 stage13-status stage13-down benchmark-stage4 benchmark-stage11 clean

help:
	@echo "MiniMesh Stage 16 commands:"
	@echo "  make dev                Start etcd and wait until it is healthy"
	@echo "  make proto              Generate Go protobuf/gRPC code in Docker"
	@echo "  make proto-local        Generate with local protoc + Go plugins"
	@echo "  make test               Run all Go tests"
	@echo "  make test-stage2        Run Stage 2 discovery/registry tests"
	@echo "  make test-stage3        Run Stage 3 load-balancer tests"
	@echo "  make test-stage4        Run Stage 4 connection-pool tests"
	@echo "  make test-stage5        Run Stage 5 retry/timeout core tests"
	@echo "  make test-stage6        Run Stage 6 circuit-breaker core tests"
	@echo "  make test-stage7        Run Stage 7 token-bucket/rule tests"
	@echo "  make test-stage8        Run Stage 8 control-stream tests"
	@echo "  make vet                Run go vet for the full project"
	@echo "  make vet-stage2         Vet Stage 2 packages"
	@echo "  make vet-stage3         Vet Stage 3 packages"
	@echo "  make vet-stage4         Vet Stage 4 connection-pool package"
	@echo "  make vet-stage5         Vet Stage 5 retry/timeout core package"
	@echo "  make vet-stage6         Vet Stage 6 circuit-breaker core package"
	@echo "  make vet-stage7         Vet Stage 7 rate-limit core package"
	@echo "  make vet-stage8         Vet Stage 8 control-stream package"
	@echo "  make run-control-plane  Start Registry API on :17070"
	@echo "  make run-sidecar        Start sidecar with etcd discovery"
	@echo "  make run-backend        Start Go echo backend on :19090"
	@echo "  make run-client         Call backend by explicit address"
	@echo "  make run-client-service Call service://echo through discovery"
	@echo "  make demo-stage1        Run the previous Stage 1 demo"
	@echo "  make demo-stage2        Run dynamic discovery demo"
	@echo "  make demo-stage3        Run RR end-to-end load-balancing demo"
	@echo "  make benchmark-stage4   Compare No Pool vs Connection Pool"
	@echo "  make demo-stage5        Run retry + timeout end-to-end demo"
	@echo "  make demo-stage6        Run circuit-breaker state transition demo"
	@echo "  make demo-stage7        Run local rate-limit load demo"
	@echo "  make demo-stage8        Run control-stream reconnect demo"
	@echo "  make test-stage9        Validate Stage 9 Go contract and proxy tests"
	@echo "  make demo-stage9        Run the Java -> Go -> Python mesh demo (Docker)"
	@echo "  make demo-stage10       Run the Prometheus + Jaeger observability demo"
	@echo "  make test-stage11       Run Stage 11 benchmark-related tests"
	@echo "  make vet-stage11        Vet Stage 11 benchmark-related packages"
	@echo "  make benchmark-stage11  Compare direct gRPC vs Mesh and capture pprof"
	@echo "  make test-stage13       Test sidecar registration and existing contracts"
	@echo "  make vet-stage13        Vet Stage 13 Go packages"
	@echo "  make demo-stage13       Build and deploy the cross-language demo to kind"
	@echo "  make stage13-status     Show Stage 13 Kubernetes resources"
	@echo "  make stage13-down       Delete the local Stage 13 kind cluster"
	@echo "  make test-stage14       Test transparent TCP proxy and Stage 14 contracts"
	@echo "  make vet-stage14        Vet Stage 14 packages"
	@echo "  make demo-stage14       Validate iptables outbound interception in kind"
	@echo "  make test-stage15       Test workload identity, RBAC, and secure proxy contracts"
	@echo "  make vet-stage15        Vet Stage 15 packages"
	@echo "  make demo-stage15       Validate sidecar mTLS, denial, and cert rotation"
	@echo "  make test-stage16       Test final-validation helpers and all Go packages"
	@echo "  make vet-stage16        Vet the complete final-validation Go surface"
	@echo "  make demo-stage16       Run fault injection and generate the final report"

dev: dev-up
	@./scripts/wait-etcd.sh
	@echo "MiniMesh development environment is ready."

dev-up:
	@$(COMPOSE) up -d etcd

dev-down:
	@$(COMPOSE) down

dev-status:
	@$(COMPOSE) ps

etcd-health:
	@$(COMPOSE) exec -T etcd etcdctl --endpoints=http://127.0.0.1:2379 endpoint health

proto:
	@$(COMPOSE) --profile tools run --rm --build proto

proto-local:
	@./scripts/proto-generate.sh

test:
	@go test ./...

test-stage2:
	@go test ./internal/discovery ./internal/registry ./internal/etcdhttp

test-stage3:
	@go test ./internal/loadbalance

test-stage4:
	@go test ./internal/connectionpool

test-stage5:
	@go test ./internal/resilience -run "Retry|Backoff|Budget|Sleep|Do"

test-stage6:
	@go test ./internal/resilience -run CircuitBreaker

test-stage7:
	@go test ./internal/resilience -run RateLimit

test-stage8:
	@go test ./internal/controlstream

test-stage9:
	@go test ./api/gen/minimesh/v1 ./internal/proxy ./examples/go/inventory-server

test-stage10:
	@go test ./internal/observability ./internal/proxy ./cmd/sidecar ./examples/go/inventory-server

test-stage11:
	@go test ./internal/observability ./internal/proxy ./internal/connectionpool ./cmd/sidecar ./examples/go/echo-server

test-stage13:
	@go test ./internal/registration ./internal/controlstream ./internal/proxy ./cmd/sidecar

test-stage14:
	@go test ./internal/transparentproxy ./internal/proxy ./examples/go/inventory-server ./cmd/sidecar

test-stage15:
	@go test ./internal/meshsecurity ./internal/proxy ./internal/registration ./cmd/sidecar

test-stage16:
	@go test ./...

vet:
	@go vet ./...

vet-stage2:
	@go vet ./internal/discovery ./internal/registry ./internal/etcdhttp

vet-stage3:
	@go vet ./internal/loadbalance

vet-stage4:
	@go vet ./internal/connectionpool

vet-stage5:
	@go vet ./internal/resilience

vet-stage6:
	@go vet ./internal/resilience

vet-stage7:
	@go vet ./internal/resilience

vet-stage8:
	@go vet ./internal/controlstream

vet-stage9:
	@go vet ./examples/go/inventory-server ./internal/proxy

vet-stage10:
	@go vet ./internal/observability ./examples/go/inventory-server ./internal/proxy ./cmd/sidecar

vet-stage11:
	@go vet ./internal/observability ./internal/proxy ./internal/connectionpool ./cmd/sidecar ./examples/go/echo-server

vet-stage13:
	@go vet ./internal/registration ./internal/controlstream ./internal/proxy ./cmd/sidecar

vet-stage14:
	@go vet ./internal/transparentproxy ./internal/proxy ./examples/go/inventory-server ./cmd/sidecar

vet-stage15:
	@go vet ./internal/meshsecurity ./internal/proxy ./internal/registration ./cmd/sidecar

vet-stage16:
	@go vet ./...

check: proto test vet

run-control-plane:
	@go run ./cmd/control-plane --listen :17070 --etcd http://127.0.0.1:2379

run-sidecar:
	@go run ./cmd/sidecar --listen :18080 --etcd http://127.0.0.1:2379 --lb round_robin

run-backend:
	@go run ./examples/go/echo-server --listen :19090

run-client:
	@go run ./examples/go/client --sidecar 127.0.0.1:18080 --backend 127.0.0.1:19090 --message hello

run-client-service:
	@go run ./examples/go/client --sidecar 127.0.0.1:18080 --service echo --message hello

demo: demo-stage8

demo-stage1:
	@./scripts/stage1-demo.sh

demo-stage2:
	@./scripts/stage2-demo.sh

demo-stage3:
	@./scripts/stage3-demo.sh round_robin

demo-stage4: benchmark-stage4

demo-stage5:
	@./scripts/stage5-demo.sh

demo-stage6:
	@./scripts/stage6-demo.sh

demo-stage7:
	@./scripts/stage7-demo.sh

demo-stage8:
	@./scripts/stage8-demo.sh

demo-stage9:
	@./scripts/stage9-demo.sh

demo-stage10:
	@./scripts/stage10-demo.sh

demo-stage13:
	@./scripts/stage13-demo.sh

demo-stage14:
	@./scripts/stage14-demo.sh

demo-stage15:
	@./scripts/stage15-demo.sh

demo-stage16:
	@./scripts/stage16-demo.sh

stage13-status:
	@docker exec minimesh-stage13-control-plane kubectl --kubeconfig=/etc/kubernetes/admin.conf get pods,services -n minimesh

stage13-down:
	@kind delete cluster --name minimesh-stage13

benchmark-stage4:
	@./scripts/stage4-benchmark.sh

benchmark-stage11:
	@./scripts/stage11-benchmark.sh

clean:
	@rm -rf api/gen/minimesh/v1/*.pb.go
