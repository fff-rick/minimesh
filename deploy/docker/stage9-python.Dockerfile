FROM python:3.13-slim
WORKDIR /app
COPY api/proto /proto
COPY examples/python/recommendation-service/server.py /app/server.py
RUN pip install --no-cache-dir grpcio==1.83.1 grpcio-tools==1.83.1 opentelemetry-api==1.44.0 opentelemetry-sdk==1.44.0 opentelemetry-exporter-otlp-proto-http==1.44.0 \
 && mkdir -p /app/generated \
 && python -m grpc_tools.protoc -I/proto --python_out=/app/generated --grpc_python_out=/app/generated /proto/minimesh/v1/commerce.proto
ENV PYTHONPATH=/app/generated
ENTRYPOINT ["python", "/app/server.py"]
