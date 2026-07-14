#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
OUTPUT_DIR="${1:-${ROOT_DIR}/output/license-studio}"
APP_DIR="${OUTPUT_DIR}/UniRouter License Studio.app"
CONTENTS_DIR="${APP_DIR}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "License Studio .app can only be packaged on macOS." >&2
  exit 1
fi

if [ -e "${APP_DIR}" ]; then
  echo "Refusing to overwrite existing app: ${APP_DIR}" >&2
  echo "Choose a new output folder or remove that exact app manually." >&2
  exit 1
fi

mkdir -p "${MACOS_DIR}"
cp "${ROOT_DIR}/packaging/license-studio/macos/Info.plist" "${CONTENTS_DIR}/Info.plist"

(
  cd "${ROOT_DIR}"
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o "${MACOS_DIR}/uniroute-license-studio" \
    ./cmd/uniroute-license-studio
)
chmod 0755 "${MACOS_DIR}/uniroute-license-studio"

if command -v codesign >/dev/null 2>&1; then
  IDENTITY="${CODESIGN_IDENTITY:--}"
  if [ "${IDENTITY}" = "-" ]; then
    codesign --force --sign - "${APP_DIR}"
  else
    codesign --force --options runtime --timestamp --sign "${IDENTITY}" "${APP_DIR}"
  fi
  codesign --verify --strict --verbose=2 "${APP_DIR}"
fi

echo "${APP_DIR}"
