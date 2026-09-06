package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/observability"
	"github.com/minimesh/minimesh/internal/resilience"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type DialFunc func(ctx context.Context, target string) (*grpc.ClientConn, error)

type Resolver interface {
	Resolve(service string) (string, error)
}

type PickerResolver interface {
	Pick(service string) (address string, done func(), err error)
}

type ConnectionProvider interface {
	Acquire(context.Context, string) (*grpc.ClientConn, func(), error)
}

type ServiceConnectionProvider interface {
	AcquireForService(context.Context, string, string) (*grpc.ClientConn, func(), error)
}

type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	RetryableCodes map[codes.Code]bool
	Budget         *resilience.Budget
}

type CircuitBreakerPolicy struct {
	Breakers     *resilience.CircuitBreakerSet
	FailureCodes map[codes.Code]bool
}

type ServerConfig struct {
	RequestTimeout time.Duration
	Retry          RetryPolicy
	CircuitBreaker CircuitBreakerPolicy
	RateLimiter    *resilience.LocalRateLimiter
	Metrics        *observability.Metrics
}

type Server struct {
	minimeshv1.UnimplementedProxyServiceServer
	dial     DialFunc
	resolver Resolver
	pool     ConnectionProvider
	config   ServerConfig
}

func defaultDialer(ctx context.Context, target string) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		ctx,
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

func defaultServerConfig() ServerConfig {
	return ServerConfig{Retry: RetryPolicy{MaxAttempts: 1}}
}

func NewServer() *Server {
	return NewServerWithResolverDialerAndConfig(nil, defaultDialer, defaultServerConfig())
}

func NewServerWithDialer(dial DialFunc) *Server {
	return NewServerWithResolverDialerAndConfig(nil, dial, defaultServerConfig())
}

func NewServerWithResolver(resolver Resolver) *Server {
	return NewServerWithResolverDialerAndConfig(resolver, defaultDialer, defaultServerConfig())
}

func NewServerWithResolverAndConfig(resolver Resolver, cfg ServerConfig) *Server {
	return NewServerWithResolverDialerAndConfig(resolver, defaultDialer, cfg)
}

func NewServerWithResolverAndDialer(resolver Resolver, dial DialFunc) *Server {
	return NewServerWithResolverDialerAndConfig(resolver, dial, defaultServerConfig())
}

func NewServerWithResolverAndPool(resolver Resolver, pool ConnectionProvider) *Server {
	return NewServerWithResolverPoolAndConfig(resolver, pool, defaultServerConfig())
}

func NewServerWithResolverDialerAndConfig(resolver Resolver, dial DialFunc, cfg ServerConfig) *Server {
	if dial == nil {
		panic("proxy: nil dialer")
	}
	cfg = normalizeServerConfig(cfg)
	return &Server{dial: dial, resolver: resolver, config: cfg}
}

func NewServerWithResolverPoolAndConfig(resolver Resolver, pool ConnectionProvider, cfg ServerConfig) *Server {
	if pool == nil {
		panic("proxy: nil connection pool")
	}
	cfg = normalizeServerConfig(cfg)
	return &Server{resolver: resolver, pool: pool, config: cfg}
}

func normalizeServerConfig(cfg ServerConfig) ServerConfig {
	if cfg.RequestTimeout < 0 {
		cfg.RequestTimeout = 0
	}
	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry.MaxAttempts = 1
	}
	if cfg.Retry.InitialBackoff < 0 {
		cfg.Retry.InitialBackoff = 0
	}
	if cfg.Retry.MaxBackoff <= 0 || cfg.Retry.MaxBackoff < cfg.Retry.InitialBackoff {
		cfg.Retry.MaxBackoff = cfg.Retry.InitialBackoff
	}
	if cfg.Retry.RetryableCodes == nil {
		cfg.Retry.RetryableCodes = map[codes.Code]bool{}
	}
	if cfg.CircuitBreaker.FailureCodes == nil {
		cfg.CircuitBreaker.FailureCodes = map[codes.Code]bool{}
	}
	return cfg
}

func (s *Server) Invoke(ctx context.Context, req *minimeshv1.ProxyRequest) (response *minimeshv1.ProxyResponse, retErr error) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, metadataCarrier(metadataFromContext(ctx)))
	ctx, span := otel.Tracer("minimesh/sidecar").Start(ctx, "proxy.invoke", trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()
	started := time.Now()
	service, method := "", ""
	if req != nil {
		service, method = serviceFromTarget(req.GetTarget()), req.GetFullMethod()
	}
	defer func() {
		code := status.Code(retErr).String()
		span.SetAttributes(attribute.String("rpc.grpc.status_code", code), attribute.String("minimesh.target_service", service), attribute.String("rpc.method", method))
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(otelcodes.Error, retErr.Error())
		}
		if s.config.Metrics != nil {
			s.config.Metrics.Requests.WithLabelValues(service, method, code).Inc()
			s.config.Metrics.Duration.WithLabelValues(service, method, code).Observe(time.Since(started).Seconds())
		}
	}()
	if err := validate(req); err != nil {
		return nil, err
	}

	if s.config.RateLimiter != nil {
		if !s.config.RateLimiter.Allow(service, req.GetFullMethod()) {
			if s.config.Metrics != nil {
				s.config.Metrics.RateLimitRejected.WithLabelValues(service, method).Inc()
			}
			return nil, status.Errorf(codes.ResourceExhausted, "local rate limit exceeded for service %q method %q", service, req.GetFullMethod())
		}
	}

	requestCtx := ctx
	cancel := func() {}
	if s.config.RequestTimeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, s.config.RequestTimeout)
	}
	defer cancel()

	type attemptResult struct {
		response *minimeshv1.ProxyResponse
		header   metadata.MD
		trailer  metadata.MD
	}
	result, err := resilience.Do(requestCtx, resilience.DoConfig{
		Retry: resilience.RetryConfig{
			MaxAttempts:    s.config.Retry.MaxAttempts,
			InitialBackoff: s.config.Retry.InitialBackoff,
			MaxBackoff:     s.config.Retry.MaxBackoff,
		},
		Budget:    s.config.Retry.Budget,
		Retryable: s.isRetryable,
	}, func(attemptCtx context.Context, attempt int) (attemptResult, error) {
		if attempt > 1 && s.config.Metrics != nil {
			s.config.Metrics.Retries.WithLabelValues(service, method).Inc()
		}
		resp, header, trailer, err := s.invokeAttempt(attemptCtx, req)
		return attemptResult{response: resp, header: header, trailer: trailer}, err
	})
	if err != nil {
		if requestCtx.Err() != nil {
			return nil, status.FromContextError(requestCtx.Err()).Err()
		}
		return nil, err
	}
	propagateResponseMetadata(ctx, result.header, result.trailer)
	return result.response, nil
}

func (s *Server) invokeAttempt(ctx context.Context, req *minimeshv1.ProxyRequest) (response *minimeshv1.ProxyResponse, header metadata.MD, trailer metadata.MD, retErr error) {
	target, done, err := s.resolveTarget(req.GetTarget())
	if err != nil {
		return nil, nil, nil, err
	}
	defer done()

	var circuitPermit *resilience.CircuitPermit
	if s.config.CircuitBreaker.Breakers != nil {
		circuitPermit, err = s.config.CircuitBreaker.Breakers.Allow(target)
		if err != nil {
			if errors.Is(err, resilience.ErrCircuitOpen) {
				if s.config.Metrics != nil {
					s.config.Metrics.CircuitBreaker.WithLabelValues(serviceFromTarget(req.GetTarget()), "open_rejected").Inc()
				}
				return nil, nil, nil, status.Errorf(codes.Unavailable, "circuit breaker open for upstream %q", target)
			}
			return nil, nil, nil, status.Errorf(codes.Internal, "circuit breaker check failed: %v", err)
		}
		defer func() {
			circuitPermit.Done(!s.isCircuitFailure(retErr))
		}()
	}

	var conn *grpc.ClientConn
	if s.pool != nil {
		var release func()
		if servicePool, ok := s.pool.(ServiceConnectionProvider); ok {
			conn, release, err = servicePool.AcquireForService(ctx, target, serviceFromTarget(req.GetTarget()))
		} else {
			conn, release, err = s.pool.Acquire(ctx, target)
		}
		if err != nil {
			return nil, nil, nil, mapDialError(ctx, err)
		}
		defer release()
	} else {
		conn, err = s.dial(ctx, target)
		if err != nil {
			return nil, nil, nil, mapDialError(ctx, err)
		}
		defer conn.Close()
	}

	outgoingCtx := copyIncomingMetadata(ctx)
	upstreamResponse := new(minimeshv1.Payload)

	invokeOptions := []grpc.CallOption{grpc.Header(&header), grpc.Trailer(&trailer)}
	if req.GetPassthroughPayload() {
		invokeOptions = append(invokeOptions, grpc.ForceCodec(passthroughCodec{}))
	}
	err = conn.Invoke(outgoingCtx, req.GetFullMethod(), req.GetPayload(), upstreamResponse, invokeOptions...)
	if err != nil {
		return nil, header, trailer, mapUpstreamError(err)
	}
	return &minimeshv1.ProxyResponse{Payload: upstreamResponse}, header, trailer, nil
}

type metadataCarrier metadata.MD

func metadataFromContext(ctx context.Context) metadata.MD {
	md, _ := metadata.FromIncomingContext(ctx)
	// FromIncomingContext already returns a deep copy with normalized keys.
	// Copying it again adds one map and one slice allocation per request.
	return md
}
func (c metadataCarrier) Get(key string) string {
	values := metadata.MD(c).Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func (c metadataCarrier) Set(key, value string) { metadata.MD(c).Set(key, value) }
func (c metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

// passthroughCodec deliberately reports the normal "proto" content subtype.
// The upstream (which may be Java or Python) therefore uses its standard
// protobuf codec, while this client side writes/reads Payload.Data as the raw
// serialized request/response expected by the selected gRPC method.
type passthroughCodec struct{}

func (passthroughCodec) Name() string { return "proto" }

func (passthroughCodec) Marshal(v any) ([]byte, error) {
	payload, ok := v.(*minimeshv1.Payload)
	if !ok || payload == nil {
		return nil, fmt.Errorf("passthrough codec requires *Payload, got %T", v)
	}
	return payload.GetData(), nil
}

func (passthroughCodec) Unmarshal(data []byte, v any) error {
	payload, ok := v.(*minimeshv1.Payload)
	if !ok || payload == nil {
		return fmt.Errorf("passthrough codec requires *Payload, got %T", v)
	}
	payload.Data = append(payload.Data[:0], data...)
	return nil
}

func propagateResponseMetadata(ctx context.Context, header, trailer metadata.MD) {
	if len(header) > 0 {
		_ = grpc.SetHeader(ctx, header)
	}
	if len(trailer) > 0 {
		grpc.SetTrailer(ctx, trailer)
	}
}

func (s *Server) isRetryable(err error) bool {
	if err == nil {
		return false
	}
	return s.config.Retry.RetryableCodes[status.Code(err)]
}

func (s *Server) isCircuitFailure(err error) bool {
	if err == nil {
		return false
	}
	return s.config.CircuitBreaker.FailureCodes[status.Code(err)]
}

func serviceFromTarget(target string) string {
	const prefix = "service://"
	if !strings.HasPrefix(target, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(target, prefix))
}

func (s *Server) resolveTarget(target string) (string, func(), error) {
	const prefix = "service://"
	if !strings.HasPrefix(target, prefix) {
		return target, func() {}, nil
	}
	service := strings.TrimSpace(strings.TrimPrefix(target, prefix))
	if service == "" {
		return "", nil, status.Error(codes.InvalidArgument, "service target is empty")
	}
	if s.resolver == nil {
		return "", nil, status.Error(codes.FailedPrecondition, "service discovery is not configured")
	}
	if picker, ok := s.resolver.(PickerResolver); ok {
		address, done, err := picker.Pick(service)
		if err != nil {
			return "", nil, status.Errorf(codes.Unavailable, "no endpoint for service %q: %v", service, err)
		}
		if done == nil {
			done = func() {}
		}
		return address, done, nil
	}
	address, err := s.resolver.Resolve(service)
	if err != nil {
		return "", nil, status.Errorf(codes.Unavailable, "no endpoint for service %q: %v", service, err)
	}
	return address, func() {}, nil
}

func validate(req *minimeshv1.ProxyRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "proxy request is required")
	}
	if strings.TrimSpace(req.GetTarget()) == "" {
		return status.Error(codes.InvalidArgument, "target is required")
	}
	if !strings.HasPrefix(req.GetFullMethod(), "/") || strings.Count(req.GetFullMethod(), "/") < 2 {
		return status.Error(codes.InvalidArgument, "full_method must be a canonical gRPC method such as /package.Service/Method")
	}
	if req.GetPayload() == nil {
		return status.Error(codes.InvalidArgument, "payload is required")
	}
	return nil
}

func mapDialError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "backend dial deadline exceeded")
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "backend dial canceled")
	}
	return status.Errorf(codes.Unavailable, "backend unavailable: %v", err)
}

func mapUpstreamError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Errorf(codes.Unknown, "backend invocation failed: %v", err)
}

func copyIncomingMetadata(ctx context.Context) context.Context {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	// FromIncomingContext returns a deep copy, so it is safe to filter and
	// reuse it as outgoing metadata without cloning every value slice again.
	outgoing := incoming
	for key := range incoming {
		if isReservedMetadata(key) {
			delete(outgoing, key)
		}
	}
	otel.GetTextMapPropagator().Inject(ctx, metadataCarrier(outgoing))
	return metadata.NewOutgoingContext(ctx, outgoing)
}

func isReservedMetadata(key string) bool {
	if strings.HasPrefix(key, ":") || strings.HasPrefix(key, "grpc-") {
		return true
	}
	switch key {
	case "content-type", "te", "user-agent":
		return true
	default:
		return false
	}
}
