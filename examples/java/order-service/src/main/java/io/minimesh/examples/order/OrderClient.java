package io.minimesh.examples.order;

import io.grpc.*;
import io.grpc.netty.shaded.io.grpc.netty.NettyChannelBuilder;
import io.grpc.stub.MetadataUtils;
import io.minimesh.v1.*;
import java.util.concurrent.TimeUnit;

public final class OrderClient {
  public static void main(String[] args) {
    String target = System.getenv().getOrDefault("ORDER_ADDRESS", "127.0.0.1:19090");
    String sku = System.getenv().getOrDefault("ORDER_SKU", args.length > 0 ? args[0] : "book");
    long timeoutMillis = Long.parseLong(System.getenv().getOrDefault("ORDER_TIMEOUT_MS", "2000"));
    int colon = target.lastIndexOf(':');
    if (colon <= 0 || colon == target.length() - 1) throw new IllegalArgumentException("expected host:port: " + target);
    ManagedChannel channel = NettyChannelBuilder.forTarget("dns:///" + target).usePlaintext().build();
    Metadata headers = new Metadata();
    headers.put(Metadata.Key.of("x-request-id", Metadata.ASCII_STRING_MARSHALLER), "stage9-request");
    headers.put(Metadata.Key.of("traceparent", Metadata.ASCII_STRING_MARSHALLER), "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01");
    OrderServiceGrpc.OrderServiceBlockingStub client = OrderServiceGrpc.newBlockingStub(channel)
        .withInterceptors(MetadataUtils.newAttachHeadersInterceptor(headers)).withDeadlineAfter(timeoutMillis, TimeUnit.MILLISECONDS);
    try {
      OrderResponse response = client.placeOrder(OrderRequest.newBuilder().setSku(sku).setQuantity(1).build());
      System.out.printf("order=%s available=%s recommendation=%s trace_id=%s request_id=%s%n", response.getOrderId(), response.getAvailable(), response.getRecommendation(), response.getTraceId(), response.getRequestId());
    } catch (StatusRuntimeException e) { System.err.println("grpc_status=" + e.getStatus().getCode() + " description=" + e.getStatus().getDescription()); System.exit(2); }
    finally { channel.shutdown(); }
  }
}
