#!/usr/bin/env sh
# Install the agtk CLI from a tagged GitHub Release.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/pedromvgomes/agentic-toolkit/main/install.sh | sh
#   curl -fsSL .../install.sh | AGTK_VERSION=v0.1.0 sh
#   curl -fsSL .../install.sh | AGTK_INSTALL_DIR=$HOME/bin sh
#
# Environment overrides (all optional):
#   AGTK_VERSION         Tag to install (e.g. v0.1.0). Default: latest release.
#   AGTK_INSTALL_DIR     Where to put the binary. Default: /usr/local/bin if
#                        writable, else $HOME/.local/bin (with a PATH hint).
#   AGTK_OS              Override detected OS (darwin or linux).
#   AGTK_ARCH            Override detected arch (amd64 or arm64).
#   AGTK_NO_COMPLETION   Skip shell-completion install. Default: install for
#                        bash/zsh/fish based on $SHELL.
#
# After install, run `agtk --version` to confirm. Use `agtk update` for
# subsequent upgrades — the installed binary self-replaces from the same
# release archives this script downloads.

set -eu

repo="pedromvgomes/agentic-toolkit"
binary="agtk"

err() {
	printf '%s\n' "install.sh: $*" >&2
	exit 1
}

info() {
	printf '%s\n' "$*"
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

need_cmd uname
need_cmd tar
need_cmd mkdir
need_cmd mv
need_cmd rm
need_cmd mktemp

# Pick a downloader. curl preferred, wget as fallback.
if command -v curl >/dev/null 2>&1; then
	downloader="curl"
elif command -v wget >/dev/null 2>&1; then
	downloader="wget"
else
	err "need either curl or wget on PATH"
fi

# Pick a sha256 tool.
if command -v shasum >/dev/null 2>&1; then
	sha256_cmd="shasum -a 256"
elif command -v sha256sum >/dev/null 2>&1; then
	sha256_cmd="sha256sum"
else
	err "need either shasum or sha256sum on PATH"
fi

# ===== platform detection =====

os="${AGTK_OS:-}"
if [ -z "$os" ]; then
	case "$(uname -s)" in
		Darwin) os="darwin" ;;
		Linux)  os="linux" ;;
		*) err "unsupported OS: $(uname -s) (set AGTK_OS=darwin|linux to override)" ;;
	esac
fi

arch="${AGTK_ARCH:-}"
if [ -z "$arch" ]; then
	case "$(uname -m)" in
		x86_64|amd64)  arch="amd64" ;;
		arm64|aarch64) arch="arm64" ;;
		*) err "unsupported arch: $(uname -m) (set AGTK_ARCH=amd64|arm64 to override)" ;;
	esac
fi

# ===== version resolution =====

version="${AGTK_VERSION:-}"
if [ -z "$version" ]; then
	api_url="https://api.github.com/repos/$repo/releases/latest"
	case "$downloader" in
		curl) tag_line=$(curl -fsSL "$api_url" | grep -E '"tag_name"' | head -n 1) ;;
		wget) tag_line=$(wget -qO- "$api_url" | grep -E '"tag_name"' | head -n 1) ;;
	esac
	# Extract the value: "tag_name": "v0.1.0",
	version=$(printf '%s' "$tag_line" | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
	[ -n "$version" ] || err "could not resolve latest release tag from $api_url"
fi
case "$version" in
	v*) ;;
	*) err "AGTK_VERSION must start with 'v' (got '$version')" ;;
esac
# Strip leading "v" for archive filename.
version_num="${version#v}"

# ===== install location =====

install_dir="${AGTK_INSTALL_DIR:-}"
hint_path=0
if [ -z "$install_dir" ]; then
	if [ -w "/usr/local/bin" ]; then
		install_dir="/usr/local/bin"
	else
		install_dir="$HOME/.local/bin"
		hint_path=1
	fi
fi
mkdir -p "$install_dir"
target="$install_dir/$binary"

# ===== download + verify =====

archive="${binary}_${version_num}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$version"
archive_url="$base_url/$archive"
checksums_url="$base_url/checksums.txt"

tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

archive_path="$tmp/$archive"
checksums_path="$tmp/checksums.txt"

info "downloading $archive_url"
case "$downloader" in
	curl) curl -fsSL -o "$archive_path"   "$archive_url"   || err "download failed: $archive_url" ;;
	wget) wget -q   -O "$archive_path"   "$archive_url"   || err "download failed: $archive_url" ;;
esac
case "$downloader" in
	curl) curl -fsSL -o "$checksums_path" "$checksums_url" || err "download failed: $checksums_url" ;;
	wget) wget -q   -O "$checksums_path" "$checksums_url" || err "download failed: $checksums_url" ;;
esac

info "verifying sha256"
expected=$(grep " $archive\$" "$checksums_path" | awk '{print $1}')
[ -n "$expected" ] || err "$archive not listed in checksums.txt"
actual=$(eval "$sha256_cmd \"$archive_path\"" | awk '{print $1}')
if [ "$expected" != "$actual" ]; then
	err "checksum mismatch for $archive (expected $expected, got $actual)"
fi

# ===== extract + install =====

info "extracting to $tmp"
tar -xzf "$archive_path" -C "$tmp"
[ -f "$tmp/$binary" ] || err "$binary not found in archive"

info "installing to $target"
chmod +x "$tmp/$binary"
mv "$tmp/$binary" "$target"

info ""
info "installed $binary $version → $target"

if [ "$hint_path" = 1 ]; then
	case ":$PATH:" in
		*":$install_dir:"*) ;;
		*)
			info ""
			info "note: $install_dir is not on your PATH."
			info "  add this to your shell profile:"
			info "    export PATH=\"$install_dir:\$PATH\""
			;;
	esac
fi

# ===== shell completion =====

install_completion() {
	if [ "${AGTK_NO_COMPLETION:-0}" = "1" ]; then
		return 0
	fi
	shell_name=$(basename "${SHELL:-}")
	case "$shell_name" in
		bash)
			comp_dir="${BASH_COMPLETION_USER_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions}"
			comp_file="$comp_dir/$binary"
			mkdir -p "$comp_dir" || return 0
			"$target" completion bash >"$comp_file" 2>/dev/null || return 0
			info ""
			info "installed bash completion → $comp_file"
			info "  (requires the bash-completion package; restart your shell to pick it up)"
			;;
		zsh)
			comp_dir="${XDG_DATA_HOME:-$HOME/.local/share}/zsh/site-functions"
			comp_file="$comp_dir/_$binary"
			mkdir -p "$comp_dir" || return 0
			"$target" completion zsh >"$comp_file" 2>/dev/null || return 0
			info ""
			info "installed zsh completion → $comp_file"
			info "  ensure your ~/.zshrc has '$comp_dir' on \$fpath, e.g."
			info "    fpath=($comp_dir \$fpath)"
			info "    autoload -U compinit && compinit"
			;;
		fish)
			comp_dir="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions"
			comp_file="$comp_dir/$binary.fish"
			mkdir -p "$comp_dir" || return 0
			"$target" completion fish >"$comp_file" 2>/dev/null || return 0
			info ""
			info "installed fish completion → $comp_file"
			info "  (fish auto-loads completions from this directory)"
			;;
		"")
			info ""
			info "could not detect your shell; run '$binary completion <shell>' to set up"
			info "  completion manually. Supported: bash, zsh, fish, powershell."
			;;
		*)
			info ""
			info "shell '$shell_name' detected — no automatic completion install."
			info "  run '$binary completion <shell>' for a shell agtk supports"
			info "  (bash, zsh, fish, powershell)."
			;;
	esac
}

install_completion

info ""
info "run '$binary --version' to confirm. 'agtk update' upgrades in place."
