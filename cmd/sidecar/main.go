package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	httppprof "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/controlstream"
	"github.com/minimesh/minimesh/internal/discovery"
	"github.com/minimesh/minimesh/internal/loadbalance"
	"github.com/minimesh/minimesh/internal/meshsecurity"
	"github.com/minimesh/minimesh/internal/observability"
	"github.com/minimesh/minimesh/internal/proxy"
	"github.com/minimesh/minimesh/internal/registration"
	"github.com/minimesh/minimesh/internal/resilience"
	"github.com/minimesh/minimesh/internal/transparentproxy"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

func main() {
	listenAddr := flag.String("listen", ":18080", "gRPC listen address")
	controlPlane := flag.String("control-plane", "127.0.0.1:17071", "control-plane gRPC stream address")
	sidecarID := flag.String("sidecar-id", "", "stable sidecar identity; defaults to the listen address")
	lbAlgorithm := flag.String("lb", string(loadbalance.RoundRobin), "load balancing algorithm")
	usePool := flag.Bool("connection-pool", true, "reuse upstream gRPC connections")
	maxConnections := flag.Int("max-connections", 2, "maximum upstream connections per endpoint")
	maxIdle := flag.Duration("max-idle", 30*time.Second, "close an idle upstream connection after this duration")
	keepaliveTime := flag.Duration("keepalive-time", 30*time.Second, "upstream gRPC keepalive ping interval; 0 disables explicit keepalive")
	keepaliveTimeout := flag.Duration("keepalive-timeout", 5*time.Second, "upstream gRPC keepalive timeout")
	requestTimeout := flag.Duration("request-timeout", 2*time.Second, "maximum total time for an upstream request including retries; 0 preserves only the caller deadline")
	maxAttempts := flag.Int("max-attempts", 3, "maximum total attempts including the first request")
	retryInitialBackoff := flag.Duration("retry-backoff", 20*time.Millisecond, "initial exponential retry backoff")
	retryMaxBackoff := flag.Duration("retry-max-backoff", 200*time.Millisecond, "maximum retry backoff")
	retryableCodesText := flag.String("retryable-codes", "unavailable,resource_exhausted,internal", "comma-separated gRPC status codes eligible for retry")
	retryBudgetRate := flag.Float64("retry-budget-rate", 100, "retry-budget tokens refilled per second")
	retryBudgetBurst := flag.Int("retry-budget-burst", 100, "maximum retry-budget burst tokens")
	circuitEnabled := flag.Bool("circuit-breaker", true, "enable per-upstream circuit breaking")
	circuitWindow := flag.Int("circuit-window", 20, "sliding window size per upstream")
	circuitMinRequests := flag.Int("circuit-min-requests", 10, "minimum requests before evaluating failure rate")
	circuitFailureRate := flag.Float64("circuit-failure-rate", 0.5, "failure-rate threshold in [0,1] that opens the circuit")
	circuitCooldown := flag.Duration("circuit-cooldown", 5*time.Second, "open-state cooldown before half-open probes")
	circuitHalfOpenProbes := flag.Int("circuit-half-open-probes", 2, "successful half-open probes required to close")
	circuitFailureCodesText := flag.String("circuit-failure-codes", "unavailable,resource_exhausted,internal,deadline_exceeded", "comma-separated gRPC status codes counted as circuit failures")
	rateLimitEnabled := flag.Bool("rate-limit", true, "enable local token-bucket rate limiting")
	rateLimitRate := flag.Float64("rate-limit-rate", 0, "default requests per second; 0 disables the default rule")
	rateLimitBurst := flag.Int("rate-limit-burst", 1000, "default token-bucket burst when default rate is enabled")
	serviceRateLimitsText := flag.String("service-rate-limits", "", "service rules: service=rate:burst;service2=rate:burst")
	routeRateLimitsText := flag.String("route-rate-limits", "", "route rules: service|/package.Service/Method=rate:burst;...")
	metricsListen := flag.String("metrics-listen", ":18081", "Prometheus metrics listen address; empty disables")
	pprofEnabled := flag.Bool("pprof", false, "expose /debug/pprof on metrics-listen; enable only on trusted benchmark/debug networks")
	otelEndpoint := flag.String("otel-endpoint", "", "OTLP/HTTP traces endpoint; empty disables export")
	serviceName := flag.String("service-name", "minimesh-sidecar", "OpenTelemetry service.name")
	registryAddress := flag.String("registry-address", "", "control-plane HTTP base URL used to register the colocated service; empty disables registration")
	registerService := flag.String("register-service", "", "name of the colocated business service")
	registerInstance := flag.String("register-instance", "", "instance ID of the colocated business service; defaults to sidecar-id")
	registerAddress := flag.String("register-address", "", "host:port advertised for the colocated business service")
	registerTTL := flag.Duration("register-ttl", 15*time.Second, "service registration lease TTL")
	readinessServicesText := flag.String("readiness-services", "", "comma-separated discovered services required by /readyz")
	transparentListen := flag.String("transparent-listen", "", "IPv4 TCP listener for iptables REDIRECT traffic; empty disables transparent proxying")
	mtlsListen := flag.String("mtls-listen", "", "Sidecar mTLS ingress listener; empty disables service-to-service mTLS")
	mtlsBackend := flag.String("mtls-backend", "", "colocated plaintext business address for mTLS ingress")
	mtlsCA := flag.String("mtls-ca", "", "mesh trust bundle PEM")
	mtlsCert := flag.String("mtls-cert", "", "workload certificate PEM")
	mtlsKey := flag.String("mtls-key", "", "workload private key PEM")
	mtlsTrustDomain := flag.String("mtls-trust-domain", "minimesh.local", "SPIFFE trust domain")
	mtlsNamespace := flag.String("mtls-namespace", "default", "identity namespace")
	mtlsAllowedPeerServices := flag.String("mtls-allowed-peer-services", "", "comma-separated services authorized on mTLS ingress")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	shutdownTracer, err := observability.InitTracer(ctx, *serviceName, *otelEndpoint)
	if err != nil {
		slog.Error("configure tracing", "error", err)
		os.Exit(2)
	}
	defer func() { _ = shutdownTracer(context.Background()) }()
	metrics := observability.NewMetrics()

	cache := discovery.NewCache()
	var controlReady atomic.Bool
	var registrationReady atomic.Bool
	if *sidecarID == "" {
		*sidecarID = *listenAddr
	}
	if *registerInstance == "" {
		*registerInstance = *sidecarID
	}
	registrationConfigured := *registryAddress != "" || *registerService != "" || *registerAddress != ""
	var readinessServices []string
	for _, service := range strings.Split(*readinessServicesText, ",") {
		if service = strings.TrimSpace(service); service != "" {
			readinessServices = append(readinessServices, service)
		}
	}
	if registrationConfigured {
		cfg := registration.Config{
			RegistryURL: *registryAddress,
			Endpoint:    discovery.Endpoint{Service: *registerService, InstanceID: *registerInstance, Address: *registerAddress},
			TTL:         *registerTTL,
			OnRegistered: func(leaseID int64) {
				registrationReady.Store(true)
				slog.Info("colocated service registered", "service", *registerService, "instance", *registerInstance, "address", *registerAddress, "lease_id", leaseID)
			},
		}
		go func() {
			if registerErr := registration.Run(ctx, cfg); registerErr != nil {
				slog.Error("service registration stopped", "error", registerErr)
				cancel()
			}
		}()
	}
	go controlstream.Run(ctx, controlstream.ClientConfig{
		Address: *controlPlane, SidecarID: *sidecarID,
		OnSnapshot: func() { controlReady.Store(true) },
	}, cache)

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		slog.Error("sidecar listen failed", "addr", *listenAddr, "error", err)
		os.Exit(1)
	}
	if *transparentListen != "" {
		transparentListener, listenErr := net.Listen("tcp4", *transparentListen)
		if listenErr != nil {
			slog.Error("transparent proxy listen failed", "addr", *transparentListen, "error", listenErr)
			os.Exit(1)
		}
		go func() {
			serveErr := transparentproxy.Serve(ctx, transparentListener, transparentproxy.Config{
				OnConnection: func(target string) {
					slog.Info("transparent connection", "original_destination", target)
				},
				OnError: func(proxyErr error) { slog.Warn("transparent proxy connection failed", "error", proxyErr) },
			})
			if serveErr != nil && ctx.Err() == nil {
				slog.Error("transparent proxy stopped", "error", serveErr)
				cancel()
			}
		}()
	}
	identityFiles := meshsecurity.Files{CA: *mtlsCA, Cert: *mtlsCert, Key: *mtlsKey}
	securityConfigured := *mtlsListen != "" || *mtlsBackend != "" || *mtlsCA != "" || *mtlsCert != "" || *mtlsKey != ""
	if securityConfigured {
		if !*usePool {
			slog.Error("mTLS requires the upstream connection pool")
			os.Exit(2)
		}
		allowedIdentities := make([]string, 0)
		for _, peerService := range strings.Split(*mtlsAllowedPeerServices, ",") {
			if peerService = strings.TrimSpace(peerService); peerService != "" {
				allowedIdentities = append(allowedIdentities, meshsecurity.SPIFFEID(*mtlsTrustDomain, *mtlsNamespace, peerService))
			}
		}
		serverTLS, tlsErr := meshsecurity.ServerTLSConfig(identityFiles, allowedIdentities)
		if tlsErr != nil {
			slog.Error("configure mTLS ingress", "error", tlsErr)
			os.Exit(2)
		}
		identity, serial, identityErr := meshsecurity.CurrentIdentity(identityFiles)
		if identityErr != nil {
			slog.Error("load workload identity", "error", identityErr)
			os.Exit(2)
		}
		secureListener, listenErr := net.Listen("tcp", *mtlsListen)
		if listenErr != nil {
			slog.Error("mTLS ingress listen failed", "addr", *mtlsListen, "error", listenErr)
			os.Exit(1)
		}
		go func() {
			serveErr := meshsecurity.ServeRelay(ctx, secureListener, meshsecurity.RelayConfig{
				TLS: serverTLS, Backend: *mtlsBackend,
				OnConnection: func(info meshsecurity.AcceptInfo) {
					slog.Info("mTLS connection accepted", "peer_identity", info.PeerIdentity, "peer_serial", info.PeerSerial)
				},
				OnError: func(relayErr error) { slog.Warn("mTLS ingress connection rejected", "error", relayErr) },
			})
			if serveErr != nil && ctx.Err() == nil {
				slog.Error("mTLS ingress stopped", "error", serveErr)
				cancel()
			}
		}()
		slog.Info("workload identity loaded", "identity", identity, "serial", serial, "allowed_peers", allowedIdentities)
	}
	lbResolver, err := loadbalance.NewResolver(cache, loadbalance.Algorithm(*lbAlgorithm))
	if err != nil {
		slog.Error("invalid load balancing algorithm", "algorithm", *lbAlgorithm, "error", err)
		os.Exit(2)
	}

	retryableCodes, err := parseRetryableCodes(*retryableCodesText)
	if err != nil {
		slog.Error("invalid retryable code", "value", *retryableCodesText, "error", err)
		os.Exit(2)
	}
	retryBudget := resilience.NewBudget(resilience.BudgetConfig{RatePerSecond: *retryBudgetRate, Burst: *retryBudgetBurst})
	circuitFailureCodes, err := parseGRPCCodes(*circuitFailureCodesText)
	if err != nil {
		slog.Error("invalid circuit failure code", "value", *circuitFailureCodesText, "error", err)
		os.Exit(2)
	}
	var circuitBreakers *resilience.CircuitBreakerSet
	if *circuitEnabled {
		circuitBreakers = resilience.NewCircuitBreakerSet(resilience.CircuitBreakerConfig{
			WindowSize:           *circuitWindow,
			MinimumRequests:      *circuitMinRequests,
			FailureRateThreshold: *circuitFailureRate,
			Cooldown:             *circuitCooldown,
			HalfOpenMaxRequests:  *circuitHalfOpenProbes,
		})
	}
	var localRateLimiter *resilience.LocalRateLimiter
	if *rateLimitEnabled {
		serviceRules, parseErr := resilience.ParseServiceRateRules(*serviceRateLimitsText)
		if parseErr != nil {
			slog.Error("invalid service rate limit rules", "value", *serviceRateLimitsText, "error", parseErr)
			os.Exit(2)
		}
		routeRules, parseErr := resilience.ParseRouteRateRules(*routeRateLimitsText)
		if parseErr != nil {
			slog.Error("invalid route rate limit rules", "value", *routeRateLimitsText, "error", parseErr)
			os.Exit(2)
		}
		var defaultRule *resilience.RateLimitRule
		if *rateLimitRate > 0 {
			defaultRule = &resilience.RateLimitRule{RatePerSecond: *rateLimitRate, Burst: *rateLimitBurst}
		}
		localRateLimiter, err = resilience.NewLocalRateLimiter(defaultRule, serviceRules, routeRules)
		if err != nil {
			slog.Error("create local rate limiter", "error", err)
			os.Exit(2)
		}
	}

	proxyConfig := proxy.ServerConfig{
		RequestTimeout: *requestTimeout,
		Retry: proxy.RetryPolicy{
			MaxAttempts:    *maxAttempts,
			InitialBackoff: *retryInitialBackoff,
			MaxBackoff:     *retryMaxBackoff,
			RetryableCodes: retryableCodes,
			Budget:         retryBudget,
		},
		CircuitBreaker: proxy.CircuitBreakerPolicy{
			Breakers:     circuitBreakers,
			FailureCodes: circuitFailureCodes,
		},
		RateLimiter: localRateLimiter,
		Metrics:     metrics,
	}

	var metricsServer *http.Server
	if *metricsListen != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry(), promhttp.HandlerOpts{}))
		mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
		})
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
			if !controlReady.Load() || (registrationConfigured && !registrationReady.Load()) {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			for _, service := range readinessServices {
				if len(cache.List(service)) == 0 {
					http.Error(w, "required service not discovered: "+service, http.StatusServiceUnavailable)
					return
				}
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
		})
		if *pprofEnabled {
			mux.HandleFunc("/debug/pprof/", httppprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", httppprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", httppprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", httppprof.Trace)
		}
		metricsServer = &http.Server{Addr: *metricsListen, Handler: mux, ReadHeaderTimeout: 2 * time.Second}
		go func() {
			if serveErr := metricsServer.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
				slog.Error("rate-limit metrics server stopped", "error", serveErr)
				cancel()
			}
		}()
	}

	server := grpc.NewServer()
	var connPool *proxy.GRPCConnectionPool
	if *usePool {
		poolConfig := proxy.GRPCPoolConfig{
			MaxConnectionsPerTarget: *maxConnections,
			MaxIdleTime:             *maxIdle,
			KeepaliveTime:           *keepaliveTime,
			KeepaliveTimeout:        *keepaliveTimeout,
		}
		if securityConfigured {
			poolConfig.CredentialsForService = func(targetService string) (credentials.TransportCredentials, error) {
				clientTLS, tlsErr := meshsecurity.ClientTLSConfig(
					identityFiles,
					meshsecurity.SPIFFEID(*mtlsTrustDomain, *mtlsNamespace, targetService),
					targetService+".mesh",
				)
				if tlsErr != nil {
					return nil, tlsErr
				}
				return credentials.NewTLS(clientTLS), nil
			}
		}
		connPool, err = proxy.NewGRPCConnectionPool(poolConfig)
		if err != nil {
			slog.Error("create connection pool", "error", err)
			os.Exit(2)
		}
		minimeshv1.RegisterProxyServiceServer(server, proxy.NewServerWithResolverPoolAndConfig(lbResolver, connPool, proxyConfig))
		metrics.RegisterConnectionStats(
			func() float64 { return float64(connPool.Stats().ActiveConnections) },
			func() float64 { return float64(connPool.Stats().IdleConnections) },
			func() float64 { return float64(connPool.Stats().ConnectionCreated) },
		)
	} else {
		minimeshv1.RegisterProxyServiceServer(server, proxy.NewServerWithResolverAndConfig(lbResolver, proxyConfig))
		metrics.RegisterConnectionStats(func() float64 { return 0 }, func() float64 { return 0 }, func() float64 { return 0 })
	}

	go func() {
		<-ctx.Done()
		slog.Info("sidecar graceful shutdown started")
		server.GracefulStop()
		if connPool != nil {
			stats := connPool.Stats()
			slog.Info("upstream connection pool final stats", "active", stats.ActiveConnections, "idle", stats.IdleConnections, "created_total", stats.ConnectionCreated, "targets", stats.Targets)
			_ = connPool.Close()
		}
		if localRateLimiter != nil {
			slog.Info("local rate limiter final stats", "rules", localRateLimiter.Stats())
		}
		if metricsServer != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = metricsServer.Shutdown(shutdownCtx)
			shutdownCancel()
		}
	}()

	slog.Info("MiniMesh sidecar listening", "addr", *listenAddr, "transparent_listen", *transparentListen, "mtls_listen", *mtlsListen, "control_plane", *controlPlane, "sidecar_id", *sidecarID, "lb", *lbAlgorithm, "connection_pool", *usePool, "metrics_listen", *metricsListen, "pprof", *pprofEnabled, "otel_endpoint", *otelEndpoint, "service_name", *serviceName, "register_service", *registerService)
	if err := server.Serve(listener); err != nil && ctx.Err() == nil {
		slog.Error("sidecar stopped", "error", err)
		os.Exit(1)
	}
}

func parseRetryableCodes(text string) (map[codes.Code]bool, error) {
	return parseGRPCCodes(text)
}

func parseGRPCCodes(text string) (map[codes.Code]bool, error) {
	result := make(map[codes.Code]bool)
	for _, raw := range strings.Split(text, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		var code codes.Code
		switch name {
		case "unavailable":
			code = codes.Unavailable
		case "resource_exhausted", "resourceexhausted":
			code = codes.ResourceExhausted
		case "internal":
			code = codes.Internal
		case "aborted":
			code = codes.Aborted
		case "deadline_exceeded", "deadlineexceeded":
			code = codes.DeadlineExceeded
		default:
			return nil, fmt.Errorf("unsupported gRPC code %q", raw)
		}
		result[code] = true
	}
	return result, nil
}
