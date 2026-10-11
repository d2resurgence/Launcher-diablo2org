#!/bin/bash
# Signs the macOS app and its DMG with the Developer ID certificate, then
# notarizes and staples the DMG, for tagged releases in CI.
#
#   build/darwin/sign.sh app  bin/launcher.app
#   build/darwin/sign.sh dmg  bin/launcher-v1.2.3-universal.dmg
#
# "app" signs the bundled blizzard-legacy-dl first (codesign --deep doesn't
# reach Contents/Resources/bin), then the bundle, with the hardened runtime
# and a secure timestamp. "dmg" signs the disk image, submits it to Apple's
# notary service and waits, then staples the ticket so it opens offline.
#
# Secrets come from the environment and are never printed:
#   APPLE_CERT_P12       the Developer ID Application certificate, base64 .p12
#   APPLE_CERT_PASSWORD  its password
#   APPLE_TEAM_ID        the team the certificate belongs to
# and for notarizing, either an App Store Connect API key:
#   APPLE_API_KEY        base64 .p8 key
#   APPLE_API_KEY_ID     its key id
#   APPLE_API_ISSUER     its issuer id
# or an Apple ID:
#   APPLE_ID, APPLE_APP_PASSWORD (an app-specific password)
set -euo pipefail

mode=${1:?usage: sign.sh app|dmg PATH}
target=${2:?usage: sign.sh app|dmg PATH}

: "${APPLE_CERT_P12:?}" "${APPLE_CERT_PASSWORD:?}" "${APPLE_TEAM_ID:?}"

work=$(mktemp -d)
keychain="$work/signing.keychain-db"
keychain_password=$(uuidgen)
cleanup() {
  security delete-keychain "$keychain" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

# A keychain of its own, so the certificate never touches the runner's.
security create-keychain -p "$keychain_password" "$keychain"
security set-keychain-settings -lut 3600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
printf '%s' "$APPLE_CERT_P12" | base64 --decode > "$work/cert.p12"
security import "$work/cert.p12" -k "$keychain" -P "$APPLE_CERT_PASSWORD" -T /usr/bin/codesign >/dev/null
security set-key-partition-list -S apple-tool:,apple: -s -k "$keychain_password" "$keychain" >/dev/null
security list-keychains -d user -s "$keychain" $(security list-keychains -d user | tr -d '"')

identity=$(security find-identity -v -p codesigning "$keychain" | grep "Developer ID Application" | grep "($APPLE_TEAM_ID)" | head -1 | awk '{print $2}')
if [ -z "$identity" ]; then
  echo "no Developer ID Application identity for team $APPLE_TEAM_ID" >&2
  exit 1
fi

sign() {
  codesign --force --options runtime --timestamp --keychain "$keychain" --sign "$identity" "$@"
}

case "$mode" in
app)
  sign "$target/Contents/Resources/bin/blizzard-legacy-dl"
  sign "$target"
  codesign --verify --strict --deep --verbose=2 "$target"
  ;;
dmg)
  sign "$target"
  if [ -n "${APPLE_API_KEY:-}" ]; then
    : "${APPLE_API_KEY_ID:?}" "${APPLE_API_ISSUER:?}"
    printf '%s' "$APPLE_API_KEY" | base64 --decode > "$work/key.p8"
    auth=(--key "$work/key.p8" --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER")
  else
    : "${APPLE_ID:?set APPLE_API_KEY or APPLE_ID}" "${APPLE_APP_PASSWORD:?}"
    auth=(--apple-id "$APPLE_ID" --password "$APPLE_APP_PASSWORD" --team-id "$APPLE_TEAM_ID")
  fi
  xcrun notarytool submit "$target" "${auth[@]}" --wait --timeout 1h
  xcrun stapler staple "$target"
  xcrun stapler validate "$target"
  spctl --assess --type open --context context:primary-signature --verbose=2 "$target"
  ;;
*)
  echo "unknown mode $mode" >&2
  exit 2
  ;;
esac
