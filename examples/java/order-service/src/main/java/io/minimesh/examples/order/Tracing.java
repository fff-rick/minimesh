package io.minimesh.examples.order;

import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.context.Context;
import io.opentelemetry.context.propagation.ContextPropagators;
import io.opentelemetry.context.propagation.TextMapGetter;
import io.opentelemetry.context.propagation.TextMapSetter;
import io.opentelemetry.api.trace.propagation.W3CTraceContextPropagator;
import io.opentelemetry.exporter.otlp.trace.OtlpGrpcSpanExporter;
import io.opentelemetry.sdk.OpenTelemetrySdk;
import io.opentelemetry.sdk.resources.Resource;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.SdkTracerProviderBuilder;
import io.opentelemetry.sdk.trace.export.BatchSpanProcessor;
import io.grpc.Metadata;

final class Tracing {
  private static final TextMapGetter<Metadata> GETTER = new TextMapGetter<>() {
    public Iterable<String> keys(Metadata c) { return c.keys(); }
    public String get(Metadata c, String key) { return c.get(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER)); }
  };
  private static final TextMapSetter<Metadata> SETTER = (c, key, value) -> {
    Metadata.Key<String> metadataKey = Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER);
    c.removeAll(metadataKey); c.put(metadataKey, value);
  };

  static void init() {
    String endpoint = System.getenv().getOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "");
    String name = System.getenv().getOrDefault("OTEL_SERVICE_NAME", "order");
    SdkTracerProviderBuilder provider = SdkTracerProvider.builder()
      .setResource(Resource.getDefault().merge(Resource.create(Attributes.of(AttributeKey.stringKey("service.name"), name))));
    if (!endpoint.isBlank()) provider.addSpanProcessor(BatchSpanProcessor.builder(OtlpGrpcSpanExporter.builder().setEndpoint(endpoint).build()).build());
    OpenTelemetrySdk.builder().setTracerProvider(provider.build())
      .setPropagators(ContextPropagators.create(W3CTraceContextPropagator.getInstance())).buildAndRegisterGlobal();
  }
  static Context extract(Metadata headers) { return GlobalOpenTelemetry.getPropagators().getTextMapPropagator().extract(Context.current(), headers, GETTER); }
  static void inject(Metadata headers) { GlobalOpenTelemetry.getPropagators().getTextMapPropagator().inject(Context.current(), headers, SETTER); }
  static Span serverSpan(Metadata headers) { return GlobalOpenTelemetry.getTracer("minimesh/order").spanBuilder("order.place").setParent(extract(headers)).setSpanKind(SpanKind.SERVER).startSpan(); }
}
