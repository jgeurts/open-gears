#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
"$root/scripts/build-libusb.sh"
. "$root/scripts/go-env.sh"
if test "$(uname -s)" = Linux; then
    export LD_LIBRARY_PATH="$prefix/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
fi
cd "$root"
unformatted=$(gofmt -l cmd internal)
test -z "$unformatted" || { printf 'Run gofmt on:\n%s\n' "$unformatted" >&2; exit 1; }
go vet ./...
go test -race -count=1 ./...
"$root/scripts/check-swift.sh"
