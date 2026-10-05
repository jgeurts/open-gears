#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
app=false
package=false
for arg in "$@"; do
    case "$arg" in
        --app) app=true ;;
        --package) package=true ;;
        *) echo "Usage: $0 [--app] [--package]" >&2; exit 1 ;;
    esac
done
os=$(go env GOOS)
arch=$(go env GOARCH)
case "$os" in darwin|linux) ;; *) echo 'Supported builds: macOS and Linux' >&2; exit 1 ;; esac
if "$package" && test "$os" = darwin; then app=true; fi
if "$app" && test "$os" != darwin; then echo 'The native app requires macOS' >&2; exit 1; fi
version=${VERSION:-dev}
case "$version" in *[!a-zA-Z0-9._-]*|'') echo 'Invalid VERSION' >&2; exit 1 ;; esac
commit=$(git -C "$root" rev-parse --short HEAD 2>/dev/null || printf unknown)
"$root/scripts/build-libusb.sh"
. "$root/scripts/go-env.sh"
cd "$root"
mkdir -p bin/lib build
if test "$os" = linux; then
    export CGO_LDFLAGS="$CGO_LDFLAGS -Wl,-rpath,\$ORIGIN/lib"
fi
# Go may reuse an existing executable with the same build ID even after macOS
# packaging changed its load commands. Link to a fresh path before patching.
cli_temp=$(mktemp "$root/build/open-gears.XXXXXX")
trap 'rm -f "$cli_temp"' 0
trap 'exit 1' HUP INT TERM
go build -trimpath -ldflags="-s -w -X main.version=$version -X main.commit=$commit" -o "$cli_temp" ./cmd/open-gears
mv "$cli_temp" bin/open-gears
if test "$os" = darwin; then
    cp "$prefix/lib/libusb-1.0.0.dylib" bin/lib/
    install_name_tool -id '@rpath/libusb-1.0.0.dylib' bin/lib/libusb-1.0.0.dylib
    install_name_tool -change "$prefix/lib/libusb-1.0.0.dylib" '@rpath/libusb-1.0.0.dylib' bin/open-gears
    install_name_tool -add_rpath '@loader_path/lib' bin/open-gears
    codesign --force --sign - bin/lib/libusb-1.0.0.dylib bin/open-gears
else
    cp -L "$prefix/lib/libusb-1.0.so.0" bin/lib/libusb-1.0.so.0
fi
if "$app"; then
    bundle="$root/bin/Open Gears.app"
    rm -rf "$bundle"
    mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Frameworks" "$bundle/Contents/Resources"
    cp macos/Info.plist "$bundle/Contents/Info.plist"
    cp macos/AppIcon.icns "$bundle/Contents/Resources/"
    cp bin/open-gears "$bundle/Contents/MacOS/open-gears"
    cp bin/lib/libusb-1.0.0.dylib "$bundle/Contents/Frameworks/"
    install_name_tool -add_rpath '@loader_path/../Frameworks' "$bundle/Contents/MacOS/open-gears"
    swiftc -swift-version 6 -O -parse-as-library -module-cache-path "$root/build/swift-cache" -target "$(uname -m)-apple-macosx26.0" macos/*.swift -o "$bundle/Contents/MacOS/OpenGears"
    cp LICENSE "$bundle/Contents/Resources/"
    codesign --force --deep --sign - "$bundle"
fi
if "$package"; then
    mkdir -p dist
    name="open-gears-$version-$os-$arch"
    stage="$root/build/package/$name"
    rm -rf "$stage"
    mkdir -p "$stage/lib" "$stage/licenses"
    cp bin/open-gears "$stage/"
    cp bin/lib/* "$stage/lib/"
    cp LICENSE README.md "$stage/"
    cp "$root/build/deps/libusb-1.0.30/COPYING" "$stage/licenses/libusb-LGPL-2.1.txt"
    cp THIRD_PARTY_NOTICES.md "$stage/licenses/"
    gousb_dir=$(go list -m -f '{{.Dir}}' github.com/google/gousb)
    cp "$gousb_dir/LICENSE" "$stage/licenses/gousb-Apache-2.0.txt"
    tar -czf "$root/dist/$name.tar.gz" -C "$root/build/package" "$name"
    if "$app"; then
        cp "$stage/licenses/"* "$bundle/Contents/Resources/"
        codesign --force --deep --sign - "$bundle"
        ditto -c -k --keepParent "$bundle" "$root/dist/Open-Gears-$version-$os-$arch.zip"
    fi
    source_stage="$root/build/package/libusb-source"
    mkdir -p "$source_stage"
    cp build/deps/libusb-1.0.30.tar.bz2 build/deps/darwin-shutdown.patch "$source_stage/"
    cp build/deps/libusb-1.0.30/COPYING "$source_stage/"
    cp scripts/build-libusb.sh "$source_stage/"
    printf '%s\n' 'libusb 1.0.30 plus upstream commit 94a5224ea1a7515c38c618694e8794e058c16412.' 'Extract the source tar, then patch -p1 < ../darwin-shutdown.patch.' './configure --disable-static; make; make install. Dynamic linking permits library replacement.' > "$source_stage/README.txt"
    tar -czf dist/libusb-1.0.30-patched-source.tar.gz -C "$source_stage" .
fi
