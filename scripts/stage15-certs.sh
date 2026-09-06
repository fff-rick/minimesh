#!/bin/sh
set -eu

OUTPUT=${1:?output directory is required}
SERIAL_BASE=${2:-1000}
if [ "$#" -ge 2 ]; then
  shift 2
else
  shift 1
fi
SERVICES=${*:-order inventory recommendation}
TRUST_DOMAIN=${MINIMESH_TRUST_DOMAIN:-minimesh.local}
NAMESPACE=${MINIMESH_IDENTITY_NAMESPACE:-minimesh}

mkdir -p "$OUTPUT"
if [ ! -f "$OUTPUT/ca.crt" ] || [ ! -f "$OUTPUT/ca.key" ]; then
  openssl genrsa -out "$OUTPUT/ca.key" 2048 >/dev/null 2>&1
  openssl req -x509 -new -key "$OUTPUT/ca.key" -sha256 -days 3650 \
    -subj "/CN=MiniMesh Stage 15 Root CA" -out "$OUTPUT/ca.crt"
fi

index=0
for service in $SERVICES; do
  service_dir="$OUTPUT/$service"
  mkdir -p "$service_dir"
  openssl genrsa -out "$service_dir/tls.key" 2048 >/dev/null 2>&1
  openssl req -new -key "$service_dir/tls.key" -subj "/CN=$service.mesh" \
    -out "$service_dir/tls.csr"
  printf '%s\n' \
    'basicConstraints=critical,CA:FALSE' \
    'keyUsage=critical,digitalSignature,keyEncipherment' \
    'extendedKeyUsage=serverAuth,clientAuth' \
    "subjectAltName=DNS:$service.mesh,URI:spiffe://$TRUST_DOMAIN/ns/$NAMESPACE/sa/$service" \
    >"$service_dir/extensions.cnf"
  serial=$((SERIAL_BASE + index))
  openssl x509 -req -in "$service_dir/tls.csr" \
    -CA "$OUTPUT/ca.crt" -CAkey "$OUTPUT/ca.key" -set_serial "$serial" \
    -days 1 -sha256 -extfile "$service_dir/extensions.cnf" \
    -out "$service_dir/tls.crt" >/dev/null 2>&1
  cp "$OUTPUT/ca.crt" "$service_dir/ca.crt"
  rm -f "$service_dir/tls.csr" "$service_dir/extensions.cnf"
  index=$((index + 1))
done
