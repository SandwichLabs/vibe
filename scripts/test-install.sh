#!/usr/bin/env bash
# shellcheck disable=SC2015,SC2016,SC2329 # "A && ok || bad" is the test idiom; stubs are written with single-quoted printf; cleanup runs from a trap
# Tests for install.sh (see docs/packaging.md, "Installing").
#
#   scripts/test-install.sh                       full suite, fake archives, Docker matrix
#   scripts/test-install.sh --native              full suite on this machine (no Docker)
#   scripts/test-install.sh --images "ubuntu:24.04 debian:12"
#   scripts/test-install.sh --assets DIR --tag v0.1.0 [--native | --images "..."]
#                                                 smoke test against real release assets
#                                                 (DIR holds the archives, checksums.txt, install.sh)
#
# Full suite: archives are built with the real scripts/package.sh from FAKE binaries (shell scripts that
# print the real --version format), so the layout is exactly what release.yml ships while the test stays
# fast and runs on any glibc. A local python http.server stands in for GitHub (VIBE_DOWNLOAD_BASE and
# VIBE_LATEST_URL). Each Docker image runs install.sh as an unprivileged user with its own /bin/sh
# (dash on Ubuntu and Debian, bash on Fedora). Needs: docker (unless --native), python3, curl, tar.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
IMAGES_DEFAULT="ubuntu:22.04 ubuntu:24.04 debian:12 fedora:latest"

# ------------------------------------------------------------------ inner (runs inside a container or natively)
inner() {
	local srv="${T_SRV:?}" installer="${T_INSTALLER:?}" mode="${T_MODE:?}" tag="${T_TAG:-}"
	local fails=0 passes=0 out rc home
	local os arch
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
	: "$arch"

	ok() { passes=$((passes + 1)); echo "  ok   $*"; }
	bad() { fails=$((fails + 1)); echo "  FAIL $*"; }
	check() { # description, command...
		local d="$1"
		shift
		if "$@" >/dev/null 2>&1; then ok "$d"; else bad "$d"; fi
	}
	fresh() { # new HOME with nothing in it; PATH has no ~/.local/bin
		home="$(mktemp -d)"
		export HOME="$home"
		unset XDG_DATA_HOME VIBE_INSTALL_DIR INSTALL_DIR VIBE_APP_DIR VIBE_VERSION
		if [ "$os" = darwin ]; then DATA="$HOME/Library/Application Support"; else DATA="$HOME/.local/share"; fi
		BIN="$HOME/.local/bin"
	}
	inst() { # args...; sets out and rc
		set +e
		out="$(VIBE_DOWNLOAD_BASE="${BASE:-$srv/download/$tag}" sh "$installer" "$@" 2>&1 </dev/null)"
		rc=$?
		set -e
	}
	has() { printf '%s\n' "$out" | grep -qE -- "$1"; }

	if [ "$mode" = smoke ]; then
		echo "== smoke: default install of $tag"
		fresh
		inst --version "$tag"
		[ "$rc" -eq 0 ] && ok "install exits 0" || { bad "install exits 0"; echo "$out"; }
		check "vibe --version prints the version" bash -c "'$BIN/vibe' --version | grep -F '${tag#v}'"
		[ "$os" = darwin ] && check "app installed and signed" codesign --verify --deep --strict "$HOME/Applications/Vibe Studio.app"
		check "manifest written" test -s "$DATA/vibe/install-manifest"
		echo "== smoke: re-run is an upgrade-in-place"
		inst --version "$tag"
		[ "$rc" -eq 0 ] && ok "re-run exits 0" || bad "re-run exits 0"
		echo "== smoke: uninstall"
		inst --uninstall
		check "vibe removed" test ! -e "$BIN/vibe"
		[ "$os" = darwin ] && check "app removed" test ! -e "$HOME/Applications/Vibe Studio.app"
		check "manifest removed" test ! -e "$DATA/vibe/install-manifest"
		echo "== smoke: --cli-only"
		fresh
		inst --version "$tag" --cli-only
		[ "$rc" -eq 0 ] && ok "cli-only exits 0" || bad "cli-only exits 0"
		check "no vibe-studio with --cli-only" test ! -e "$BIN/vibe-studio"
		echo "passed=$passes failed=$fails"
		[ "$fails" -eq 0 ]
		return
	fi

	echo "== 1. default install (CLI + Studio)"
	fresh
	tag=v0.1.0
	inst --version v0.1.0
	[ "$rc" -eq 0 ] && ok "exits 0" || { bad "exits 0"; echo "$out"; }
	check "vibe --version is 0.1.0" bash -c "'$BIN/vibe' --version | grep -F 'vibe version 0.1.0 (commit:'"
	check "vibe-studio installed and runs" bash -c "'$BIN/vibe-studio' --version | grep -F 0.1.0"
	if [ "$os" = linux ]; then
		check "desktop entry has absolute, quoted Exec" grep -Fx "Exec=\"$BIN/vibe-studio\" %U" "$DATA/applications/vibe-studio.desktop"
		check "MIME xml installed" test -f "$DATA/mime/packages/vibe-bundle.xml"
		check "icon installed" test -f "$DATA/icons/hicolor/48x48/apps/vibe-studio.png"
	fi
	check "manifest lists version" grep -Fx "version=0.1.0" "$DATA/vibe/install-manifest"
	check "manifest lists vibe with its hash" grep -E "^F [0-9a-f]{64} $BIN/vibe\$" "$DATA/vibe/install-manifest"
	has "not on your PATH" && ok "PATH hint printed" || bad "PATH hint printed"
	check "no leftover temp or .new files" bash -c "! find '$BIN' -name '*.new*' | grep -q ."

	echo "== 2. piped through stdin, --cli-only"
	fresh
	set +e
	out="$(cat "$installer" | VIBE_DOWNLOAD_BASE="$srv/download/v0.1.0" sh -s -- --cli-only --version v0.1.0 2>&1)"
	rc=$?
	set -e
	[ "$rc" -eq 0 ] && ok "exits 0" || { bad "exits 0"; echo "$out"; }
	check "only vibe is installed" bash -c "test -x '$BIN/vibe' && test ! -e '$BIN/vibe-studio' && test ! -e '$DATA/applications/vibe-studio.desktop'"
	check "manifest has exactly one file" bash -c "[ \"\$(grep -c '^F ' '$DATA/vibe/install-manifest')\" = 1 ]"

	echo "== 3. collision guard (a foreign 'vibe', e.g. Mistral Vibe)"
	fresh
	mkdir -p "$BIN"
	MARK="$HOME/foreign-ran"
	printf '#!/bin/sh\n# mistral-vibe\ntouch "%s"\necho "vibe version 9.9.9 (commit: x, built: y)"\n' "$MARK" >"$BIN/vibe"
	chmod 755 "$BIN/vibe"
	inst --version v0.1.0
	[ "$rc" -ne 0 ] && ok "refused" || bad "refused"
	has "Mistral Vibe" && ok "message names Mistral Vibe" || bad "message names Mistral Vibe"
	has "[-][-]force" && ok "message names --force" || bad "message names --force"
	check "foreign binary untouched" grep -F mistral-vibe "$BIN/vibe"
	check "foreign binary was never executed" test ! -e "$MARK"
	check "nothing else installed" test ! -e "$DATA/vibe/install-manifest"
	inst --version v0.1.0 --install-dir "$HOME/other/bin"
	[ "$rc" -eq 0 ] && ok "--install-dir elsewhere works" || bad "--install-dir elsewhere works"
	check "foreign binary still untouched" grep -F mistral-vibe "$BIN/vibe"
	inst --version v0.1.0 --force
	[ "$rc" -eq 0 ] && ok "--force replaces it" || bad "--force replaces it"
	check "now ours" bash -c "'$BIN/vibe' --version | grep -F 'vibe version 0.1.0'"
	# A second foreign 'vibe' earlier on PATH is a warning, not a refusal.
	mkdir -p "$HOME/shadow"
	printf '#!/bin/sh\necho mistral\n' >"$HOME/shadow/vibe"
	chmod 755 "$HOME/shadow/vibe"
	set +e
	out="$(PATH="$HOME/shadow:$BIN:$PATH" VIBE_DOWNLOAD_BASE="$srv/download/v0.1.0" sh "$installer" --version v0.1.0 2>&1 </dev/null)"
	rc=$?
	set -e
	[ "$rc" -eq 0 ] && has "another 'vibe' comes first" && ok "shadowing vibe on PATH warns" || bad "shadowing vibe on PATH warns"

	echo "== 4. checksum mismatch aborts"
	fresh
	BASE="$srv/download/bad" inst --version v0.1.0
	[ "$rc" -ne 0 ] && ok "refused" || bad "refused"
	has "checksum mismatch" && ok "says checksum mismatch" || bad "says checksum mismatch"
	check "nothing installed" bash -c "test ! -e '$BIN/vibe' && test ! -e '$DATA/vibe/install-manifest'"
	BASE=''

	echo "== 5. upgrade 0.1.0 -> 0.2.0 removes files that are gone"
	fresh
	inst --version v0.1.0
	[ "$os" = linux ] && check "0.1.0 icon present" test -f "$DATA/icons/hicolor/24x24/apps/vibe-studio-old.png"
	BASE="$srv/download/v0.2.0" inst --version v0.2.0
	[ "$rc" -eq 0 ] && ok "upgrade exits 0" || { bad "upgrade exits 0"; echo "$out"; }
	BASE=''
	has "upgrading:  0.1.0 -> 0.2.0" && ok "prints old -> new" || bad "prints old -> new"
	check "vibe is 0.2.0" bash -c "'$BIN/vibe' --version | grep -F 0.2.0"
	check "manifest version is 0.2.0" grep -Fx "version=0.2.0" "$DATA/vibe/install-manifest"
	[ "$os" = linux ] && check "icon dropped by 0.2.0 is removed" test ! -e "$DATA/icons/hicolor/24x24/apps/vibe-studio-old.png"

	echo "== 6. uninstall removes exactly the manifest, keeps user data"
	mkdir -p "$HOME/.config/vibe" "$HOME/ws"
	echo keep >"$HOME/.config/vibe/settings.json"
	echo keep >"$HOME/ws/vibe.yaml"
	files="$(sed -n 's/^F [0-9a-f]* //p' "$DATA/vibe/install-manifest")"
	[ -n "$files" ] && ok "manifest has entries" || bad "manifest has entries"
	inst --uninstall
	[ "$rc" -eq 0 ] && ok "exits 0" || { bad "exits 0"; echo "$out"; }
	left=0
	while IFS= read -r f; do [ -e "$f" ] && left=$((left + 1)); done <<<"$files"
	[ "$left" -eq 0 ] && ok "every manifest file is gone" || bad "$left manifest files remain"
	check "manifest removed" test ! -e "$DATA/vibe/install-manifest"
	check "config kept" test -f "$HOME/.config/vibe/settings.json"
	check "workspace kept" test -f "$HOME/ws/vibe.yaml"
	has "Kept on purpose" && ok "says what is kept" || bad "says what is kept"
	inst --uninstall
	[ "$rc" -ne 0 ] && has "no install manifest" && ok "uninstall with no manifest explains" || bad "uninstall with no manifest explains"

	echo "== 7. 'latest' resolves through the releases/latest redirect"
	fresh
	set +e
	out="$(VIBE_LATEST_URL="$srv/releases/latest" VIBE_DOWNLOAD_BASE="$srv/download/v0.2.0" sh "$installer" --cli-only 2>&1 </dev/null)"
	rc=$?
	set -e
	[ "$rc" -eq 0 ] && ok "exits 0" || { bad "exits 0"; echo "$out"; }
	check "got 0.2.0" bash -c "'$BIN/vibe' --version | grep -F 0.2.0"

	echo "== 8. --dry-run changes nothing"
	fresh
	inst --version v0.1.0 --dry-run
	[ "$rc" -eq 0 ] && ok "exits 0" || bad "exits 0"
	check "nothing written" bash -c "test ! -e '$BIN' && test ! -e '$DATA/vibe'"

	echo "== 9. unsupported platform is refused clearly"
	fresh
	stub="$(mktemp -d)"
	printf '#!/bin/sh\n[ "$1" = -m ] && echo riscv64 || /usr/bin/uname "$@"\n' >"$stub/uname"
	chmod 755 "$stub/uname"
	set +e
	out="$(PATH="$stub:$PATH" sh "$installer" --version v0.1.0 2>&1 </dev/null)"
	rc=$?
	set -e
	[ "$rc" -ne 0 ] && has "unsupported architecture 'riscv64'" && ok "riscv64 refused" || bad "riscv64 refused"

	if [ "$os" = linux ]; then
		echo "== 10. missing libraries produce a distro hint, not a failure"
		fresh
		stub="$(mktemp -d)"
		printf '#!/bin/sh\nif [ "$1" = --version ]; then echo "ldd (GNU libc) 2.35"; else echo "	libwebkit2gtk-4.1.so.0 => not found"; fi\n' >"$stub/ldd"
		chmod 755 "$stub/ldd"
		set +e
		out="$(PATH="$stub:$PATH" VIBE_DOWNLOAD_BASE="$srv/download/v0.1.0" sh "$installer" --version v0.1.0 2>&1 </dev/null)"
		rc=$?
		set -e
		[ "$rc" -eq 0 ] && ok "still exits 0" || bad "still exits 0"
		has "missing libraries" && has "libwebkit2gtk-4.1.so.0" && ok "lists the missing library" || bad "lists the missing library"
		has "sudo (apt|dnf|pacman|zypper)|needs WebKitGTK" && ok "prints an install hint" || bad "prints an install hint"
	fi

	echo "== 11. hostile or broken releases install nothing"
	for case_ in swap:"checksum mismatch" nosum:"no entry for" decoy:"no entry for" trav:"unsafe paths" link:"links or special files"; do
		fresh
		name=${case_%%:*}
		want=${case_#*:}
		BASE="$srv/download/$name" inst --version v0.1.0
		BASE=''
		[ "$rc" -ne 0 ] && has "$want" && ok "$name refused ($want)" || { bad "$name refused ($want)"; echo "$out"; }
		check "$name: nothing installed" bash -c "test ! -e '$BIN/vibe' && test ! -e '$DATA/vibe/install-manifest' && test ! -e '$HOME/evil'"
	done

	echo "== 12. a tampered manifest cannot make uninstall delete other things"
	fresh
	inst --version v0.1.0
	mkdir -p "$HOME/precious/sub"
	echo keep >"$HOME/precious/sub/f"
	echo keep >"$HOME/notvibe"
	{
		echo "F 0000 $HOME/precious"
		echo "F 0000 /"
		echo "F $(printf '%064d' 0) $HOME/notvibe"
		echo "F $(sha256sum "$HOME/notvibe" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$HOME/notvibe" | cut -d' ' -f1) $HOME/notvibe"
		echo "D - $HOME/precious"
		echo "F ../../etc/passwd"
	} >>"$DATA/vibe/install-manifest"
	inst --uninstall
	[ "$rc" -eq 0 ] && ok "uninstall still succeeds" || { bad "uninstall still succeeds"; echo "$out"; }
	check "directory entry not removed" test -f "$HOME/precious/sub/f"
	check "foreign file not removed" test -f "$HOME/notvibe"
	check "real files were removed" test ! -e "$BIN/vibe"

	echo "== 13. a foreign vibe that replaced ours is never overwritten or removed"
	fresh
	inst --version v0.1.0
	printf '#!/bin/sh\necho mistral\n' >"$BIN/vibe"
	BASE="$srv/download/v0.2.0" inst --version v0.2.0
	BASE=''
	[ "$rc" -ne 0 ] && ok "upgrade refused" || bad "upgrade refused"
	check "foreign vibe untouched" bash -c "'$BIN/vibe' | grep -F mistral"
	inst --uninstall
	check "uninstall keeps the foreign vibe" bash -c "'$BIN/vibe' | grep -F mistral"
	has "kept .*vibe .*changed" && ok "uninstall says it kept it" || bad "uninstall says it kept it"
	check "ours was removed" test ! -e "$BIN/vibe-studio"

	echo "== 14. unusual install dir (space, dollar sign) gives a valid quoted Exec"
	if [ "$os" = linux ]; then
		fresh
		inst --version v0.1.0 --install-dir "$HOME/my \$bin/x"
		check "Exec is quoted and escaped" grep -Fx "Exec=\"$HOME/my \\\\\$bin/x/vibe-studio\" %U" "$DATA/applications/vibe-studio.desktop"
	fi

	echo "passed=$passes failed=$fails"
	[ "$fails" -eq 0 ]
}

if [ "${1:-}" = --inner ]; then
	inner
	exit $?
fi

# ------------------------------------------------------------------ outer
IMAGES="$IMAGES_DEFAULT" NATIVE=0 ASSETS='' TAG=''
while [ $# -gt 0 ]; do
	case "$1" in
	--native) NATIVE=1 ;;
	--images) IMAGES="$2"; shift ;;
	--assets) ASSETS="$(cd "$2" && pwd)"; shift ;;
	--tag) TAG="$2"; shift ;;
	-h | --help) sed -n '2,/^set -/p' "$0" | sed 's/^# \{0,1\}//' | head -n -1; exit 0 ;;
	*) echo "unknown option $1" >&2; exit 2 ;;
	esac
	shift
done

WORK="$(mktemp -d)"
SERVER_PID=''
cleanup() {
	[ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT

case "$(uname -m)" in x86_64 | amd64) ARCH=amd64 ;; *) ARCH=arm64 ;; esac
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
SRVDIR="$WORK/srv"
mkdir -p "$SRVDIR/download"

mkchecksums() { # dir
	(cd "$1" && { sha256sum -- ./*.tar.gz 2>/dev/null || shasum -a 256 -- ./*.tar.gz; } | sed 's| \./| |' >checksums.txt)
}

fake_release() { # ver with_extra_icon(0|1)
	local ver="$1" extra="$2" root="$WORK/root-$1" dist="$SRVDIR/download/v$1"
	mkdir -p "$root/scripts" "$root/studio/build/bin" "$root/studio/build/linux" "$dist"
	cp "$REPO/scripts/package.sh" "$root/scripts/"
	cp -R "$REPO"/studio/build/linux/vibe-studio.desktop "$REPO"/studio/build/linux/vibe-mime.xml "$REPO"/studio/build/linux/icons "$root/studio/build/linux/"
	printf 'Apache-2.0 (test)\n' >"$root/LICENSE"
	printf 'Vibe (test)\n' >"$root/NOTICE"
	printf '# notices (test)\n' >"$root/THIRD_PARTY_NOTICES.md"
	printf '#!/bin/sh\n# github.com/SandwichLabs/vibe\necho "vibe version %s (commit: abc1234, built: 2026-01-01T00:00:00Z)"\n' "$ver" >"$root/vibe"
	printf '#!/bin/sh\n# github.com/SandwichLabs/vibe\necho "vibe-studio %s (commit: abc1234, built: 2026-01-01T00:00:00Z)"\n' "$ver" >"$root/studio/build/bin/vibe-studio"
	chmod 755 "$root/vibe" "$root/studio/build/bin/vibe-studio"
	(cd "$root" && bash scripts/package.sh "$OS" "$ARCH" "$ver" "$dist" >/dev/null)
	if [ "$extra" = 1 ]; then # add an icon that only this version ships
		local x="$WORK/x-$ver"
		mkdir -p "$x"
		tar -xzf "$dist/vibe-studio_${ver}_${OS}_${ARCH}.tar.gz" -C "$x"
		mkdir -p "$x/share/icons/hicolor/24x24/apps"
		printf 'png\n' >"$x/share/icons/hicolor/24x24/apps/vibe-studio-old.png"
		(cd "$x" && tar -czf "$dist/vibe-studio_${ver}_${OS}_${ARCH}.tar.gz" .)
	fi
	mkchecksums "$dist"
}

# Hostile or broken releases, each with checksums.txt that is internally consistent where the attack needs it.
craft_archives() {
	local d="$SRVDIR/download" a="vibe-studio_0.1.0_${OS}_${ARCH}.tar.gz" sum
	# swap: a valid archive of a different version under the 0.1.0 name, checksums of the real one
	mkdir -p "$d/swap" "$d/nosum" "$d/decoy" "$d/trav" "$d/link"
	cp "$d/v0.1.0/"* "$d/swap/"
	cp "$d/v0.2.0/vibe-studio_0.2.0_${OS}_${ARCH}.tar.gz" "$d/swap/$a"
	# nosum: checksums.txt lists other files only
	cp "$d/v0.1.0/$a" "$d/nosum/"
	echo "0000000000000000000000000000000000000000000000000000000000000000  other.tar.gz" >"$d/nosum/checksums.txt"
	# decoy: the right hash, but on a line whose name merely ends with the asset name
	cp "$d/v0.1.0/$a" "$d/decoy/"
	sum="$(sha256sum "$d/v0.1.0/$a" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$d/v0.1.0/$a" | cut -d' ' -f1)"
	echo "$sum  x$a" >"$d/decoy/checksums.txt"
	# trav and link: consistent checksums, hostile contents
	python3 - "$d" "$a" <<'PY'
import hashlib, io, sys, tarfile
d, a = sys.argv[1:3]
def build(kind):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as t:
        def f(name, data, mode=0o755):
            i = tarfile.TarInfo(name); i.size = len(data); i.mode = mode
            t.addfile(i, io.BytesIO(data))
        body = b'#!/bin/sh\n# github.com/SandwichLabs/vibe\necho "vibe version 9.9.9 (commit: a, built: b)"\n'
        if kind == "trav":
            f("../evil", b"pwned")
            f("bin/vibe", body); f("bin/vibe-studio", body)
        else:
            i = tarfile.TarInfo("bin/vibe"); i.type = tarfile.SYMTYPE; i.linkname = "/etc/passwd"; t.addfile(i)
            f("bin/vibe-studio", body)
    return buf.getvalue()
for kind in ("trav", "link"):
    data = build(kind)
    open("%s/%s/%s" % (d, kind, a), "wb").write(data)
    open("%s/%s/checksums.txt" % (d, kind), "w").write("%s  %s\n" % (hashlib.sha256(data).hexdigest(), a))
PY
}

if [ -n "$ASSETS" ]; then
	[ -n "$TAG" ] || { echo "--assets needs --tag" >&2; exit 2; }
	mkdir -p "$SRVDIR/download/$TAG"
	cp "$ASSETS"/* "$SRVDIR/download/$TAG/"
	cp "$ASSETS/install.sh" "$WORK/install.sh"
	MODE=smoke
else
	echo "building fake releases with scripts/package.sh ($OS/$ARCH)"
	fake_release 0.1.0 1
	fake_release 0.2.0 0
	# A tampered archive that still has the old, honest checksum.
	mkdir -p "$SRVDIR/download/bad"
	cp "$SRVDIR/download/v0.1.0/"* "$SRVDIR/download/bad/"
	printf 'tampered' >>"$SRVDIR/download/bad/vibe-studio_0.1.0_${OS}_${ARCH}.tar.gz"
	craft_archives
	echo v0.2.0 >"$SRVDIR/LATEST"
	cp "${VIBE_TEST_INSTALLER:-$REPO/install.sh}" "$WORK/install.sh"
	MODE=full
fi
cp "$HERE/test-install.sh" "$WORK/test-install.sh"

cat >"$WORK/server.py" <<'PY'
import http.server, os, sys
root = sys.argv[1]
class H(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **k):
        super().__init__(*a, directory=root, **k)
    def do_GET(self):
        if self.path == "/releases/latest":
            tag = open(os.path.join(root, "LATEST")).read().strip()
            self.send_response(302)
            self.send_header("Location", "http://%s/releases/tag/%s" % (self.headers["Host"], tag))
            self.end_headers()
            return
        super().do_GET()
    def log_message(self, *a):
        pass
s = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
tmp = os.path.join(root, "..", "port.tmp")
open(tmp, "w").write(str(s.server_address[1]))
os.replace(tmp, os.path.join(root, "..", "port"))
s.serve_forever()
PY
python3 "$WORK/server.py" "$SRVDIR" 2>"$WORK/server.log" &
SERVER_PID=$!
# A cold python3 on a fresh macOS runner can take several seconds to start, so allow 60 s and
# fail with a clear message if the server died instead.
for _ in $(seq 600); do
	[ -s "$WORK/port" ] && break
	kill -0 "$SERVER_PID" 2>/dev/null || { echo "test-install.sh: fake GitHub server exited before it listened:" >&2; cat "$WORK/server.log" >&2; exit 1; }
	sleep 0.1
done
[ -s "$WORK/port" ] || { echo "test-install.sh: fake GitHub server did not listen within 60 s:" >&2; cat "$WORK/server.log" >&2; exit 1; }
PORT="$(cat "$WORK/port")"
SRV="http://127.0.0.1:$PORT"
echo "serving fake GitHub on $SRV"

status=0
if [ "$NATIVE" -eq 1 ]; then
	echo "######## native ($OS/$ARCH)"
	( export T_SRV="$SRV" T_INSTALLER="$WORK/install.sh" T_MODE="$MODE" T_TAG="$TAG"; bash "$WORK/test-install.sh" --inner ) || status=1
else
	command -v docker >/dev/null || { echo "docker is required (or use --native)" >&2; exit 2; }
	chmod -R a+rX "$WORK"
	for img in $IMAGES; do
		slug="vibe-install-test:$(echo "$img" | tr ':/' '--')"
		if ! docker image inspect "$slug" >/dev/null 2>&1; then
			echo "building $slug"
			docker build -q -t "$slug" - >/dev/null <<EOF
FROM $img
RUN if command -v apt-get >/dev/null; then apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends curl ca-certificates tar gzip >/dev/null; \
    else (command -v curl >/dev/null || dnf install -y -q curl) && (command -v tar >/dev/null || dnf install -y -q tar gzip); fi
EOF
		fi
		echo "######## $img"
		docker run --rm --network host --security-opt label=disable -u 1000:1000 \
			-e T_SRV="$SRV" -e T_INSTALLER=/work/install.sh -e T_MODE="$MODE" -e T_TAG="$TAG" \
			-v "$WORK:/work:ro" "$slug" bash /work/test-install.sh --inner || status=1
	done
fi
[ "$status" -eq 0 ] && echo "ALL PASSED" || echo "FAILURES"
exit "$status"
