#!/bin/sh
set -eu

PROXY_UID=${PROXY_UID:-1337}
PROXY_PORT=${PROXY_PORT:-15001}
OUTBOUND_PORTS=${OUTBOUND_PORTS:?OUTBOUND_PORTS is required}

iptables -w -t nat -N MINIMESH_OUTPUT 2>/dev/null || true
iptables -w -t nat -F MINIMESH_OUTPUT
iptables -w -t nat -C OUTPUT -p tcp -j MINIMESH_OUTPUT 2>/dev/null || \
  iptables -w -t nat -A OUTPUT -p tcp -j MINIMESH_OUTPUT

# Traffic created by the proxy must not be redirected back into itself.
iptables -w -t nat -A MINIMESH_OUTPUT -m owner --uid-owner "$PROXY_UID" -j RETURN
# Preserve Pod-local business-to-sidecar and health-check connections.
iptables -w -t nat -A MINIMESH_OUTPUT -d 127.0.0.0/8 -j RETURN
# Stage 14 deliberately captures only declared business ports.
iptables -w -t nat -A MINIMESH_OUTPUT -p tcp -m multiport \
  --dports "$OUTBOUND_PORTS" -j REDIRECT --to-ports "$PROXY_PORT"

iptables-save -t nat
