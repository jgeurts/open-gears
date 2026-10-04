#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
verify_cli() {
    cli=$1
    "$cli" version
    "$cli" protocol decode bb25300100aabb
    if test "$(uname -s)" = Darwin; then
        deps=$(otool -L "$cli")
        printf '%s\n' "$deps"
        printf '%s\n' "$deps" | grep -q '@rpath/libusb-1.0.0.dylib'
        if printf '%s\n' "$deps" | grep -E '/(opt/homebrew|usr/local|Users|private/tmp)/'; then
            echo 'CLI still depends on a development library path' >&2
            exit 1
        fi
    else
        ldd "$cli"
        if ldd "$cli" | grep -q 'not found'; then exit 1; fi
    fi
}

for archive in dist/open-gears-*.tar.gz; do
    test -f "$archive"
    name=$(basename "$archive" .tar.gz)
    tar -xzf "$archive" -C "$stage"
    verify_cli "$stage/$name/open-gears"
done
if test "$(uname -s)" = Darwin; then
    for archive in dist/Open-Gears-*.zip; do
        test -f "$archive"
        app_stage="$stage/$(basename "$archive" .zip)"
        mkdir -p "$app_stage"
        ditto -x -k "$archive" "$app_stage"
        bundle="$app_stage/Open Gears.app"
        codesign --verify --deep --strict "$bundle"
        verify_cli "$bundle/Contents/MacOS/open-gears"
    done
fi
