#!/usr/bin/env bash
# Build release bundles for every supported platform into ./dist, each
# containing the console host + all plugins, then write SHA256SUMS.txt.
# Usage: scripts/dist.sh [version]   (version defaults to `git describe`)
set -euo pipefail

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
PLATFORMS=(darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64)
PLUGINS=$(ls -d cmd/console-plugin-* | sed 's#cmd/##')

rm -rf dist && mkdir -p dist
echo "Building Console $VERSION for ${#PLATFORMS[@]} platforms…"

for plat in "${PLATFORMS[@]}"; do
  os="${plat%/*}"; arch="${plat#*/}"
  name="console_${VERSION}_${os}_${arch}"
  out="dist/$name"; mkdir -p "$out"
  ext=""; [ "$os" = "windows" ] && ext=".exe"

  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-X main.version=$VERSION" -o "$out/console$ext" ./cmd/console
  for p in $PLUGINS; do
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -trimpath -o "$out/$p$ext" "./cmd/$p"
  done
  cp README.md LICENSE CHANGELOG.md SECURITY.md "$out/"

  if [ "$os" = "windows" ]; then
    (cd dist && zip -qr "$name.zip" "$name")
  else
    tar -C dist -czf "dist/$name.tar.gz" "$name"
  fi
  rm -rf "$out"
  echo "  $name"
done

(cd dist && shasum -a 256 ./*.tar.gz ./*.zip > SHA256SUMS.txt)
echo "Wrote dist/SHA256SUMS.txt"
