#!/bin/sh
set -eu

fail() {
    printf 'Open Gears: %s\n' "$*" >&2
    exit 1
}

test "$(uname -s)" = Darwin || fail 'This installer requires macOS Tahoe 26 or later.'
macos_version=$(sw_vers -productVersion)
macos_major=${macos_version%%.*}
case "$macos_major" in
    ''|*[!0-9]*) fail "Cannot determine the macOS version: $macos_version" ;;
esac
test "$macos_major" -ge 26 || fail "macOS Tahoe 26 or later is required; this Mac runs $macos_version."
case "$(uname -m)" in
    arm64) arch=arm64 ;;
    x86_64) arch=amd64 ;;
    *) fail 'Supported Macs use Apple silicon or Intel processors.' ;;
esac

version=${OPEN_GEARS_VERSION:-v0.1.0-alpha.3}
case "$version" in
    v[0-9]*) ;;
    *) fail 'OPEN_GEARS_VERSION must be a release tag such as v0.1.0-alpha.3.' ;;
esac
case "$version" in
    *[!a-zA-Z0-9._-]*) fail 'OPEN_GEARS_VERSION contains invalid characters.' ;;
esac
install_dir=${OPEN_GEARS_INSTALL_DIR:-"${HOME:?HOME is not set}/Applications"}
case "$install_dir" in
    /*) ;;
    *) fail 'OPEN_GEARS_INSTALL_DIR must be an absolute directory path.' ;;
esac
for command in curl shasum ditto codesign mktemp awk; do
    command -v "$command" >/dev/null 2>&1 || fail "Required macOS tool is missing: $command"
done

work_dir=
stage_dir=
backup_dir=
destination="$install_dir/Open Gears.app"
committed=false
cleanup() {
    status=$?
    trap - 0 HUP INT TERM
    if test "$committed" = false && test -n "$backup_dir"; then
        previous="$backup_dir/Open Gears.app"
        if test -e "$previous" || test -L "$previous"; then
            if ! test -e "$destination" && ! test -L "$destination"; then
                if mv "$previous" "$destination"; then
                    rmdir "$backup_dir" || :
                else
                    printf 'Open Gears: restore the previous app from %s\n' "$previous" >&2
                fi
            else
                printf 'Open Gears: the previous app is preserved at %s\n' "$previous" >&2
            fi
        else
            rmdir "$backup_dir" 2>/dev/null || :
        fi
    fi
    if test -n "$stage_dir"; then rm -rf "$stage_dir"; fi
    if test -n "$work_dir"; then rm -rf "$work_dir"; fi
    exit "$status"
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/open-gears-install.XXXXXX")
asset="Open-Gears-$version-darwin-$arch.zip"
release_url="https://github.com/jgeurts/open-gears/releases/download/$version"
printf 'Downloading Open Gears %s for %s…\n' "$version" "$arch"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --retry 2 --connect-timeout 15 --output "$work_dir/$asset" "$release_url/$asset" \
    || fail "Download failed. Check that release $version has a Mac app for $arch."
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --retry 2 --connect-timeout 15 --output "$work_dir/SHA256SUMS" "$release_url/SHA256SUMS" \
    || fail 'Could not download the release checksums.'
expected=$(awk -v asset="$asset" '
    $2 == asset || $2 == "*" asset {
        if (NF != 2 || length($1) != 64 || $1 !~ /^[0-9a-fA-F]+$/) exit 1
        count++
        hash = tolower($1)
    }
    END {
        if (count != 1) exit 1
        print hash
    }
' "$work_dir/SHA256SUMS") || fail 'The release has no unique, valid checksum for this app.'
actual=$(shasum -a 256 "$work_dir/$asset") || fail 'Could not calculate the app checksum.'
actual=${actual%% *}
test "$actual" = "$expected" || fail 'The downloaded app failed checksum verification; the current app was kept.'

ditto -x -k "$work_dir/$asset" "$work_dir/extracted" || fail 'Could not extract the app archive.'
bundle="$work_dir/extracted/Open Gears.app"
test -d "$bundle" && ! test -L "$bundle" \
    && test -f "$bundle/Contents/Info.plist" \
    && test -x "$bundle/Contents/MacOS/OpenGears" \
    || fail 'The archive does not contain a complete Open Gears app.'
codesign --verify --deep --strict "$bundle" || fail 'The app failed code signature verification; the current app was kept.'

mkdir -p "$install_dir" || fail "Could not create the install directory: $install_dir"
stage_dir=$(mktemp -d "$install_dir/.open-gears-install.XXXXXX")
ditto "$bundle" "$stage_dir/Open Gears.app" || fail 'Could not stage the app in the install directory.'
codesign --verify --deep --strict "$stage_dir/Open Gears.app" || fail 'The staged app failed code signature verification; the current app was kept.'
if test -e "$destination" || test -L "$destination"; then
    backup_dir=$(mktemp -d "$install_dir/Open Gears backup.XXXXXX")
    mv "$destination" "$backup_dir/Open Gears.app" || fail 'Could not preserve the previous app.'
fi
mv "$stage_dir/Open Gears.app" "$destination" || fail 'Could not install the app; restoring the previous app.'
committed=true

printf '\nInstalled Open Gears %s at:\n  %s\n' "$version" "$destination"
if test -n "$backup_dir"; then
    printf 'Previous app saved at:\n  %s/Open Gears.app\n' "$backup_dir"
fi
printf '\nOpen Open Gears from this folder in Finder.\n'
printf 'If macOS blocks the first launch, review Privacy & Security in System Settings and choose Open Anyway.\n'
