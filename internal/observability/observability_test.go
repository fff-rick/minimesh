package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestMetricsExposeStage10Families(t *testing.T) {
	metrics := NewMetrics()
	metrics.Requests.WithLabelValues("inventory", "/inventory.Check", "OK").Inc()
	metrics.Duration.WithLabelValues("inventory", "/inventory.Check", "OK").Observe(0.05)
	metrics.Retries.WithLabelValues("inventory", "/inventory.Check").Inc()
	metrics.CircuitBreaker.WithLabelValues("inventory", "open_rejected").Inc()
	metrics.RateLimitRejected.WithLabelValues("inventory", "/inventory.Check").Inc()
	metrics.RegisterConnectionStats(func() float64 { return 3 }, func() float64 { return 2 }, func() float64 { return 5 })

	families, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	got := make(map[string]bool, len(families))
	for _, family := range families {
		got[family.GetName()] = true
	}
	for _, name := range []string{
		"minimesh_request_total",
		"minimesh_request_duration_seconds",
		"minimesh_active_connections",
		"minimesh_idle_connections",
		"minimesh_connection_created_total",
		"minimesh_retry_total",
		"minimesh_circuit_breaker_total",
		"minimesh_rate_limit_rejected_total",
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		if !got[name] {
			t.Errorf("metric family %q was not gathered", name)
		}
	}
}

func TestInitTracerWithoutEndpointInstallsW3CPropagation(t *testing.T) {
	shutdown, err := InitTracer(context.Background(), "test-service", "")
	if err != nil {
		t.Fatalf("InitTracer() error = %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
	})
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(trace.ContextWithSpanContext(context.Background(), spanContext), carrier)
	if got := carrier.Get("traceparent"); got == "" {
		t.Fatal("W3C traceparent was not injected")
	}
}
