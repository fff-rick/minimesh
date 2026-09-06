package io.minimesh.examples.order;

import com.google.protobuf.ByteString;
import io.grpc.*;
import io.grpc.netty.shaded.io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.StreamObserver;
import io.minimesh.v1.*;
import java.util.concurrent.TimeUnit;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.context.Scope;

public final class OrderServer {
  public static void main(String[] args) throws Exception {
	Tracing.init();
    int port = Integer.parseInt(System.getenv().getOrDefault("ORDER_PORT", "19090"));
    String sidecar = System.getenv().getOrDefault("SIDECAR_ADDRESS", "127.0.0.1:18080");
    ManagedChannel channel = channelFor(sidecar);
    Server server = NettyServerBuilder.forPort(port)
        .addService(ServerInterceptors.intercept(new Orders(channel), MetadataBridge.CAPTURE)).build().start();
    Runtime.getRuntime().addShutdownHook(new Thread(() -> { channel.shutdown(); server.shutdown(); }));
    server.awaitTermination();
  }

  private static ManagedChannel channelFor(String address) {
    int colon = address.lastIndexOf(':');
    if (colon <= 0 || colon == address.length() - 1) throw new IllegalArgumentException("expected host:port: " + address);
    return NettyChannelBuilder.forTarget("dns:///" + address).usePlaintext().build();
  }

  private static final class Orders extends OrderServiceGrpc.OrderServiceImplBase {
    private final ProxyServiceGrpc.ProxyServiceBlockingStub proxy;
    Orders(Channel channel) { proxy = ProxyServiceGrpc.newBlockingStub(MetadataBridge.withForwardedMetadata(channel)); }

    @Override public void placeOrder(OrderRequest request, StreamObserver<OrderResponse> observer) {
      Span span = Tracing.serverSpan(MetadataBridge.INBOUND.get(Context.current()));
      try (Scope ignored = span.makeCurrent()) {
        InventoryRequest inventory = InventoryRequest.newBuilder().setSku(request.getSku()).setQuantity(request.getQuantity()).build();
        ProxyRequest proxyRequest = ProxyRequest.newBuilder().setTarget("service://inventory")
            // grpc-java's descriptor omits the leading slash; ProxyService's
            // transport contract deliberately requires canonical gRPC paths.
            .setFullMethod("/" + InventoryServiceGrpc.getCheckInventoryMethod().getFullMethodName())
            .setPayload(Payload.newBuilder().setData(ByteString.copyFrom(inventory.toByteArray()))).setPassthroughPayload(true).build();
        long timeoutMillis = 1500;
        Deadline deadline = Context.current().getDeadline();
        if (deadline != null) timeoutMillis = Math.max(1, deadline.timeRemaining(TimeUnit.MILLISECONDS));
        ProxyResponse proxied = proxy.withDeadlineAfter(timeoutMillis, TimeUnit.MILLISECONDS).invoke(proxyRequest);
        InventoryResponse result = InventoryResponse.parseFrom(proxied.getPayload().getData());
        observer.onNext(OrderResponse.newBuilder().setOrderId("order-" + request.getSku()).setAvailable(result.getAvailable())
            .setRecommendation(result.getRecommendation()).setTraceId(result.getTraceId()).setRequestId(result.getRequestId()).build());
        observer.onCompleted();
      } catch (StatusRuntimeException e) { span.recordException(e); observer.onError(e); }
        catch (Exception e) { span.recordException(e); observer.onError(Status.INTERNAL.withCause(e).withDescription("decode inventory response").asRuntimeException()); }
        finally { span.end(); }
    }
  }
}
