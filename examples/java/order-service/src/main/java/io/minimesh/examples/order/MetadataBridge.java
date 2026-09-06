package io.minimesh.examples.order;

import io.grpc.*;

final class MetadataBridge {
  static final Context.Key<Metadata> INBOUND = Context.key("minimesh-inbound-metadata");

  static final ServerInterceptor CAPTURE = new ServerInterceptor() {
    @Override public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
        ServerCall<ReqT, RespT> call, Metadata headers, ServerCallHandler<ReqT, RespT> next) {
      return Contexts.interceptCall(Context.current().withValue(INBOUND, headers), call, headers, next);
    }
  };

  static Channel withForwardedMetadata(Channel channel) {
    return ClientInterceptors.intercept(channel, new ClientInterceptor() {
      @Override public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
          MethodDescriptor<ReqT, RespT> method, CallOptions options, Channel next) {
        ClientCall<ReqT, RespT> call = next.newCall(method, options);
        return new ForwardingClientCall.SimpleForwardingClientCall<>(call) {
          @Override public void start(Listener<RespT> listener, Metadata headers) {
            Metadata incoming = INBOUND.get(Context.current());
            if (incoming != null) headers.merge(incoming);
	            Tracing.inject(headers);
            super.start(listener, headers);
          }
        };
      }
    });
  }

  static String value(String key) {
    Metadata incoming = INBOUND.get(Context.current());
    return incoming == null ? "" : String.valueOf(incoming.get(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER)) == null ? "" : incoming.get(Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER)));
  }
}
