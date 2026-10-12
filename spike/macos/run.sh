#!/usr/bin/env bash
# Spike #43 (branch spike/43-macos only). Runs on a macOS runner after the build steps.
# Greppable lines start with "RESULT <key>:". Never exits non-zero for a probe failure.
set -uo pipefail
ARCH="${ARCH:?}"
ROOT="$(pwd)"
W="$RUNNER_TEMP/spike"; rm -rf "$W"; mkdir -p "$W"
R() { echo "RESULT $ARCH $1: $2"; }
G() { echo "::group::$*"; }
E() { echo "::endgroup::"; }
VER=0.0.0-spike
TARBALL="vibe-studio_${VER}_darwin_${ARCH}.tar.gz"

G "0 environment"
sw_vers; uname -m; clang --version | head -1; go version
R runner_macos "$(sw_vers -productVersion) ($(uname -m))"
E

G "1 build outputs before packaging"
APP0="$ROOT/studio/build/bin/vibe-studio.app"
codesign -dvvv "$APP0" 2>&1 | grep -E 'Identifier|flags|Signature|Authority|TeamIdentifier|CDHash|Sealed'
codesign -dvvv "$ROOT/vibe" 2>&1 | grep -E 'Identifier|flags|Signature|CDHash'
R cli_signature_from_go_linker "$(codesign -dvvv "$ROOT/vibe" 2>&1 | grep -E '^CodeDirectory' | head -1)"
E

G "2 package.sh (ad-hoc codesign --force --deep -s -, no hardened runtime)"
scripts/package.sh darwin "$ARCH" "$VER" dist
rc=$?; R package_sh_rc "$rc"
E

G "3 codesign -dvvv / spctl on the packaged app"
mkdir -p "$W/pkg"; tar -xzf "dist/$TARBALL" -C "$W/pkg"
APP="$W/pkg/Vibe Studio.app"
codesign -dvvv "$APP" 2>&1
fl="$(codesign -dvvv "$APP" 2>&1 | grep -E '^CodeDirectory' )"
R app_codedirectory "$fl"
case "$fl" in *runtime*) R hardened_runtime "PRESENT (unexpected)";; *) R hardened_runtime "absent (expected)";; esac
codesign --verify --deep --strict --verbose=2 "$APP" 2>&1; R codesign_verify_rc "$?"
sp="$(spctl -a -vv "$APP" 2>&1)"; sprc=$?
echo "$sp"; R spctl_app "rc=$sprc :: $(echo "$sp" | tr '\n' ' ')"
spctl -a -vv -t exec "$W/pkg/vibe" 2>&1 | tee "$W/spctl-cli.txt"; R spctl_cli "$(tr '\n' ' ' <"$W/spctl-cli.txt")"
E

G "4 curl-style download: no quarantine xattr"
mkdir -p "$W/srv" "$W/dl" "$W/ex"
cp "dist/$TARBALL" "$W/srv/"
printf 'a,b\n1,2\n3,4\n5,6\n' >"$W/srv/test.csv"
python3 -m http.server 8765 --bind 127.0.0.1 --directory "$W/srv" >"$W/http.log" 2>&1 &
HTTP_PID=$!
sleep 2
curl -fsS -o "$W/dl/$TARBALL" "http://127.0.0.1:8765/$TARBALL"; R curl_rc "$?"
x="$(xattr -l "$W/dl/$TARBALL" 2>&1)"; echo "xattr tarball: [$x]"
R xattr_tarball_after_curl "[${x}]"
tar -xzf "$W/dl/$TARBALL" -C "$W/ex"
xr="$(xattr -lr "$W/ex" 2>&1 | grep -c quarantine)"
R quarantine_attrs_in_extracted_tree "$xr"
xattr -lr "$W/ex" 2>&1 | head -20
exApp="$W/ex/Vibe Studio.app"
codesign --verify --deep --strict "$exApp"; R verify_after_curl_extract_rc "$?"
sp="$(spctl -a -vv "$exApp" 2>&1)"; R spctl_extracted_app "rc=$? :: $(echo "$sp" | tr '\n' ' ')"
"$W/ex/vibe" --version; R extracted_cli_version_rc "$?"
"$exApp/Contents/MacOS/vibe-studio" --version; R extracted_studio_version_rc "$?"
mkdir -p "$W/ws" && (cd "$W/ws" && "$W/ex/vibe" workspace init . --name smoke </dev/null && "$W/ex/vibe" query "select 42 as answer" </dev/null); R extracted_cli_query_rc "$?"
E

G "5 GUI launch from the curl-installed (non-quarantined) app + OnFileOpen"
echo '{}' >/dev/null
printf 'x' >"$W/cold.vibe"; printf 'x' >"$W/warm.vibe"; printf 'x' >"$W/handler.vibe"
mkdir -p "$W/ws2" "$W/ws3"
OUT="$W/studio.out"; ERR="$W/studio.err"; : >"$OUT"; : >"$ERR"
open -a "$exApp" --stdout "$OUT" --stderr "$ERR" "$W/ws2"; R open_cold_folder_rc "$?"
sleep 20
pgrep -fl "Vibe Studio.app/Contents/MacOS/vibe-studio"; R studio_running_after_open "$(pgrep -f 'Vibe Studio.app/Contents/MacOS/vibe-studio' | wc -l | tr -d ' ') process(es)"
screencapture -x "$W/shot-curl.png" 2>&1; ls -l "$W/shot-curl.png"
echo "--- studio stderr"; cat "$ERR"; echo "--- studio stdout"; cat "$OUT"
R onfileopen_cold_folder "$(grep -c "SPIKE-OnFileOpen: $W/ws2" "$ERR") hit(s)"
open -a "$exApp" "$W/ws3"; sleep 6
R onfileopen_warm_folder "$(grep -c "SPIKE-OnFileOpen: $W/ws3" "$ERR") hit(s)"
open -a "$exApp" "$W/warm.vibe"; sleep 6
R onfileopen_warm_vibe_explicit_app "$(grep -c "SPIKE-OnFileOpen: $W/warm.vibe" "$ERR") hit(s)"
LSR=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
"$LSR" -f "$exApp"; R lsregister_rc "$?"
"$LSR" -dump 2>/dev/null | grep -B2 -A12 'dev.sandwichlabs.vibe-bundle' | head -40
open "$W/handler.vibe"; R open_vibe_default_handler_rc "$?"; sleep 8
R onfileopen_vibe_default_handler "$(grep -c "SPIKE-OnFileOpen: $W/handler.vibe" "$ERR") hit(s)"
echo "--- studio stderr (final)"; cat "$ERR"
pkill -f 'Vibe Studio.app/Contents/MacOS/vibe-studio'; sleep 2
E

G "6 quarantine contrast"
mkdir -p "$W/q"; tar -xzf "$W/dl/$TARBALL" -C "$W/q"
QV="0083;$(printf '%x' "$(date +%s)");Safari;$(uuidgen)"
xattr -w com.apple.quarantine "$QV" "$W/q/vibe"
xattr -wr com.apple.quarantine "$QV" "$W/q/Vibe Studio.app"
xattr -l "$W/q/vibe"
R quarantine_set_on_cli "$(xattr -l "$W/q/vibe" | tr '\n' ' ')"
R spctl_quarantined_app "$(spctl -a -vv "$W/q/Vibe Studio.app" 2>&1 | tr '\n' ' ')"
timeout 30 "$W/q/vibe" --version; R quarantined_cli_from_shell_rc "$?"
timeout 30 "$W/q/Vibe Studio.app/Contents/MacOS/vibe-studio" --version; R quarantined_studio_binary_from_shell_rc "$?"
QERR="$W/q.err"; : >"$QERR"
timeout 40 open -a "$W/q/Vibe Studio.app" --stderr "$QERR" "$W/ws2"; R open_quarantined_rc "$?"
sleep 15
R quarantined_app_running "$(pgrep -f 'q/Vibe Studio.app/Contents/MacOS/vibe-studio' | wc -l | tr -d ' ') process(es)"
screencapture -x "$W/shot-quarantined.png" 2>&1
log show --last 2m --predicate 'process == "syspolicyd" OR process == "CoreServicesUIAgent" OR eventMessage CONTAINS "Gatekeeper"' --style compact 2>/dev/null | tail -25
pkill -f 'q/Vibe Studio.app/Contents/MacOS/vibe-studio'
E

G "7 extension library validation (probe signed three ways)"
cp "$ROOT/probe" "$W/probe-adhoc";  codesign --force -s - "$W/probe-adhoc"
cp "$ROOT/probe" "$W/probe-hr";     codesign --force -s - --options runtime "$W/probe-hr"
cat >"$W/dlv.plist" <<'PL'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>com.apple.security.cs.disable-library-validation</key><true/></dict></plist>
PL
cp "$ROOT/probe" "$W/probe-hr-dlv"; codesign --force -s - --options runtime --entitlements "$W/dlv.plist" "$W/probe-hr-dlv"
for p in adhoc hr hr-dlv; do codesign -dvvv "$W/probe-$p" 2>&1 | grep -E '^CodeDirectory'; done
EXTDIR="$W/extdir"
echo "--- (a) ad-hoc, no hardened runtime (first run downloads the extensions)"
"$W/probe-adhoc" ext "$EXTDIR" "http://127.0.0.1:8765/test.csv" 2>&1 | tee "$W/ext-a.txt"; R ext_adhoc_no_runtime_rc "${PIPESTATUS[0]} :: $(tr '\n' ' ' <"$W/ext-a.txt")"
echo "--- (b) ad-hoc + --options runtime"
"$W/probe-hr" ext "$EXTDIR" "http://127.0.0.1:8765/test.csv" 2>&1 | tee "$W/ext-b.txt"; R ext_adhoc_hardened_runtime_rc "${PIPESTATUS[0]} :: $(tr '\n' ' ' <"$W/ext-b.txt" | cut -c1-900)"
echo "--- (c) ad-hoc + --options runtime + disable-library-validation"
"$W/probe-hr-dlv" ext "$EXTDIR" "http://127.0.0.1:8765/test.csv" 2>&1 | tee "$W/ext-c.txt"; R ext_hardened_plus_dlv_rc "${PIPESTATUS[0]} :: $(tr '\n' ' ' <"$W/ext-c.txt" | cut -c1-900)"
echo "--- extension files"
find "$EXTDIR" -name '*.duckdb_extension' | while read -r f; do
  echo "$f"; file "$f"; codesign -dvvv "$f" 2>&1 | grep -E 'Signature|Identifier|flags|Authority|TeamIdentifier|not signed' ; vtool -show-build "$f" 2>&1 | grep -E 'platform|minos|sdk'
done
R excel_ext_codesign "$(codesign -dvvv "$(find "$EXTDIR" -name 'excel.duckdb_extension' | head -1)" 2>&1 | grep -E 'Signature|not signed|flags' | tr '\n' ' ')"
kill "$HTTP_PID" 2>/dev/null
E

G "8 keychain survives a re-sign"
export -n ANTHROPIC_API_KEY VIBE_LLM_API_KEY 2>/dev/null; unset ANTHROPIC_API_KEY VIBE_LLM_API_KEY
export VIBE_CONFIG_DIR="$W/cfg"; mkdir -p "$VIBE_CONFIG_DIR"; export HOME_BAK="$HOME"
cp "$ROOT/vibe" "$W/vibe-v1"; codesign --force -s - "$W/vibe-v1"
(cd "$ROOT" && go build -ldflags "-X github.com/SandwichLabs/vibe/cmd.version=9.9.9-upgrade" -o "$W/vibe-v2" ./cmd/vibe); R build_v2_rc "$?"
codesign --force -s - "$W/vibe-v2"
c1="$(codesign -dvvv "$W/vibe-v1" 2>&1 | grep -E '^CDHash')"; c2="$(codesign -dvvv "$W/vibe-v2" 2>&1 | grep -E '^CDHash')"
R cdhash_v1_v2 "$c1 -> $c2"
keychain_round() { # $1 label, $2 service
  export VIBE_KEYRING_SERVICE="$2"
  printf 'dummy-secret-%s\n' "$1" | "$W/vibe-v1" config llm-key set --provider anthropic </dev/stdin; R "kc_${1}_set_v1_rc" "$?"
  "$W/vibe-v1" config llm-key status --provider anthropic 2>&1 | tr '\n' ' ' >"$W/st1"; R "kc_${1}_status_v1" "$(cat "$W/st1")"
  "$W/vibe-v2" config llm-key status --provider anthropic 2>&1 | tr '\n' ' ' >"$W/st2"; R "kc_${1}_status_after_resign_v2" "$(cat "$W/st2")"
  security find-generic-password -s "$2" 2>&1 | head -8
  security dump-keychain 2>&1 | grep -A12 "\"$2\"" | head -20
  "$W/vibe-v2" config llm-key clear --provider anthropic 2>&1; R "kc_${1}_clear_rc" "$?"
}
echo "--- default (runner login) keychain"
security list-keychains -d user; security default-keychain
keychain_round login vibe-spike-login
echo "--- temporary keychain as default"
KC="$RUNNER_TEMP/spike.keychain-db"
security create-keychain -p spikepw "$KC" && security set-keychain-settings -lut 7200 "$KC" && security unlock-keychain -p spikepw "$KC"
OLD="$(security list-keychains -d user | tr -d '"' | tr '\n' ' ')"
security list-keychains -d user -s "$KC" $OLD
security default-keychain -d user -s "$KC"; security default-keychain
keychain_round temp vibe-spike-temp
security delete-keychain "$KC" 2>&1
E

G "9 minimum macOS (LC_BUILD_VERSION) and Info.plist"
for f in "$ROOT/vibe" "$APP0/Contents/MacOS/vibe-studio" "$ROOT/probe"; do
  echo "== $f"; lipo -info "$f"; vtool -show-build "$f"
  R "minos_$(basename "$f")" "$(vtool -show-build "$f" | grep -E 'minos|sdk' | tr -s ' ' | tr '\n' ' ')"
done
echo "all LC_BUILD_VERSION minos in Studio:"; otool -l "$APP0/Contents/MacOS/vibe-studio" | grep -A4 LC_BUILD_VERSION
R plist_LSMinimumSystemVersion "$(/usr/libexec/PlistBuddy -c 'Print :LSMinimumSystemVersion' "$APP0/Contents/Info.plist")"
plutil -p "$APP0/Contents/Info.plist"
otool -L "$APP0/Contents/MacOS/vibe-studio" | head -30
E
echo "SPIKE DONE"
