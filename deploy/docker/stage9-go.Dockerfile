FROM golang:1.23.2-bookworm AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
COPY examples/go ./examples/go
RUN go build -buildvcs=false -o /out/control-plane ./cmd/control-plane \
 && go build -buildvcs=false -o /out/sidecar ./cmd/sidecar \
 && go build -buildvcs=false -o /out/inventory ./examples/go/inventory-server \
 && go build -buildvcs=false -o /out/client ./examples/go/client \
 && go build -buildvcs=false -o /out/probe ./examples/go/probe-client \
 && go build -buildvcs=false -o /out/faulty-echo ./examples/go/faulty-echo-server

FROM debian:bookworm-slim AS control-plane
COPY --from=build /out/control-plane /control-plane
ENTRYPOINT ["/control-plane"]

FROM debian:bookworm-slim AS sidecar
COPY --from=build /out/sidecar /sidecar
ENTRYPOINT ["/sidecar"]

FROM debian:bookworm-slim AS inventory
COPY --from=build /out/inventory /inventory
ENTRYPOINT ["/inventory"]

FROM debian:bookworm-slim AS client
COPY --from=build /out/client /client
ENTRYPOINT ["/client"]

FROM debian:bookworm-slim AS probe
COPY --from=build /out/probe /probe
ENTRYPOINT ["/probe"]

FROM debian:bookworm-slim AS faulty-echo
COPY --from=build /out/faulty-echo /faulty-echo
ENTRYPOINT ["/faulty-echo"]

# Stage 16 deliberately builds the Go source once. Compose otherwise treats
# every service/target pair as an independent build and repeats module fetches.
FROM debian:bookworm-slim AS stage16-go
COPY --from=build /out/ /usr/local/bin/
