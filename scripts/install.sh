#!/bin/sh
# cpu_bench_go installer
#
# Usage:
#   curl -fsSL https://cpu-bench.hra42.com/install | sh
#
# Env overrides:
#   VERSION       release tag (default: latest; accepts "1.2.3" or "v1.2.3")
#   INSTALL_DIR   where to install (default: /usr/local/bin, falls back to ~/.local/bin)
#   BIN_NAME      installed binary name (default: cpu_bench_go)

set -eu

REPO="hra42/cpu_benchmark_go"
BIN_NAME="${BIN_NAME:-cpu_bench_go}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

info() {
	printf '==> %s\n' "$*" >&2
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

# --- detect OS ---
uname_s=$(uname -s)
case "$uname_s" in
	Darwin) os=darwin ;;
	Linux)  os=linux ;;
	MINGW*|MSYS*|CYGWIN*)
		die "Windows is not supported; use WSL"
		;;
	*)
		die "unsupported OS: $uname_s"
		;;
esac

# --- detect arch ---
uname_m=$(uname -m)
case "$uname_m" in
	x86_64|amd64)   arch=amd64 ;;
	aarch64|arm64)  arch=arm64 ;;
	riscv64)        arch=riscv64 ;;
	*)
		die "unsupported architecture: $uname_m"
		;;
esac

# --- map to release asset ---
case "$os/$arch" in
	darwin/arm64)
		asset="cpu_bench_go-macos-arm64"
		;;
	darwin/amd64)
		die "Intel Macs are not shipped as prebuilt binaries; build from source: https://github.com/${REPO}#build-from-source"
		;;
	linux/amd64)
		asset="cpu_bench_go-linux-amd64"
		;;
	linux/arm64)
		asset="cpu_bench_go-linux-arm64"
		;;
	linux/riscv64)
		asset="cpu_bench_go-linux-riscv64"
		;;
	*)
		die "no prebuilt binary for $os/$arch"
		;;
esac

# --- normalize version + build URL ---
if [ "$VERSION" = "latest" ]; then
	url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
	case "$VERSION" in
		v*) tag="$VERSION" ;;
		*)  tag="v$VERSION" ;;
	esac
	url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
fi

# --- tmp + cleanup ---
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t cpubench)
trap 'rm -rf "$tmp"' EXIT INT TERM

# --- download ---
info "downloading $asset ($VERSION)"
if command -v curl >/dev/null 2>&1; then
	curl -fsSL -o "$tmp/$BIN_NAME" "$url" \
		|| die "download failed: $url"
elif command -v wget >/dev/null 2>&1; then
	wget -qO "$tmp/$BIN_NAME" "$url" \
		|| die "download failed: $url"
else
	die "neither curl nor wget found in PATH"
fi

chmod +x "$tmp/$BIN_NAME"

# --- pick install dir ---
dest_dir="$INSTALL_DIR"
need_sudo=0
fell_back=0

if [ -d "$dest_dir" ] && [ -w "$dest_dir" ]; then
	:
elif [ ! -d "$dest_dir" ] && [ -w "$(dirname "$dest_dir")" ] 2>/dev/null; then
	mkdir -p "$dest_dir"
elif command -v sudo >/dev/null 2>&1 && [ -t 0 ]; then
	need_sudo=1
else
	dest_dir="$HOME/.local/bin"
	mkdir -p "$dest_dir"
	fell_back=1
fi

dest="$dest_dir/$BIN_NAME"

if [ "$need_sudo" = "1" ]; then
	info "installing to $dest (requires sudo)"
	sudo mv "$tmp/$BIN_NAME" "$dest"
else
	info "installing to $dest"
	mv "$tmp/$BIN_NAME" "$dest"
fi

# --- macOS: strip quarantine so unsigned binary runs without Gatekeeper popup ---
if [ "$os" = "darwin" ] && command -v xattr >/dev/null 2>&1; then
	xattr -d com.apple.quarantine "$dest" 2>/dev/null || true
fi

# --- final message + PATH hint if needed ---
if [ "$fell_back" = "1" ]; then
	case ":$PATH:" in
		*":$dest_dir:"*) ;;
		*)
			info "$dest_dir is not on \$PATH"
			info "add this to your shell profile:"
			info '  export PATH="$HOME/.local/bin:$PATH"'
			;;
	esac
fi

info "installed $BIN_NAME to $dest"
info "run: $BIN_NAME --help"
