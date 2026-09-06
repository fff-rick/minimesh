// Package observability contains the small, process-local telemetry setup used
// by the MiniMesh demo. Production deployments should export through an OTel
// Collector rather than coupling each workload directly to a backend.
package observability

import (
	"context"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Metrics is deliberately scoped to one sidecar process so every metric has a
// stable service label and no global-registration conflicts in tests.
type Metrics struct {
	Requests          *prometheus.CounterVec
	Duration          *prometheus.HistogramVec
	Retries           *prometheus.CounterVec
	CircuitBreaker    *prometheus.CounterVec
	RateLimitRejected *prometheus.CounterVec
	registry          *prometheus.Registry
}

func NewMetrics() *Metrics {
	m := &Metrics{
		Requests:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "minimesh_request_total", Help: "Total proxy requests."}, []string{"service", "method", "code"}),
		Duration:          prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "minimesh_request_duration_seconds", Help: "End-to-end proxy request duration."}, []string{"service", "method", "code"}),
		Retries:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "minimesh_retry_total", Help: "Total retry attempts after the initial attempt."}, []string{"service", "method"}),
		CircuitBreaker:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "minimesh_circuit_breaker_total", Help: "Circuit breaker rejections."}, []string{"service", "event"}),
		RateLimitRejected: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "minimesh_rate_limit_rejected_total", Help: "Requests rejected by the local rate limiter."}, []string{"service", "method"}),
		registry:          prometheus.NewRegistry(),
	}
	m.registry.MustRegister(m.Requests, m.Duration, m.Retries, m.CircuitBreaker, m.RateLimitRejected)
	m.registry.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return m
}

func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

func (m *Metrics) RegisterActiveConnections(fn func() float64) {
	m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "minimesh_active_connections", Help: "Active upstream gRPC connections."}, fn))
}

func (m *Metrics) RegisterConnectionStats(active, idle, created func() float64) {
	m.registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "minimesh_active_connections", Help: "Upstream gRPC connections currently serving requests."}, active),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "minimesh_idle_connections", Help: "Reusable idle upstream gRPC connections."}, idle),
		prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "minimesh_connection_created_total", Help: "Total upstream gRPC connections created."}, created),
	)
}

// InitTracer configures W3C propagation and an optional OTLP/HTTP exporter.
// An empty endpoint keeps tracing local/no-op, which preserves earlier stages.
func InitTracer(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if strings.TrimSpace(endpoint) == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, err
	}
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}
