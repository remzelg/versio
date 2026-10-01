#!/bin/sh
# Installs versio for the current user on macOS or Linux:
#
#	curl -fsSL http://127.0.0.1:8080/install.sh | sh
#
# It downloads the launcher for this OS and CPU, checks it against the
# published SHA256SUMS, installs it as ~/.local/bin/versio, and runs it once so
# the launcher downloads and verifies the app itself. No sudo is needed.
#
# Environment overrides:
#
#	VERSIO_BASE_URL        release host (default http://127.0.0.1:8080)
#	VERSIO_INSTALL_DIR     where the versio command goes (default ~/.local/bin)
#	VERSIO_NO_MODIFY_PATH  "1" leaves shell profiles alone
#
# Everything is inside main, which is called on the last line, so a download
# cut off partway through defines nothing and runs nothing.

set -eu

main() {
	base_url="${VERSIO_BASE_URL:-http://127.0.0.1:8080}"
	bin_dir="${VERSIO_INSTALL_DIR:-$HOME/.local/bin}"

	detect_platform
	name="versio-launcher_${os}_${arch}"
	url="$base_url/versio/launcher"

	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT

	say "Installing versio..."
	curl -fsSL -o "$tmp/$name" "$url/$name" || fail "could not download versio from $base_url"
	curl -fsSL -o "$tmp/SHA256SUMS" "$url/SHA256SUMS" || fail "could not download versio from $base_url"

	expected="$(awk -v f="$name" '$2 == f { print $1 }' "$tmp/SHA256SUMS")"
	[ -n "$expected" ] || fail "could not verify the download; please try again"
	[ "$(sha256 "$tmp/$name")" = "$expected" ] || fail "the download was corrupted; please try again"

	# Copy next to the destination, then rename, so a partly written file is
	# never on PATH. Rerunning replaces only the launcher; installed releases
	# in ~/.versio are kept.
	mkdir -p "$bin_dir"
	cp "$tmp/$name" "$bin_dir/.versio.tmp"
	chmod 755 "$bin_dir/.versio.tmp"
	mv -f "$bin_dir/.versio.tmp" "$bin_dir/versio"

	# First run installs the app. Its output is only shown if it fails.
	# stdin is redirected because under "curl | sh" it is the rest of this
	# script.
	if "$bin_dir/versio" </dev/null >/dev/null 2>"$tmp/first-run"; then
		result="versio $("$bin_dir/versio" --version) is installed."
	else
		cat "$tmp/first-run" >&2
		result="versio is installed, but could not finish setting up."
	fi

	add_to_path
	say "$result $next"
}

detect_platform() {
	case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "this system ($(uname -s)) is not supported; on Windows, run in PowerShell: irm $base_url/install.ps1 | iex" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "this processor ($(uname -m)) is not supported" ;;
	esac
	# A terminal running under Rosetta reports x86_64 on Apple Silicon;
	# install the native build instead.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
		[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
		arch=arm64
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{ print $1 }'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{ print $1 }'
	else
		fail "need sha256sum or shasum to verify the download"
	fi
}

# add_to_path appends bin_dir to PATH in the user's shell profile, unless it
# is already on PATH or VERSIO_NO_MODIFY_PATH=1. It sets next to what the
# user should do to start versio.
add_to_path() {
	next="Run: versio"
	case ":$PATH:" in
	*":$bin_dir:"*) return ;;
	esac
	if [ "${VERSIO_NO_MODIFY_PATH:-}" = 1 ]; then
		next="Add $bin_dir to your PATH, then run: versio"
		return
	fi
	line="export PATH=\"$bin_dir:\$PATH\""
	case "$(basename "${SHELL:-sh}")" in
	zsh) profile="${ZDOTDIR:-$HOME}/.zshrc" ;;
	bash) [ "$os" = darwin ] && profile="$HOME/.bash_profile" || profile="$HOME/.bashrc" ;;
	*) profile="$HOME/.profile" ;;
	esac
	if ! grep -qsF "$line" "$profile"; then
		printf '\n# Added by the versio installer\n%s\n' "$line" >>"$profile"
	fi
	next="Open a new terminal, then run: versio"
}

say() { printf '%s\n' "$1"; }
fail() {
	printf 'versio: install failed: %s\n' "$1" >&2
	exit 1
}

main "$@"
