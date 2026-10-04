#!/bin/sh
set -eu
if test "$(uname -s)" != Darwin; then
    printf '%s\n' 'Skipping native process tests: macOS is required.'
    exit 0
fi
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_dir="$root/build/swift-tests"
mkdir -p "$test_dir" "$root/build/swift-cache"
target="$(uname -m)-apple-macosx26.0"
swiftc -swift-version 6 -strict-concurrency=complete -parse-as-library \
    -module-cache-path "$root/build/swift-cache" -target "$target" \
    "$root/macos/tests/ProcessFixture.swift" -o "$test_dir/process-fixture"
swiftc -swift-version 6 -strict-concurrency=complete -parse-as-library \
    -module-cache-path "$root/build/swift-cache" -target "$target" \
    "$root/macos/CLIRunner.swift" "$root/macos/tests/CLIRunnerTests.swift" \
    -o "$test_dir/cli-runner-tests"
"$test_dir/cli-runner-tests" "$test_dir/process-fixture"
swiftc -swift-version 6 -strict-concurrency=complete -parse-as-library \
    -module-cache-path "$root/build/swift-cache" -target "$target" \
    "$root/macos/CLIRunner.swift" "$root/macos/Models.swift" \
    "$root/macos/AppModel.swift" "$root/macos/tests/AppModelTests.swift" \
    -o "$test_dir/app-model-tests"
"$test_dir/app-model-tests"
