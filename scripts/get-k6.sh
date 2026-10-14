#!/usr/bin/env bash
# Download the pinned k6 release into .tmp/bin and verify it against pinned SHA-256 values
# and the release's published checksums file. Works on Linux and in Git Bash on Windows.
set -euo pipefail

VERSION="v2.3.0"
SHA_LINUX_AMD64="39c3117b6af817592dcd0ce4242105c0a7af10948c2a425306f0be8f7a8a8ab1"
SHA_WINDOWS_AMD64="112276d495e5741c968e2bc09ea6196099c1275bd6db9ee0875d173c7148ce43"
SHA_CHECKSUMS="8ebbfcd5a13d68ac67d488c2b5f28bf901ea22d2fbbfb15f5345d65c39163be2"

root="$(cd "$(dirname "$0")/.." && pwd)"
dl="$root/.tmp/k6dl"
bin="$root/.tmp/bin"
mkdir -p "$dl" "$bin"

case "$(uname -s)" in
  Linux*) asset="k6-$VERSION-linux-amd64.tar.gz"; want="$SHA_LINUX_AMD64"; exe="k6" ;;
  MINGW* | MSYS* | CYGWIN*) asset="k6-$VERSION-windows-amd64.zip"; want="$SHA_WINDOWS_AMD64"; exe="k6.exe" ;;
  *) echo "get-k6: unsupported platform $(uname -s)" >&2; exit 1 ;;
esac
[ "$(uname -m)" = "x86_64" ] || { echo "get-k6: only amd64 is pinned" >&2; exit 1; }

if [ "${FORCE:-}" != 1 ] && [ -x "$bin/$exe" ] && "$bin/$exe" version 2>/dev/null | grep -q "$VERSION "; then
  echo "get-k6: $bin/$exe is already $VERSION"
  exit 0
fi

base="https://github.com/grafana/k6/releases/download/$VERSION"
curl -fsSL --proto '=https' --tlsv1.2 -o "$dl/checksums.txt" "$base/k6-$VERSION-checksums.txt"
[ -f "$dl/$asset" ] || curl -fsSL --proto '=https' --tlsv1.2 -o "$dl/$asset" "$base/$asset"

# Both the pinned hash and the published checksums file must match.
echo "$SHA_CHECKSUMS  $dl/checksums.txt" | sha256sum -c --quiet -
echo "$want  $dl/$asset" | sha256sum -c --quiet -
grep -q "^$want  $asset\$" "$dl/checksums.txt" || { echo "get-k6: $asset not in checksums.txt with the pinned hash" >&2; exit 1; }

rm -rf "$dl/x" && mkdir -p "$dl/x"
case "$asset" in
  *.zip) unzip -q "$dl/$asset" -d "$dl/x" ;;
  *) tar -xzf "$dl/$asset" -C "$dl/x" ;;
esac
cp "$dl/x/k6-$VERSION-"*/"$exe" "$bin/$exe"
chmod +x "$bin/$exe"
K6_NO_USAGE_REPORT=true "$bin/$exe" version
