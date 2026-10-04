#!/bin/sh
# Source from a repository script, after setting root.
prefix="$root/build/deps/install"
export CGO_ENABLED=1
export PKG_CONFIG=true
export CGO_CFLAGS="-I$prefix/include/libusb-1.0"
export CGO_LDFLAGS="-L$prefix/lib -lusb-1.0"
if test "$(uname -s)" = Darwin; then
    export MACOSX_DEPLOYMENT_TARGET=26.0
    export CGO_CFLAGS="$CGO_CFLAGS -mmacosx-version-min=26.0"
    export CGO_LDFLAGS="$CGO_LDFLAGS -mmacosx-version-min=26.0"
fi
