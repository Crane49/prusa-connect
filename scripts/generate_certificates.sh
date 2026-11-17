#!/bin/bash
#
# Generate TLS certificates for Prusa Connect.
#
# Chain used (intentionally flat - no intermediate CA):
#
#   Root CA  (ECDSA P-256, CA:TRUE)
#     ├── api.crt: HTTPS / WSS API server (browser-facing)
#     └── printer.crt: HTTPS / WSS printer-facing listener
#
#   certs/printer/connect.der: root CA exported as DER; copy to USB flash
#                               so the xBuddy printer trusts our server.
#
# Why no intermediate CA?
#   xBuddy firmware (mbedTLS) validates the full presented chain against its
#   trust anchor (connect.der).  The TLS server must therefore send every
#   cert between the leaf and the trust anchor.  Keeping the chain flat means
#   config.yaml can reference the leaf cert directly and nothing is silently
#   omitted from the handshake.
#
# Usage:
#   ./scripts/generate_certificates.sh          # all certs
#   ./scripts/generate_certificates.sh server   # API cert only
#   ./scripts/generate_certificates.sh prusa    # printer cert + connect.der only
#
# Environment overrides:
#   API_CERT_CN   API_CERT_DNS   API_CERT_IPS
#   PRUSA_CERT_CN PRUSA_CERT_DNS PRUSA_CERT_IPS

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

MODE="${1:-all}"

CERT_ROOT_DIR="${PROJECT_ROOT}/certs"
CA_DIR="${CERT_ROOT_DIR}/ca"
SERVER_DIR="${CERT_ROOT_DIR}/server"
PRINTER_DIR="${CERT_ROOT_DIR}/printer"

ROOT_KEY="${CA_DIR}/root-ca.key"
ROOT_CERT="${CA_DIR}/root-ca.crt"

API_CN="${API_CERT_CN:-localhost}"
API_DNS="${API_CERT_DNS:-localhost}"
API_IPS="${API_CERT_IPS:-127.0.0.1}"

PRINTER_CN="${PRUSA_CERT_CN:-prusa-connect.local}"
PRINTER_DNS="${PRUSA_CERT_DNS:-prusa-connect.local}"
PRINTER_IPS="${PRUSA_CERT_IPS:-127.0.0.1}"

# Git Bash rewrites an argument that looks like a Unix path into a Windows one,
# so openssl receives the -subj value below as "C:/Program Files/Git/C=CZ/..."
# and refuses it. This names the one argument shape to leave alone: a value
# starting "/C=", which is the subject and never a file path, because a
# converted path starts "/c/" and the match is case sensitive. Other shells
# ignore the variable.
export MSYS2_ARG_CONV_EXCL="/C="

mkdir -p "$CA_DIR" "$SERVER_DIR" "$PRINTER_DIR"

# Build a comma-separated SAN string from DNS and IP CSV lists.
#
# Every IP entry is duplicated as a DNS:<ip> string. This is a workaround for
# mbedtls 2.x in xBuddy firmware: its hostname verification compares the
# string passed to mbedtls_ssl_set_hostname() byte-for-byte against DNS SAN
# entries only: IP SAN entries are not compared against an IP-shaped hostname
# string. Without the DNS:<ip> duplicate, a printer configured with
# `hostname=192.168.1.50` in prusa_printer_settings.ini will fail handshake
# with MBEDTLS_X509_BADCERT_CN_MISMATCH (0x04) even though the cert is correctly
# signed by the trusted CA and the IP matches an IP: SAN entry. mbedtls 3.x
# handles IP-string matching correctly, but Buddy is stuck on 2.x.
# `DNS:<ip>` is technically not a valid DNS name per RFC 5280, but CA/Browser
# Forum has historically tolerated it and string equality is the only check
# done by mbedtls 2.x.
build_san_entries() {
    local dns_csv="$1"
    local ip_csv="$2"
    local entries=()
    local item

    IFS=',' read -r -a dns_items <<< "$dns_csv"
    for item in "${dns_items[@]}"; do
        item="$(echo "$item" | xargs)"
        [ -n "$item" ] && entries+=("DNS:${item}")
    done

    IFS=',' read -r -a ip_items <<< "$ip_csv"
    for item in "${ip_items[@]}"; do
        item="$(echo "$item" | xargs)"
        if [ -n "$item" ]; then
            entries+=("IP:${item}")
            entries+=("DNS:${item}")  # xBuddy mbedtls 2.x hostname-IP workaround
        fi
    done

    local joined="" idx
    for idx in "${!entries[@]}"; do
        [ "$idx" -gt 0 ] && joined+=","
        joined+="${entries[$idx]}"
    done
    printf '%s' "$joined"
}

# Generate the root CA if it does not already exist.
ensure_ca() {
    if [ -f "$ROOT_KEY" ] && [ -f "$ROOT_CERT" ]; then
        return
    fi
    echo "Generating root CA..."
    openssl ecparam -name prime256v1 -genkey -noout -out "$ROOT_KEY"
    # basicConstraints CA:TRUE is required by mbedTLS - without it the cert is
    # treated as an end-entity cert and rejected as a trust anchor.
    openssl req -x509 -new -sha256 \
        -key "$ROOT_KEY" \
        -out "$ROOT_CERT" \
        -days 3650 \
        -subj "/C=CZ/O=Prusa Connect/CN=Prusa Connect Root CA" \
        -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \
        -addext "keyUsage=critical,keyCertSign,cRLSign" \
        -addext "subjectKeyIdentifier=hash"
}

# Generate a leaf server certificate signed directly by the root CA.
generate_server_cert() {
    local name="$1"
    local cn="$2"
    local dns_csv="$3"
    local ip_csv="$4"

    local key="${SERVER_DIR}/${name}.key"
    local csr="${SERVER_DIR}/${name}.csr"
    local crt="${SERVER_DIR}/${name}.crt"
    local ext="${SERVER_DIR}/${name}-ext.cnf"

    local san
    san="$(build_san_entries "$dns_csv" "$ip_csv")"

    echo "Generating ${name} certificate (CN=${cn}, SAN=${san})..."
    openssl ecparam -name prime256v1 -genkey -noout -out "$key"
    openssl req -new -sha256 \
        -key "$key" \
        -out "$csr" \
        -subj "/C=CZ/O=Prusa Connect/CN=${cn}"

    cat > "$ext" <<EOF
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
extendedKeyUsage=serverAuth
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid,issuer
subjectAltName=${san}
EOF

    openssl x509 -req -sha256 \
        -in "$csr" \
        -CA "$ROOT_CERT" \
        -CAkey "$ROOT_KEY" \
        -CAcreateserial \
        -out "$crt" \
        -days 825 \
        -extfile "$ext"

    rm -f "$ext"
}

# Export the root CA as DER - this file goes on the printer's USB flash drive.
generate_printer_trust() {
    echo "Generating printer trust store (connect.der)..."
    openssl x509 -in "$ROOT_CERT" -outform DER -out "${PRINTER_DIR}/connect.der"
}

ensure_ca

case "$MODE" in
    server)
        generate_server_cert "api" "$API_CN" "$API_DNS" "$API_IPS"
        ;;
    prusa)
        generate_server_cert "printer" "$PRINTER_CN" "$PRINTER_DNS" "$PRINTER_IPS"
        generate_printer_trust
        ;;
    all)
        generate_server_cert "api" "$API_CN" "$API_DNS" "$API_IPS"
        generate_server_cert "printer" "$PRINTER_CN" "$PRINTER_DNS" "$PRINTER_IPS"
        generate_printer_trust
        ;;
    *)
        echo "Usage: $0 [server|prusa|all]" >&2
        exit 1
        ;;
esac

echo ""
echo "Done. Certificate files:"
echo "  Root CA:          ${ROOT_CERT}"
if [ "$MODE" = "server" ] || [ "$MODE" = "all" ]; then
    echo "  API cert:         ${SERVER_DIR}/api.crt"
    echo "  API key:          ${SERVER_DIR}/api.key"
fi
if [ "$MODE" = "prusa" ] || [ "$MODE" = "all" ]; then
    echo "  Printer cert:     ${SERVER_DIR}/printer.crt"
    echo "  Printer key:      ${SERVER_DIR}/printer.key"
    echo "  Printer DER:      ${PRINTER_DIR}/connect.der"
    echo ""
    echo "Copy connect.der to the root of the USB flash drive together with"
    echo "prusa_printer_settings.ini (use scripts/generate_printer_config.sh)."
fi
