#!/bin/sh
set -eu

repository="Inzaniak/rts"
install_dir="${RTS_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "RTS supports macOS and Linux." >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "RTS does not provide a binary for $(uname -m)." >&2; exit 1 ;;
esac

latest_url="https://github.com/$repository/releases/latest"
resolved_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$latest_url")"
tag="${resolved_url##*/}"
version="${tag#v}"
asset="rts_${version}_${os}_${arch}.tar.gz"
download_base="https://github.com/$repository/releases/download/$tag"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT INT TERM

curl -fsSL "$download_base/$asset" -o "$temp_dir/$asset"
curl -fsSL "$download_base/checksums.txt" -o "$temp_dir/checksums.txt"

expected="$(awk -v asset="$asset" '$2 == asset || $2 == "*" asset { print $1 }' "$temp_dir/checksums.txt")"
if [ -z "$expected" ]; then
  echo "No checksum found for $asset." >&2
  exit 1
fi
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$temp_dir/$asset" | awk '{print $1}')"
else
  actual="$(sha256sum "$temp_dir/$asset" | awk '{print $1}')"
fi
if [ "$actual" != "$expected" ]; then
  echo "Checksum verification failed for $asset." >&2
  exit 1
fi

tar -xzf "$temp_dir/$asset" -C "$temp_dir"
mkdir -p "$install_dir"
install -m 0755 "$temp_dir/rts" "$install_dir/rts"

echo "Installed RTS $tag to $install_dir/rts"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to your PATH to run rts." ;;
esac
