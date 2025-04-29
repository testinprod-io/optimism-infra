#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
TLS_DIR="$SCRIPT_DIR/tls"

version=$(openssl version)

# Adjust this check as needed if your system reports a slightly different string
if [[ "$version" != *"OpenSSL 3.4.0"* ]]; then
  echo "openssl version detected: $version"
  echo "This script only tested with OpenSSL 3.4.0"
  exit 1
fi

echo "Generating mTLS credentials for local development..."
echo ""

mkdir -p "$TLS_DIR"

if [ ! -f "$TLS_DIR/ca.crt" ]; then
  echo "Generating CA"
  openssl req -newkey rsa:2048 \
    -new -nodes -x509 \
    -days 365 \
    -sha256 \
    -out "$TLS_DIR/ca.crt" \
    -keyout "$TLS_DIR/ca.key" \
    -subj "/O=OP Labs/CN=root"
fi

echo "Generating TLS key and certificate signing request"
openssl genrsa -out "$TLS_DIR/tls.key" 2048

#
# Generate the CSR with SAN for localhost. The -extensions and -config
# approach is retained to keep the final certificate identical.
#
openssl req -new -key "$TLS_DIR/tls.key" \
  -days 14 \
  -sha256 \
  -out "$TLS_DIR/tls.csr" \
  -subj "/O=OP Labs/CN=localhost" \
  -extensions san \
  -config <(
    echo '[req]'
    echo 'distinguished_name=req'
    echo '[san]'
    echo 'subjectAltName=DNS:localhost'
  )

echo "Signing TLS certificate with our local CA"
openssl x509 -req -in "$TLS_DIR/tls.csr" \
  -sha256 \
  -CA "$TLS_DIR/ca.crt" \
  -CAkey "$TLS_DIR/ca.key" \
  -CAcreateserial \
  -out "$TLS_DIR/tls.crt" \
  -days 14 \
  -extfile <(echo 'subjectAltName=DNS:localhost')

echo ""
echo "Done! Generated files in: $TLS_DIR"
echo "  - ca.crt, ca.key  (self-signed CA)"
echo "  - tls.crt, tls.key (server certificate + key)"
