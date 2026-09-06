FROM rust:1.89.0-bookworm AS build
WORKDIR /workspace
COPY examples/rust/validation-agent/Cargo.toml ./Cargo.toml
COPY examples/rust/validation-agent/src ./src
RUN cargo test --release && cargo build --release

FROM debian:bookworm-slim
COPY --from=build /workspace/target/release/minimesh-validation-agent /validation-agent
USER 65532:65532
ENTRYPOINT ["/validation-agent"]
