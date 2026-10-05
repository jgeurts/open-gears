#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/open-gears-installer-test.XXXXXX")
trap 'rm -rf "$fixture_dir"' 0
trap 'exit 1' HUP INT TERM
mkdir "$fixture_dir/tools"
printf 'Synthetic release archive\n' > "$fixture_dir/archive"

cat > "$fixture_dir/tools/uname" <<'STUB'
#!/bin/sh
case "$1" in
    -s) printf 'Darwin\n' ;;
    -m) printf '%s\n' "$OPEN_GEARS_TEST_ARCH" ;;
    *) exit 1 ;;
esac
STUB
cat > "$fixture_dir/tools/sw_vers" <<'STUB'
#!/bin/sh
printf '26.0\n'
STUB
cat > "$fixture_dir/tools/curl" <<'STUB'
#!/bin/sh
output=
url=
if test "$OPEN_GEARS_TEST_DOWNLOAD_FAILURE" = true; then exit 22; fi
while test "$#" -gt 0; do
    case "$1" in
        --output) output=$2; shift 2 ;;
        https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >> "$OPEN_GEARS_TEST_LOG"
case "$url" in
    */SHA256SUMS)
        hash=$(shasum -a 256 "$OPEN_GEARS_TEST_ARCHIVE")
        hash=${hash%% *}
        if test "$OPEN_GEARS_TEST_CHECKSUM_FAILURE" = true; then
            hash=0000000000000000000000000000000000000000000000000000000000000000
        fi
        printf '%s  %s.extra.zip\n%s  %s\n' "$hash" "$OPEN_GEARS_TEST_ASSET" "$hash" "$OPEN_GEARS_TEST_ASSET" > "$output"
        ;;
    */"$OPEN_GEARS_TEST_ASSET") cp "$OPEN_GEARS_TEST_ARCHIVE" "$output" ;;
    *) exit 1 ;;
esac
STUB
cat > "$fixture_dir/tools/ditto" <<'STUB'
#!/bin/sh
if test "$1" = -x; then
    bundle="$4/Open Gears.app"
    mkdir -p "$bundle/Contents/MacOS"
    printf '<plist/>\n' > "$bundle/Contents/Info.plist"
    printf '#!/bin/sh\nexit 0\n' > "$bundle/Contents/MacOS/OpenGears"
    chmod +x "$bundle/Contents/MacOS/OpenGears"
else
    cp -R "$1" "$2"
fi
STUB
cat > "$fixture_dir/tools/codesign" <<'STUB'
#!/bin/sh
test "$OPEN_GEARS_TEST_SIGNATURE_FAILURE" = false
STUB
cat > "$fixture_dir/tools/mv" <<'STUB'
#!/bin/sh
if test "$OPEN_GEARS_TEST_REPLACEMENT_FAILURE" = true; then
    case "$1" in */.open-gears-install.*/Open\ Gears.app) exit 1 ;; esac
fi
exec "$OPEN_GEARS_TEST_MV" "$@"
STUB
chmod +x "$fixture_dir/tools/"*

export OPEN_GEARS_INSTALL_DIR OPEN_GEARS_VERSION OPEN_GEARS_TEST_ARCH OPEN_GEARS_TEST_ASSET
export OPEN_GEARS_TEST_LOG OPEN_GEARS_TEST_ARCHIVE OPEN_GEARS_TEST_CHECKSUM_FAILURE OPEN_GEARS_TEST_SIGNATURE_FAILURE
export OPEN_GEARS_TEST_DOWNLOAD_FAILURE OPEN_GEARS_TEST_REPLACEMENT_FAILURE OPEN_GEARS_TEST_MV
OPEN_GEARS_TEST_MV=$(command -v mv)
OPEN_GEARS_TEST_ARCHIVE="$fixture_dir/archive"
OPEN_GEARS_VERSION=
OPEN_GEARS_TEST_CHECKSUM_FAILURE=false
OPEN_GEARS_TEST_SIGNATURE_FAILURE=false
OPEN_GEARS_TEST_DOWNLOAD_FAILURE=false
OPEN_GEARS_TEST_REPLACEMENT_FAILURE=false
run_installer() {
    # Read from stdin, as in the documented curl | sh installation command.
    PATH="$fixture_dir/tools:$PATH" sh < "$root/scripts/install.sh" > "$fixture_dir/output" 2>&1
}
prepare_case() {
    OPEN_GEARS_INSTALL_DIR="$fixture_dir/$1/Applications with spaces"
    OPEN_GEARS_TEST_LOG="$fixture_dir/$1/downloads"
    mkdir -p "$OPEN_GEARS_INSTALL_DIR/Open Gears.app"
    printf 'previous app\n' > "$OPEN_GEARS_INSTALL_DIR/Open Gears.app/previous.txt"
}
fail() {
    cat "$fixture_dir/output" >&2
    printf 'Installer test failed: %s\n' "$*" >&2
    exit 1
}

for architecture in arm64 x86_64; do
    prepare_case "$architecture"
    OPEN_GEARS_TEST_ARCH=$architecture
    case "$architecture" in arm64) release_arch=arm64 ;; x86_64) release_arch=amd64 ;; esac
    if test "$architecture" = x86_64; then OPEN_GEARS_VERSION=v9.8.7; fi
    release_version=${OPEN_GEARS_VERSION:-v0.1.0-alpha.3}
    OPEN_GEARS_TEST_ASSET="Open-Gears-$release_version-darwin-$release_arch.zip"
    run_installer || fail "$architecture installation"
    test -x "$OPEN_GEARS_INSTALL_DIR/Open Gears.app/Contents/MacOS/OpenGears" || fail 'installed app missing'
    test ! -e "$OPEN_GEARS_INSTALL_DIR/Open Gears.app/previous.txt" || fail 'previous app was not replaced'
    for previous in "$OPEN_GEARS_INSTALL_DIR"/Open\ Gears\ backup.*/Open\ Gears.app/previous.txt; do
        test "$(cat "$previous")" = 'previous app' || fail 'previous app backup missing'
    done
    grep -F "https://github.com/jgeurts/open-gears/releases/download/$release_version/$OPEN_GEARS_TEST_ASSET" "$OPEN_GEARS_TEST_LOG" >/dev/null || fail 'wrong release architecture or version requested'
done

for failure in download checksum signature replacement; do
    prepare_case "$failure"
    OPEN_GEARS_TEST_CHECKSUM_FAILURE=false
    OPEN_GEARS_TEST_SIGNATURE_FAILURE=false
    OPEN_GEARS_TEST_DOWNLOAD_FAILURE=false
    OPEN_GEARS_TEST_REPLACEMENT_FAILURE=false
    case "$failure" in
        download) OPEN_GEARS_TEST_DOWNLOAD_FAILURE=true ;;
        checksum) OPEN_GEARS_TEST_CHECKSUM_FAILURE=true ;;
        signature) OPEN_GEARS_TEST_SIGNATURE_FAILURE=true ;;
        replacement) OPEN_GEARS_TEST_REPLACEMENT_FAILURE=true ;;
    esac
    if run_installer; then fail "$failure failure was accepted"; fi
    test "$(cat "$OPEN_GEARS_INSTALL_DIR/Open Gears.app/previous.txt")" = 'previous app' || fail "$failure failure changed the previous app"
    test ! -e "$OPEN_GEARS_INSTALL_DIR/Open Gears.app/Contents" || fail "$failure failure installed an app"
done
printf 'Installer tests passed (Apple silicon, Intel, download/checksum/signature failure, replacement recovery).\n'
