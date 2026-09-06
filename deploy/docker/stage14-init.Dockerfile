FROM alpine:3.23
RUN apk add --no-cache iptables
COPY deploy/transparent/iptables-init.sh /usr/local/bin/minimesh-iptables
ENTRYPOINT ["/usr/local/bin/minimesh-iptables"]
