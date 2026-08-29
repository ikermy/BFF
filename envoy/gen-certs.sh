#!/usr/bin/env bash
# =============================================================================
# gen-certs.sh — генерация самоподписанных сертификатов для dev-режима Envoy.
# =============================================================================
# Создаёт envoy/certs/localhost.pem и envoy/certs/localhost-key.pem.
# Запуск из корня проекта:
#   ./envoy/gen-certs.sh
#
# Требования: openssl.
# =============================================================================

set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
CERTS_DIR="$DIR/certs"
mkdir -p "$CERTS_DIR"

CERT="$CERTS_DIR/localhost.pem"
KEY="$CERTS_DIR/localhost-key.pem"

if [[ -f "$CERT" && -f "$KEY" ]]; then
  echo "Сертификаты уже существуют: $CERTS_DIR (пропускаю)."
  echo "Для перегенерации удалите их и запустите скрипт заново."
  exit 0
fi

echo "Генерация самоподписанного сертификата (localhost)..."
openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
  -keyout "$KEY" \
  -out "$CERT" \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"

echo "Готово:"
echo "  cert: $CERT"
echo "  key:  $KEY"
echo ""
echo "Запуск dev-стека:"
echo "  docker compose up -d"
