#!/usr/bin/env bash
# update-sri.sh — recompute SHA-384 hashes for all JS assets and update
# index.html (modulepreload + script integrity) and app.js (LIB_INTEGRITY map).
# Run this after editing any file in web/js/ or web/lib/.
#
# Usage: bash scripts/update-sri.sh

set -euo pipefail

WEB="$(cd "$(dirname "$0")/../web" && pwd)"
INDEX="$WEB/index.html"
APPJS="$WEB/js/app.js"

hash_file() {
  openssl dgst -sha384 -binary "$1" | openssl base64 -A
}

echo "Computing hashes..."

H_APP=$(hash_file "$WEB/js/app.js")
H_TRANSPORT=$(hash_file "$WEB/js/transport.js")
H_CRYPTO=$(hash_file "$WEB/js/crypto.js")
H_CLIPBOARD=$(hash_file "$WEB/js/clipboard.js")
H_DEVICE=$(hash_file "$WEB/js/device.js")
H_UI=$(hash_file "$WEB/js/ui.js")
H_STREAM=$(hash_file "$WEB/js/stream.js")
H_WEBRTC=$(hash_file "$WEB/js/webrtc.js")
H_PREVIEW=$(hash_file "$WEB/js/preview.js")
H_PDF=$(hash_file "$WEB/js/pdf-thumbnail.js")
H_NACL=$(hash_file "$WEB/lib/tweetnacl.min.js")
H_NACL_UTIL=$(hash_file "$WEB/lib/tweetnacl-util.min.js")
H_QR=$(hash_file "$WEB/lib/qrcode.min.js")

echo "Updating index.html..."

# Update modulepreload integrity attributes
sed -i "s|href=\"/js/transport.js\"[^>]*|href=\"/js/transport.js\"    integrity=\"sha384-${H_TRANSPORT}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/crypto.js\"[^>]*|href=\"/js/crypto.js\"       integrity=\"sha384-${H_CRYPTO}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/clipboard.js\"[^>]*|href=\"/js/clipboard.js\"    integrity=\"sha384-${H_CLIPBOARD}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/device.js\"[^>]*|href=\"/js/device.js\"       integrity=\"sha384-${H_DEVICE}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/ui.js\"[^>]*|href=\"/js/ui.js\"           integrity=\"sha384-${H_UI}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/stream.js\"[^>]*|href=\"/js/stream.js\"       integrity=\"sha384-${H_STREAM}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/webrtc.js\"[^>]*|href=\"/js/webrtc.js\"       integrity=\"sha384-${H_WEBRTC}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/preview.js\"[^>]*|href=\"/js/preview.js\"      integrity=\"sha384-${H_PREVIEW}\" crossorigin=\"anonymous\"|" "$INDEX"
sed -i "s|href=\"/js/pdf-thumbnail.js\"[^>]*|href=\"/js/pdf-thumbnail.js\" integrity=\"sha384-${H_PDF}\" crossorigin=\"anonymous\"|" "$INDEX"

# Update app.js script tag integrity
sed -i "s|src=\"/js/app.js\"[^>]*|src=\"/js/app.js\" integrity=\"sha384-${H_APP}\" crossorigin=\"anonymous\"|" "$INDEX"

echo "Updating app.js LIB_INTEGRITY..."

# Update LIB_INTEGRITY map entries
sed -i "s|'sha384-[^']*'\(.*tweetnacl\.min\.js'\)|'sha384-${H_NACL}'\1|" "$APPJS"
sed -i "s|'sha384-[^']*'\(.*tweetnacl-util\.min\.js'\)|'sha384-${H_NACL_UTIL}'\1|" "$APPJS"
sed -i "s|'sha384-[^']*'\(.*qrcode\.min\.js'\)|'sha384-${H_QR}'\1|" "$APPJS"

echo ""
echo "Done. Hashes updated:"
echo "  app.js            sha384-${H_APP}"
echo "  transport.js      sha384-${H_TRANSPORT}"
echo "  crypto.js         sha384-${H_CRYPTO}"
echo "  clipboard.js      sha384-${H_CLIPBOARD}"
echo "  device.js         sha384-${H_DEVICE}"
echo "  ui.js             sha384-${H_UI}"
echo "  stream.js         sha384-${H_STREAM}"
echo "  webrtc.js         sha384-${H_WEBRTC}"
echo "  preview.js        sha384-${H_PREVIEW}"
echo "  pdf-thumbnail.js  sha384-${H_PDF}"
echo "  tweetnacl         sha384-${H_NACL}"
echo "  tweetnacl-util    sha384-${H_NACL_UTIL}"
echo "  qrcode            sha384-${H_QR}"
echo ""
echo "Remember to also bump the cache version in web/sw.js!"
