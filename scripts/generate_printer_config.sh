#!/bin/bash
# Writes prusa_printer_settings.ini, the file a printer reads from a USB drive
# to find this server. `make generate-printer-config` runs it.
#
# Needs go. It reads config/config.yaml through cmd/printer-listener, which
# prints the listener a printer dials, its port and whether it uses TLS. The
# script stops when that command fails: a settings file with a guessed port
# gives a printer that never connects.
#
# The connector instance it reads is `xbuddy`. A deployment that named its
# connector differently edits the port in the INI by hand.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONFIG_FILE="${PROJECT_ROOT}/config/config.yaml"
OUTPUT_FILE="${PROJECT_ROOT}/prusa_printer_settings.ini"
CONNECTOR=xbuddy

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Config file not found: $CONFIG_FILE"
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "Error: go is required to read $CONFIG_FILE"
    exit 1
fi

if ! ANSWER="$(cd "$PROJECT_ROOT" && go run ./cmd/printer-listener --config "$CONFIG_FILE" --connector "$CONNECTOR")"; then
    exit 1
fi

LISTENER="$(echo "$ANSWER" | tr -d '\r' | sed -n 's/^listener=//p')"
WS_PORT="$(echo "$ANSWER" | tr -d '\r' | sed -n 's/^port=//p')"
TLS_ENABLED="$(echo "$ANSWER" | tr -d '\r' | sed -n 's/^tls=//p')"

if [ -z "$LISTENER" ] || [ -z "$WS_PORT" ] || [ -z "$TLS_ENABLED" ]; then
    echo "Error: cmd/printer-listener printed no listener, port or tls line" >&2
    exit 1
fi

# custom_cert tells the printer to load the certificate `make certificate-prusa`
# wrote, which the operator copies to the USB drive with this file.
CUSTOM_CERT=0
if [ -f "${PROJECT_ROOT}/certs/printer/connect.der" ]; then
    CUSTOM_CERT=1
fi

# This machine's address on the local network. Each step runs only when the
# one before it found nothing.
LOCAL_IP=""
if [[ "$OSTYPE" == "darwin"* ]]; then
    LOCAL_IP=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null)
else
    LOCAL_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    if [ -z "$LOCAL_IP" ]; then
        LOCAL_IP=$(ip route get 8.8.8.8 2>/dev/null | sed -n 's/.* src \([^ ]*\).*/\1/p' | head -1)
    fi
fi

if [ -z "$LOCAL_IP" ]; then
    LOCAL_IP=$(ifconfig 2>/dev/null | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*' | grep -v '127.0.0.1' | head -1)
fi

if [ -z "$LOCAL_IP" ]; then
    LOCAL_IP="localhost"
fi

cat >"$OUTPUT_FILE" <<INI
# Prusa Printer Settings
# Generated automatically from $LISTENER.addr in config/config.yaml
# Printer listener IP: $LOCAL_IP
# Printer listener port: $WS_PORT
# TLS Enabled: $TLS_ENABLED
# Custom cert present: $CUSTOM_CERT
# Generated: $(date)

[service::connect]
hostname=$LOCAL_IP
port=$WS_PORT
tls=$TLS_ENABLED
custom_cert=$CUSTOM_CERT

# No token. On the printer choose Settings > Network > Prusa Connect >
# Add Printer to Connect, and approve the code on the server. The printer
# then stores the token the server issued.
INI

echo "✓ Generated $OUTPUT_FILE"
echo "  Hostname: $LOCAL_IP"
echo "  Port: $WS_PORT"
echo "  TLS: $TLS_ENABLED"
echo "  custom_cert: $CUSTOM_CERT"
echo ""
echo "Copy this file to the root of your USB flash drive and insert it into the printer."
