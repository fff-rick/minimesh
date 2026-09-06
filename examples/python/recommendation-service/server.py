import os
import time
from concurrent import futures

import grpc
from minimesh.v1 import commerce_pb2, commerce_pb2_grpc
from opentelemetry import propagate, trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor


def metadata(context, key):
    return next((value for name, value in context.invocation_metadata() if name == key), "")


class Recommendations(commerce_pb2_grpc.RecommendationServiceServicer):
    def Recommend(self, request, context):
        carrier = {name: value for name, value in context.invocation_metadata()}
        parent = propagate.extract(carrier)
        with trace.get_tracer("minimesh/recommendation").start_as_current_span("recommendation.get", context=parent) as span:
            span.set_attribute("recommendation.sku", request.sku)
            if request.sku == "missing":
                context.abort(grpc.StatusCode.NOT_FOUND, "recommendation not found")
            if request.sku == "slow":
                time.sleep(0.2)
            response_headers = {}
            propagate.inject(response_headers)
            return commerce_pb2.RecommendationResponse(
                recommendation=f"recommended-with-{request.sku}",
                trace_id=response_headers.get("traceparent", ""),
                request_id=metadata(context, "x-request-id"),
            )


def configure_tracing():
    service_name = os.getenv("OTEL_SERVICE_NAME", "recommendation")
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
    provider = TracerProvider(resource=Resource.create({SERVICE_NAME: service_name}))
    if endpoint:
        provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=endpoint)))
    trace.set_tracer_provider(provider)


def main():
    configure_tracing()
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    commerce_pb2_grpc.add_RecommendationServiceServicer_to_server(Recommendations(), server)
    server.add_insecure_port(f"[::]:{os.getenv('RECOMMENDATION_PORT', '19092')}")
    server.start()
    server.wait_for_termination()


if __name__ == "__main__":
    main()
