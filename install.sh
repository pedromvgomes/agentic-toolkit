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
#
# Path strategy mirrors github.com/pedromvgomes/gt's installer: pick a
# location the shell already loads from when one is available, fall
# back to a conventional dir with an explicit fpath/source hint when
# not. The previous version used XDG paths that, while tidy, are not
# on zsh's default $fpath — files landed but completions never loaded.
#
# zsh:
#   - $(brew --prefix)/share/zsh/site-functions/_agtk when brew is
#     present and the dir is writable (already on default $fpath for
#     brew-installed zsh).
#   - else ~/.zsh/completions/_agtk (with a hint to add it to $fpath
#     in ~/.zshrc before compinit).
# bash:
#   - ~/.local/share/bash-completion/completions/agtk
#     (auto-loaded by the bash-completion package).
# fish:
#   - ~/.config/fish/completions/agtk.fish (fish auto-loads).

completion_path() {
	case "$1" in
		zsh)
			if command -v brew >/dev/null 2>&1; then
				brew_prefix=$(brew --prefix 2>/dev/null || true)
				if [ -n "$brew_prefix" ] && [ -d "$brew_prefix/share/zsh/site-functions" ] && [ -w "$brew_prefix/share/zsh/site-functions" ]; then
					echo "$brew_prefix/share/zsh/site-functions/_$binary"
					return 0
				fi
			fi
			echo "$HOME/.zsh/completions/_$binary"
			;;
		bash) echo "$HOME/.local/share/bash-completion/completions/$binary" ;;
		fish) echo "$HOME/.config/fish/completions/$binary.fish" ;;
		*) return 1 ;;
	esac
}

completion_note() {
	case "$1" in
		zsh)
			case "$2" in
				*/site-functions/_*) echo "restart your shell (e.g. 'exec zsh') to load completions" ;;
				*) echo "add 'fpath=(~/.zsh/completions \$fpath)' to ~/.zshrc before 'compinit', then restart your shell" ;;
			esac
			;;
		bash) echo "ensure the bash-completion package is installed; restart your shell to load" ;;
		fish) echo "fish auto-loads from this directory; new shells pick it up immediately" ;;
	esac
}

install_completion() {
	if [ "${AGTK_NO_COMPLETION:-0}" = "1" ]; then
		return 0
	fi
	shell_name=$(basename "${SHELL:-}")
	case "$shell_name" in
		bash|zsh|fish) ;;
		"")
			info ""
			info "could not detect your shell; run '$binary completion <shell>' to set up"
			info "  completion manually. Supported: bash, zsh, fish, powershell."
			return 0
			;;
		*)
			info ""
			info "shell '$shell_name' detected — no automatic completion install."
			info "  run '$binary completion <shell>' for a shell agtk supports"
			info "  (bash, zsh, fish, powershell)."
			return 0
			;;
	esac
	[ -n "${HOME:-}" ] || return 0
	comp_path=$(completion_path "$shell_name") || return 0
	mkdir -p "$(dirname "$comp_path")" || return 0
	"$target" completion "$shell_name" >"$comp_path" || return 0
	info ""
	info "installed $shell_name completion → $comp_path"
	note=$(completion_note "$shell_name" "$comp_path")
	[ -z "$note" ] || info "  note: $note"
}

install_completion

info ""
info "run '$binary --version' to confirm. 'agtk update' upgrades in place."
