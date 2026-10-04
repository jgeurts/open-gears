#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
deps="$root/build/deps"
prefix="$deps/install"
version=1.0.30
fix=94a5224ea1a7515c38c618694e8794e058c16412
source_hash=fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf
patch_hash=bfb47d978347d090a03f43623688311d22b9aa0a16f772dc470e97802a5e5ca2
mkdir -p "$deps"

verify() {
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$1" | cut -d ' ' -f 1)
    else
        actual=$(shasum -a 256 "$1" | cut -d ' ' -f 1)
    fi
    test "$actual" = "$2" || { echo "SHA256 mismatch: $1" >&2; exit 1; }
}
fetch() {
    if ! test -f "$1"; then
        curl --fail --location --retry 3 --proto '=https' "$3" -o "$1.part"
        mv "$1.part" "$1"
    fi
    verify "$1" "$2"
}
fetch "$deps/libusb-$version.tar.bz2" "$source_hash" "https://github.com/libusb/libusb/releases/download/v$version/libusb-$version.tar.bz2"
fetch "$deps/darwin-shutdown.patch" "$patch_hash" "https://github.com/libusb/libusb/commit/$fix.patch"

build_id="$version-$fix-$(uname -s)-$(uname -m)-macos26-v3"
if test -f "$prefix/open-gears-build-id" && test "$(cat "$prefix/open-gears-build-id")" = "$build_id"; then
    exit 0
fi
rm -rf "$deps/libusb-$version" "$prefix"
tar -xjf "$deps/libusb-$version.tar.bz2" -C "$deps"
patch -d "$deps/libusb-$version" -p1 < "$deps/darwin-shutdown.patch"
cd "$deps/libusb-$version"
if test "$(uname -s)" = Darwin; then export MACOSX_DEPLOYMENT_TARGET=26.0; fi
./configure --prefix="$prefix" --disable-static --disable-examples-build --disable-tests-build
if test "$(uname -s)" = Darwin; then
    jobs=$(sysctl -n hw.ncpu)
else
    jobs=$(getconf _NPROCESSORS_ONLN)
fi
make -j"$jobs"
make install
printf '%s\n' "$build_id" > "$prefix/open-gears-build-id"
