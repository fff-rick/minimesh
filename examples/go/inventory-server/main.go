// inventory-server is the Go middle service in the Stage 9 cross-language demo.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"time"

	minimeshv1 "github.com/minimesh/minimesh/api/gen/minimesh/v1"
	"github.com/minimesh/minimesh/internal/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type inventoryServer struct {
	minimeshv1.UnimplementedInventoryServiceServer
	proxy          minimeshv1.ProxyServiceClient
	recommendation minimeshv1.RecommendationServiceClient
}

func (s inventoryServer) CheckInventory(ctx context.Context, request *minimeshv1.InventoryRequest) (*minimeshv1.InventoryResponse, error) {
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = otel.GetTextMapPropagator().Extract(ctx, grpcMetadataCarrier(incoming.Copy()))
	}
	ctx, span := otel.Tracer("minimesh/inventory").Start(ctx, "inventory.check")
	defer span.End()
	span.SetAttributes(attribute.String("inventory.sku", request.GetSku()))
	// Preserve both the caller's deadline and application metadata while crossing
	// the second sidecar. The sidecar filters only transport-reserved headers.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md.Copy())
	}
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok {
		otel.GetTextMapPropagator().Inject(ctx, grpcMetadataCarrier(outgoing))
	}
	var recommendation *minimeshv1.RecommendationResponse
	var err error
	if s.recommendation != nil {
		recommendation, err = s.recommendation.Recommend(ctx, &minimeshv1.RecommendationRequest{Sku: request.GetSku()})
	} else {
		payload, marshalErr := proto.Marshal(request)
		if marshalErr != nil {
			return nil, marshalErr
		}
		result, invokeErr := s.proxy.Invoke(ctx, &minimeshv1.ProxyRequest{
			Target:             "service://recommendation",
			FullMethod:         minimeshv1.RecommendationService_Recommend_FullMethodName,
			Payload:            &minimeshv1.Payload{Data: payload},
			PassthroughPayload: true,
		})
		if invokeErr == nil {
			recommendation = new(minimeshv1.RecommendationResponse)
			invokeErr = proto.Unmarshal(result.GetPayload().GetData(), recommendation)
		}
		err = invokeErr
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		return nil, err // Preserve the standard gRPC status for the Java caller.
	}
	return &minimeshv1.InventoryResponse{
		Available:      request.GetSku() != "out-of-stock",
		Recommendation: recommendation.GetRecommendation(),
		TraceId:        recommendation.GetTraceId(),
		RequestId:      recommendation.GetRequestId(),
	}, nil
}

type grpcMetadataCarrier metadata.MD

func (c grpcMetadataCarrier) Get(key string) string {
	values := metadata.MD(c).Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func (c grpcMetadataCarrier) Set(key, value string) { metadata.MD(c).Set(key, value) }
func (c grpcMetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

func main() {
	listen := flag.String("listen", ":19091", "Inventory gRPC listen address")
	sidecar := flag.String("sidecar", "127.0.0.1:18080", "local MiniMesh sidecar address")
	recommendationAddress := flag.String("recommendation", "", "direct Recommendation gRPC address; traffic may be transparently intercepted")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownTracer, err := observability.InitTracer(ctx, os.Getenv("OTEL_SERVICE_NAME"), os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	cancel()
	if err != nil {
		slog.Error("configure tracing", "error", err)
		os.Exit(2)
	}
	defer func() { _ = shutdownTracer(context.Background()) }()

	upstreamAddress := *sidecar
	if *recommendationAddress != "" {
		upstreamAddress = *recommendationAddress
	}
	conn, err := grpc.NewClient(upstreamAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("create inventory upstream client", "address", upstreamAddress, "error", err)
		os.Exit(1)
	}
	defer conn.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		slog.Error("inventory listen failed", "error", err)
		os.Exit(1)
	}
	server := grpc.NewServer()
	handler := inventoryServer{}
	if *recommendationAddress != "" {
		handler.recommendation = minimeshv1.NewRecommendationServiceClient(conn)
	} else {
		handler.proxy = minimeshv1.NewProxyServiceClient(conn)
	}
	minimeshv1.RegisterInventoryServiceServer(server, handler)
	slog.Info("Go inventory listening", "addr", *listen, "upstream", upstreamAddress, "transparent_direct", *recommendationAddress != "")
	if err := server.Serve(listener); err != nil {
		slog.Error("inventory stopped", "error", err)
	}
}
