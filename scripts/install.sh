#!/usr/bin/env sh
set -eu

repo="SilkageNet/codex-switch"
version="${CODEX_SWITCH_VERSION:-latest}"
destination="${CODEX_SWITCH_INSTALL_DIR:-${HOME}/.local/bin}"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Unsupported operating system" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture" >&2; exit 1 ;;
esac

if [ "$version" = latest ]; then
  version=$(curl -fsSL "https://api.github.com/repos/${repo}/releases/latest" | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' | head -1)
else
  version=${version#v}
fi

archive="codex-switch_${version}_${os}_${arch}.tar.gz"
base="https://github.com/${repo}/releases/download/v${version}"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

curl -fsSL "${base}/${archive}" -o "${tmp_dir}/${archive}"
curl -fsSL "${base}/checksums.txt" -o "${tmp_dir}/checksums.txt"

cd "$tmp_dir"
if command -v shasum >/dev/null 2>&1; then
  grep " ${archive}$" checksums.txt | shasum -a 256 -c -
else
  grep " ${archive}$" checksums.txt | sha256sum -c -
fi
tar -xzf "$archive"
mkdir -p "$destination"
install -m 0755 codex-switch "$destination/codex-switch"
echo "Installed codex-switch to $destination/codex-switch"
