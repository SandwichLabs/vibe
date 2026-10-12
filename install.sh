#!/bin/sh
# shellcheck disable=SC2015 # "A && B || C" is deliberate in the two status prints
# Vibe installer for macOS and Linux (amd64, arm64). POSIX sh; never uses sudo; no telemetry.
#
#   curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh
#   curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh -s -- --cli-only
#   curl -fsSL https://sandwichlabs.dev/vibe/install.sh | VIBE_VERSION=v0.1.0 sh
#   curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh -s -- --uninstall
#
# Options:
#   --version vX.Y.Z    install this release (same as VIBE_VERSION); default: the latest release
#   --cli-only          install only the `vibe` CLI (no Studio desktop app)
#   --install-dir DIR   where the binaries go (default $HOME/.local/bin; env VIBE_INSTALL_DIR or INSTALL_DIR)
#   --force             replace a `vibe` that this installer did not put there; skip the OS version check
#   --uninstall         remove exactly the files listed in the install manifest
#   --dry-run           print what would happen; download and change nothing
#   -h, --help          show this help
#
# Environment: VIBE_VERSION, VIBE_INSTALL_DIR / INSTALL_DIR, VIBE_APP_DIR (macOS, default ~/Applications),
# VIBE_DOWNLOAD_BASE (mirror or test server; <base>/<asset> must resolve), VIBE_LATEST_URL (override of
# the releases/latest URL whose redirect names the newest tag), NO_COLOR.
#
# Upgrading is re-running this script. Every file written is recorded in
# ${XDG_DATA_HOME:-~/.local/share}/vibe/install-manifest (macOS: ~/Library/Application Support/vibe/install-manifest);
# --uninstall removes those files and nothing else. Config, credentials, caches and workspaces are never touched.
#
# The whole script is wrapped in main() so a truncated download executes nothing.

set -eu

REPO="SandwichLabs/vibe"
GH="https://github.com/$REPO"

# ---------------------------------------------------------------- output
if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
	C_RED=$(printf '\033[31m') C_YEL=$(printf '\033[33m') C_GRN=$(printf '\033[32m') C_OFF=$(printf '\033[0m')
else
	C_RED='' C_YEL='' C_GRN='' C_OFF=''
fi
say() { printf '%s\n' "$*"; }
info() { printf '%s\n' "$*" >&2; }
ok() { printf '%s%s%s\n' "$C_GRN" "$*" "$C_OFF" >&2; }
warn() { printf '%swarning:%s %s\n' "$C_YEL" "$C_OFF" "$*" >&2; }
die() {
	printf '%serror:%s %s\n' "$C_RED" "$C_OFF" "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Vibe installer (macOS and Linux, amd64 and arm64)

  curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh
  curl -fsSL https://sandwichlabs.dev/vibe/install.sh | sh -s -- [options]

Options:
  --version vX.Y.Z    install this release (or VIBE_VERSION); default: the latest release
  --cli-only          install only the `vibe` CLI (no Studio desktop app)
  --install-dir DIR   where the binaries go (default $HOME/.local/bin; or VIBE_INSTALL_DIR)
  --force             replace a `vibe` that this installer did not put there; skip OS version checks
  --uninstall         remove exactly the files recorded in the install manifest
  --dry-run           print what would happen; download and change nothing
  -h, --help          show this help

Environment: VIBE_VERSION, VIBE_INSTALL_DIR (or INSTALL_DIR), VIBE_APP_DIR (macOS, default ~/Applications),
VIBE_DOWNLOAD_BASE (mirror), VIBE_LATEST_URL (test override), NO_COLOR.
EOF
}

have() { command -v "$1" >/dev/null 2>&1; }

# ---------------------------------------------------------------- state
VERSION="${VIBE_VERSION:-latest}"
INSTALL_DIR="${VIBE_INSTALL_DIR:-${INSTALL_DIR:-$HOME/.local/bin}}"
APP_DIR="${VIBE_APP_DIR:-$HOME/Applications}"
CLI_ONLY=0 FORCE=0 UNINSTALL=0 DRY=0
OS='' ARCH='' TMP=''
MANIFEST='' DATA_HOME=''

cleanup() {
	[ -n "$TMP" ] && rm -rf "$TMP"
	return 0
}

# ---------------------------------------------------------------- helpers
download() { # url dest
	if have curl; then
		case "$1" in
		https://*) curl -fsSL --proto '=https' --tlsv1.2 --retry 2 -o "$2" "$1" ;;
		*) curl -fsSL --retry 2 -o "$2" "$1" ;;
		esac
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is required"
	fi
}

# Print the Location header of a redirect without following it.
redirect_of() { # url
	if have curl; then
		case "$1" in
		https://*) curl -fsS --proto '=https' --tlsv1.2 -o /dev/null -w '%{redirect_url}' "$1" ;;
		*) curl -fsS -o /dev/null -w '%{redirect_url}' "$1" ;;
		esac
	else
		die "curl is required to look up the latest release (or pass --version vX.Y.Z)"
	fi
}

sha256_of() { # file
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "sha256sum or shasum is required to verify the download"
	fi
}

# Manifest lines: "version=X", "F <sha256> <path>" (a file) and "D - <path>" (the macOS app bundle).
manifest_hash_of() { # path -> recorded sha256, or nothing
	[ -f "$MANIFEST" ] || return 0
	_P=$1 awk '$1 == "F" { h = $2; sub(/^F [^ ]+ /, ""); if ($0 == ENVIRON["_P"]) { print h; exit } }' "$MANIFEST"
}

in_manifest() { # path
	[ -f "$MANIFEST" ] && grep -Fxq -- "D - $1" "$MANIFEST"
}

# Is this file one of ours? Yes when it still has the hash we recorded, or (first install over an
# older copy) when it carries Vibe's Go module path. Foreign programs are never executed to find out.
is_ours() { # path
	[ -f "$1" ] || return 1
	_h=$(manifest_hash_of "$1")
	if [ -n "$_h" ] && [ "$_h" = "$(sha256_of "$1")" ]; then return 0; fi
	[ -z "$_h" ] && grep -aqF 'github.com/SandwichLabs/vibe' "$1" 2>/dev/null
}

manifest_version() {
	[ -f "$MANIFEST" ] && sed -n 's/^version=//p' "$MANIFEST" | head -n 1
	return 0
}

# ---------------------------------------------------------------- detection
detect_platform() {
	case "$(uname -s)" in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	*) die "unsupported OS '$(uname -s)'. Vibe ships for macOS and Linux; build from source: $GH" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "unsupported architecture '$(uname -m)'. Vibe ships for amd64 and arm64; build from source: $GH" ;;
	esac
	if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] && [ "$(sysctl -in sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
		ARCH=arm64 # a shell running under Rosetta on Apple silicon
	fi

	if [ "$OS" = linux ]; then
		if (ldd --version 2>&1 || true) | head -n 1 | grep -qi musl || ls /lib/ld-musl-* >/dev/null 2>&1; then
			die "this looks like a musl system (Alpine). Vibe's Linux binaries need glibc 2.35 or newer; build from source: $GH"
		fi
		_glibc=$(getconf GNU_LIBC_VERSION 2>/dev/null | awk '{print $2}') || _glibc=''
		if [ -n "$_glibc" ]; then
			_maj=${_glibc%%.*}
			_min=${_glibc#*.}
			_min=${_min%%.*}
			case "$_maj$_min" in
			*[!0-9]*) ;;
			*)
				if [ "$_maj" -lt 2 ] || { [ "$_maj" -eq 2 ] && [ "$_min" -lt 35 ]; }; then
					[ "$FORCE" -eq 1 ] || die "glibc $_glibc is too old: Vibe needs 2.35 or newer (Ubuntu 22.04, Debian 12, Fedora 36 or later). Use --force to try anyway."
				fi
				;;
			esac
		fi
	else
		_mac=$(sw_vers -productVersion 2>/dev/null || echo 0)
		_macmaj=${_mac%%.*}
		_need=13
		[ "$ARCH" = arm64 ] && _need=14
		case "$_macmaj" in
		*[!0-9]* | '') ;;
		*)
			if [ "$_macmaj" -lt "$_need" ]; then
				[ "$FORCE" -eq 1 ] || die "macOS $_mac is too old: Vibe needs macOS $_need or later on $ARCH. Use --force to try anyway."
			fi
			;;
		esac
	fi

	if [ "$OS" = darwin ]; then
		DATA_HOME="$HOME/Library/Application Support"
	else
		DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
	fi
	MANIFEST="$DATA_HOME/vibe/install-manifest"
}

resolve_version() {
	case "$VERSION" in
	latest | '')
		_url="${VIBE_LATEST_URL:-$GH/releases/latest}"
		_loc=$(redirect_of "$_url") || _loc=''
		_tag=${_loc##*/}
		[ -n "$_tag" ] && [ "$_tag" != latest ] || die "could not find the latest release at $_url (is there a published release yet? try --version vX.Y.Z)"
		VERSION=$_tag
		;;
	esac
	case "$VERSION" in
	v*) TAG=$VERSION VER=${VERSION#v} ;;
	*) TAG="v$VERSION" VER=$VERSION ;;
	esac
	case "$VER" in
	*[!0-9A-Za-z.+-]* | '') die "invalid version '$VERSION'" ;;
	esac
}

# ---------------------------------------------------------------- collision guard
guard_collision() {
	[ "$FORCE" -eq 1 ] && return 0
	for _name in vibe vibe-studio; do
		[ "$_name" = vibe-studio ] && { [ "$CLI_ONLY" -eq 1 ] || [ "$OS" = darwin ]; } && continue
		_p="$INSTALL_DIR/$_name"
		{ [ -e "$_p" ] || [ -L "$_p" ]; } || continue
		is_ours "$_p" && continue
		die "$_p already exists and is not Vibe (this installer did not write it, or it changed since).
Another program named '$_name' is installed there, quite possibly Mistral Vibe (the 'vibe' coding agent).
Nothing was changed. Either:
  - install Vibe somewhere else:  --install-dir DIR   (or VIBE_INSTALL_DIR=DIR), then put DIR first on your PATH
  - or replace that program:      --force"
	done
}

# ---------------------------------------------------------------- manifest + file placement
NEW_MANIFEST=''
record() { # F|D path
	if [ "$1" = F ]; then printf 'F %s %s\n' "$(sha256_of "$2")" "$2" >>"$NEW_MANIFEST"; else printf 'D - %s\n' "$2" >>"$NEW_MANIFEST"; fi
}

# Remove one manifest entry, but only if it is the kind of thing this installer writes. Never fatal.
remove_entry() { # manifest line
	_k=${1%% *}
	_r=${1#* }
	_h=${_r%% *}
	_path=${_r#* }
	case "$_path" in /*) ;; *) return 0 ;; esac
	case "$_path/" in */../* | *//*) return 0 ;; esac
	_base=${_path##*/}
	case "$_k" in
	D)
		[ "$_base" = "Vibe Studio.app" ] && [ -d "$_path" ] && [ ! -L "$_path" ] || return 0
		if rm -rf -- "$_path"; then info "removed $_path"; else warn "could not remove $_path"; fi
		;;
	F)
		case "$_path" in
		*/vibe | */vibe-studio) ;;
		*/applications/vibe-studio.desktop | */mime/packages/vibe-bundle.xml) ;;
		*/icons/hicolor/*/apps/vibe-studio*.png | */icons/hicolor/*/apps/vibe-studio*.svg) ;;
		*) return 0 ;;
		esac
		[ -e "$_path" ] || [ -L "$_path" ] || return 0
		if [ -L "$_path" ] || [ ! -f "$_path" ] || [ "$_h" != "$(sha256_of "$_path")" ]; then
			info "kept $_path (it changed since it was installed)"
			return 0
		fi
		if rm -f -- "$_path"; then info "removed $_path"; else warn "could not remove $_path"; fi
		;;
	esac
	return 0
}

put_file() { # src dst mode
	mkdir -p "$(dirname "$2")"
	_tmp="$2.new.$$"
	cp "$1" "$_tmp"
	chmod "$3" "$_tmp"
	mv -f "$_tmp" "$2"
	record F "$2"
}

# ---------------------------------------------------------------- Linux
refresh_linux_dbs() {
	[ "$CLI_ONLY" -eq 1 ] && return 0
	have update-mime-database && [ -d "$DATA_HOME/mime" ] && update-mime-database "$DATA_HOME/mime" >/dev/null 2>&1 || true
	have update-desktop-database && [ -d "$DATA_HOME/applications" ] && update-desktop-database "$DATA_HOME/applications" >/dev/null 2>&1 || true
	have gtk-update-icon-cache && [ -d "$DATA_HOME/icons/hicolor" ] && gtk-update-icon-cache -q -f -t "$DATA_HOME/icons/hicolor" >/dev/null 2>&1 || true
	return 0
}

install_linux() { # stage dir
	_s=$1
	if [ "$CLI_ONLY" -eq 1 ]; then
		put_file "$_s/vibe" "$INSTALL_DIR/vibe" 755
		return 0
	fi
	put_file "$_s/bin/vibe" "$INSTALL_DIR/vibe" 755
	put_file "$_s/bin/vibe-studio" "$INSTALL_DIR/vibe-studio" 755

	# The desktop entry gets an absolute, quoted Exec (desktop sessions often lack ~/.local/bin on PATH).
	# Desktop Entry spec: inside quotes escape " ` $ \ with a backslash, and the string layer doubles
	# the backslash; a literal % is %%.
	if [ -f "$_s/share/applications/vibe-studio.desktop" ]; then
		_q=$(printf '%s' "$INSTALL_DIR/vibe-studio" | sed -e 's/\\/\\\\\\\\/g' -e 's/["`$]/\\\\&/g' -e 's/%/%%/g')
		_EXECLINE="Exec=\"$_q\" %U" awk '/^Exec=vibe-studio/ { print ENVIRON["_EXECLINE"]; next } { print }' \
			"$_s/share/applications/vibe-studio.desktop" >"$TMP/vibe-studio.desktop"
		put_file "$TMP/vibe-studio.desktop" "$DATA_HOME/applications/vibe-studio.desktop" 644
	fi
	if [ -f "$_s/share/mime/packages/vibe-bundle.xml" ]; then
		put_file "$_s/share/mime/packages/vibe-bundle.xml" "$DATA_HOME/mime/packages/vibe-bundle.xml" 644
	fi
	# Icons are optional: older archives ship none.
	if [ -d "$_s/share/icons" ]; then
		(cd "$_s/share/icons" && find . -type f | sed 's|^\./||' | LC_ALL=C sort) >"$TMP/icons.list"
		while IFS= read -r _i; do
			put_file "$_s/share/icons/$_i" "$DATA_HOME/icons/$_i" 644
		done <"$TMP/icons.list"
	fi
}

check_linux_runtime() {
	[ "$CLI_ONLY" -eq 1 ] && return 0
	_missing=''
	if have ldd; then
		_missing=$(ldd "$INSTALL_DIR/vibe-studio" 2>/dev/null | grep 'not found' || true)
	fi
	if [ -n "$_missing" ]; then
		warn "Vibe Studio is installed but cannot start yet; missing libraries:"
		printf '%s\n' "$_missing" | sed 's/^[[:space:]]*/    /' >&2
		_id='' _like=''
		if [ -r /etc/os-release ]; then
			_id=$(sed -n 's/^ID=//p' /etc/os-release | tr -d '"' | head -n 1)
			_like=$(sed -n 's/^ID_LIKE=//p' /etc/os-release | tr -d '"' | head -n 1)
		fi
		case " $_id $_like " in
		*" debian "* | *" ubuntu "*) info "  Install them with:  sudo apt install libwebkit2gtk-4.1-0 libgtk-3-0" ;;
		*" fedora "* | *" rhel "* | *" centos "*) info "  Install them with:  sudo dnf install webkit2gtk4.1 gtk3" ;;
		*" arch "*) info "  Install them with:  sudo pacman -S webkit2gtk-4.1 gtk3" ;;
		*" suse "* | *" opensuse "*) info "  Install them with:  sudo zypper install libwebkit2gtk-4_1-0 gtk3" ;;
		*) info "  Vibe Studio needs WebKitGTK 4.1 (webkit2gtk-4.1) and GTK 3." ;;
		esac
		info "  The 'vibe' CLI works without them."
	fi
	# Secret Service holds credentials for connections.
	if have busctl; then
		if ! busctl --user status org.freedesktop.secrets >/dev/null 2>&1; then
			warn "no Secret Service (GNOME Keyring or KWallet) is reachable on your session bus."
			info "  Saved connection credentials need one; otherwise use env: references. Everything else works."
		fi
	fi
}

# ---------------------------------------------------------------- macOS
install_darwin() { # stage dir
	_s=$1
	put_file "$_s/vibe" "$INSTALL_DIR/vibe" 755
	[ "$CLI_ONLY" -eq 1 ] && return 0
	[ -d "$_s/Vibe Studio.app" ] || die "the archive has no 'Vibe Studio.app'"
	case "$APP_DIR" in *\'*) die "VIBE_APP_DIR must not contain a single quote" ;; esac
	mkdir -p "$APP_DIR"
	_app="$APP_DIR/Vibe Studio.app"
	if [ -e "$_app" ] && [ "$FORCE" -ne 1 ] && ! in_manifest "$_app"; then
		grep -q 'dev.sandwichlabs.vibe-studio' "$_app/Contents/Info.plist" 2>/dev/null ||
			die "$_app exists and is not Vibe Studio. Nothing was changed. Use --force to replace it, or set VIBE_APP_DIR."
	fi
	# ditto preserves the signature; the bundle is never modified after this copy.
	rm -rf "$_app.new"
	ditto "$_s/Vibe Studio.app" "$_app.new"
	if have codesign; then
		codesign --verify --deep --strict "$_app.new" || {
			rm -rf "$_app.new"
			die "the code signature of Vibe Studio.app does not verify; not installing it"
		}
	fi
	rm -rf "$_app"
	mv "$_app.new" "$_app"
	record D "$_app"
	# No vibe-studio wrapper: `vibe studio --gui` finds the app itself (open -a) in ~/Applications or /Applications.
	case "$APP_DIR" in
	"$HOME/Applications" | /Applications) ;;
	*) warn "$APP_DIR is not searched by 'vibe studio --gui'; open the app from there directly." ;;
	esac
}

# ---------------------------------------------------------------- uninstall
do_uninstall() {
	[ -f "$MANIFEST" ] || die "no install manifest at $MANIFEST; nothing to uninstall (this installer did not put Vibe on this machine)"
	[ "$DRY" -eq 1 ] && info "dry run: would remove"
	while IFS= read -r _line; do
		case "$_line" in "F "* | "D "*) ;; *) continue ;; esac
		if [ "$DRY" -eq 1 ]; then info "  ${_line#* }"; else remove_entry "$_line"; fi
	done <"$MANIFEST"
	if [ "$DRY" -eq 0 ]; then
		rm -f -- "$MANIFEST"
		rmdir "$(dirname "$MANIFEST")" 2>/dev/null || true
		CLI_ONLY=0
		refresh_linux_dbs
	fi
	ok "Vibe uninstalled."
	info "Kept on purpose (delete them yourself if you want them gone):"
	if [ "$OS" = darwin ]; then
		info "  settings:     ~/Library/Application Support/vibe (and ~/Library/Preferences)"
		info "  extensions:   ~/Library/Caches/vibe"
	else
		info "  settings:     ~/.config/vibe"
		info "  extensions:   ~/.cache/vibe (DuckDB extension cache)"
	fi
	info "  credentials:  your OS keychain / Secret Service (see: vibe conn credentials clear, run before uninstalling)"
	info "  workspaces:   your own folders, including any .vibe-local/ inside them"
}

# ---------------------------------------------------------------- main
main() {
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || die "--version needs a value"
			VERSION=$2
			shift
			;;
		--version=*) VERSION=${1#--version=} ;;
		--cli-only) CLI_ONLY=1 ;;
		--install-dir)
			[ $# -ge 2 ] || die "--install-dir needs a value"
			INSTALL_DIR=$2
			shift
			;;
		--install-dir=*) INSTALL_DIR=${1#--install-dir=} ;;
		--force) FORCE=1 ;;
		--uninstall) UNINSTALL=1 ;;
		--dry-run) DRY=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "unknown option '$1' (see --help)" ;;
		esac
		shift
	done

	[ -n "${HOME:-}" ] || die "HOME is not set"
	have tar || die "tar is required"
	case "$INSTALL_DIR" in /*) ;; *) die "the install directory must be an absolute path (got '$INSTALL_DIR')" ;; esac

	detect_platform

	if [ "$UNINSTALL" -eq 1 ]; then
		do_uninstall
		return 0
	fi

	guard_collision
	resolve_version
	_suffix="${VER}_${OS}_${ARCH}.tar.gz"
	if [ "$CLI_ONLY" -eq 1 ]; then ASSET="vibe_$_suffix"; else ASSET="vibe-studio_$_suffix"; fi
	BASE="${VIBE_DOWNLOAD_BASE:-$GH/releases/download/$TAG}"
	BASE=${BASE%/}

	_old=$(manifest_version)
	info "Vibe $VER for $OS/$ARCH ($([ "$CLI_ONLY" -eq 1 ] && echo 'CLI only' || echo 'CLI + Studio'))"
	info "  from:       $BASE/$ASSET"
	info "  binaries:   $INSTALL_DIR"
	[ "$CLI_ONLY" -eq 1 ] || { [ "$OS" = darwin ] && info "  app:        $APP_DIR/Vibe Studio.app" || info "  desktop:    $DATA_HOME (applications, mime, icons)"; }
	[ -z "$_old" ] || info "  upgrading:  $_old -> $VER"
	if [ "$DRY" -eq 1 ]; then
		info "dry run: nothing downloaded or changed."
		return 0
	fi

	TMP=$(mktemp -d "${TMPDIR:-/tmp}/vibe-install.XXXXXX")
	trap cleanup EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	trap 'exit 129' HUP

	info "Downloading..."
	download "$BASE/$ASSET" "$TMP/$ASSET" || die "could not download $BASE/$ASSET (does that release and platform exist?)"
	download "$BASE/checksums.txt" "$TMP/checksums.txt" || die "could not download $BASE/checksums.txt"

	_want=$(awk -v f="$ASSET" '{n=$2; sub(/^\*/, "", n)} n == f {print $1; exit}' "$TMP/checksums.txt")
	[ -n "$_want" ] || die "checksums.txt has no entry for $ASSET"
	_got=$(sha256_of "$TMP/$ASSET")
	[ "$_want" = "$_got" ] || die "checksum mismatch for $ASSET (expected $_want, got $_got). Aborting; nothing was installed."
	info "Checksum OK."

	# Refuse archives that would write outside the staging directory, or that hold anything but plain
	# files and directories (a symlink entry could point an installed binary at any file).
	if tar -tzf "$TMP/$ASSET" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
		die "the archive contains unsafe paths; aborting"
	fi
	if tar -tvzf "$TMP/$ASSET" | grep -v '^[-d]' | grep -q .; then
		die "the archive contains links or special files; aborting"
	fi
	mkdir -p "$TMP/stage"
	tar -xzf "$TMP/$ASSET" -C "$TMP/stage"

	NEW_MANIFEST="$TMP/manifest.new"
	: >"$NEW_MANIFEST"
	mkdir -p "$INSTALL_DIR"
	if [ "$OS" = linux ]; then install_linux "$TMP/stage"; else install_darwin "$TMP/stage"; fi

	_prev=''
	if [ -f "$MANIFEST" ]; then _prev="$TMP/manifest.old"; cp "$MANIFEST" "$_prev"; fi
	mkdir -p "$(dirname "$MANIFEST")"
	{
		printf '# vibe install manifest; written by install.sh\n'
		printf 'version=%s\n' "$VER"
		cat "$NEW_MANIFEST"
	} >"$MANIFEST.new.$$"
	mv -f "$MANIFEST.new.$$" "$MANIFEST"

	# Remove files a previous version installed that this one does not.
	if [ -n "$_prev" ]; then
		while IFS= read -r _line; do
			case "$_line" in "F "* | "D "*) ;; *) continue ;; esac
			_pp=${_line#* }
			_pp=${_pp#* }
			_P=$_pp awk '{ sub(/^[FD] [^ ]+ /, ""); if ($0 == ENVIRON["_P"]) f = 1 } END { exit !f }' "$NEW_MANIFEST" && continue
			remove_entry "$_line"
		done <"$_prev"
	fi

	[ "$OS" = linux ] && refresh_linux_dbs

	ok "Installed Vibe $VER."
	"$INSTALL_DIR/vibe" --version </dev/null 2>&1 | sed 's/^/  /' >&2 || true
	[ "$CLI_ONLY" -eq 1 ] || "$INSTALL_DIR/vibe-studio" --version </dev/null 2>&1 | sed 's/^/  /' >&2 || true

	[ "$OS" = linux ] && check_linux_runtime

	# PATH
	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		warn "$INSTALL_DIR is not on your PATH. Add it:"
		case "$(basename "${SHELL:-sh}")" in
		fish) info "  fish_add_path '$INSTALL_DIR'" ;;
		zsh) info "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && exec zsh" ;;
		bash) info "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc && exec bash" ;;
		*) info "  export PATH=\"$INSTALL_DIR:\$PATH\"   (add this to your shell's startup file)" ;;
		esac
		;;
	esac
	_other=$(command -v vibe 2>/dev/null || true)
	if [ -n "$_other" ] && [ "$_other" != "$INSTALL_DIR/vibe" ]; then
		warn "another 'vibe' comes first on your PATH: $_other"
		info "  Running 'vibe' will start that program, not Vibe. Put $INSTALL_DIR earlier on PATH, or use $INSTALL_DIR/vibe."
	fi

	if have pgrep && pgrep -x vibe-studio >/dev/null 2>&1; then
		info "Vibe Studio is running; restart it to use the new version."
	fi

	say ""
	say "Next:"
	say "  vibe workspace init demo      # make a workspace folder"
	say "  vibe studio --gui demo        # open it in Studio"
	say "  Docs: $GH"
	say "Uninstall: re-run this script with --uninstall"
}

{ main "$@"; }
