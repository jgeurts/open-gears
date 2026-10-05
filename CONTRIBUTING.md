# Contributing

Open Gears verifies SM-BCR2 adapter access on macOS. Bicycle discovery, paddle reading, and guarded X/Y assignment editing are experimental. Extra-button editors, other setting writes, flash updates, and error checks are not implemented. Actual component writes still need hardware validation.

## Development

Use Go 1.24 or later and the native build tools described in [README.md](README.md). Run:

```sh
make build
make check
make app       # macOS: compile the native app
```

`make check` builds the pinned USB dependency, checks gofmt, runs go vet, and runs the Go tests with the race detector. On macOS it also tests native helper execution with synthetic processes, including large concurrent output and timeout cleanup. Isolated installer tests verify platform selection and preservation of an existing app on validation failures. Tests use synthetic packets/captures and do not operate hardware. Changes to app code also need `make app`; Go tests do not verify Swift behavior.

Linux test binaries need the private libusb directory in their runtime search path. `scripts/check.sh` supplies it; link-time `-L` alone is insufficient. Distribution binaries instead resolve their adjacent bundled library.

Mac packaging edits the executable's library load commands after Go links it. Go can recognize the unchanged build ID and reuse that already-edited output on a repeat build. `scripts/build.sh` therefore builds to a fresh temporary path before replacing the CLI. Keep this step: otherwise a repeat build can fail when adding the same runtime library path twice. CI exercises `make build` followed by `make package` with the same version.

Use the shared Linear ticket when that integration is available. Record follow-up work there, use a `codex/` branch unless the task specifies another name, and link the ticket and GitHub PR. Keep changes focused and document incomplete checks explicitly.

## Protocol changes

- Extend [docs/protocol.md](docs/protocol.md) with the source, inspected version/hash, exact bytes, and whether a claim is documented, recovered, inferred, or live verified.
- Preserve unknown identities and payloads. Do not label synthetic results as bicycle readings or infer components from a bicycle model.
- Test malformed frames, split transfers, checksums, response correlation, cancellation, and cleanup where relevant. Hardware-dependent behavior needs a separately recorded hardware run.
- Keep read commands separate from stored-setting writes and firmware updates. A new write path needs component/version checks, an explicit change preview, readback, and an established recovery procedure before exposure.
- Do not copy proprietary application source or commit Shimano images, installers, or complete firmware-upload traces. A trace can reconstruct the original image even when the filename is absent.

Before a hardware experiment, state the expected command and response and which state it changes. Use one client, record the result, and attempt service-session cleanup. If cleanup cannot be confirmed, disconnect the charger from the bike and USB and verify that the bicycle returns to normal.

## Pull requests and releases

Describe the concrete behavior changed, why it is needed, and the checks performed. Distinguish live evidence from synthetic tests. Do not add generated-by footers or tool attribution.

CI builds the Go CLI for macOS/Linux on amd64 and arm64; Mac jobs also compile the app. Mac builds require macOS Tahoe 26 or later, with CI using macOS 26 runners for Apple silicon and Intel. Pushes, PRs, and manual runs exercise builds and checks. Release publication is reserved for `v*` tags.

Before tagging a release:

1. Run `make check`, compile the Mac app, and exercise packaged CLI/app artifacts on the relevant Mac architecture.
2. Verify bundled dynamic library paths work without Homebrew or developer build directories.
3. Include checksums, the MIT project license, third-party licenses, and `libusb-1.0.30-patched-source.tar.gz` with upstream source, the patch, LGPL license, and rebuild instructions.
4. Update [CHANGELOG.md](CHANGELOG.md), the capability table, the README's pinned install command, and the default version in `scripts/install.sh`. Record platforms and hardware functions not verified.

Mac artifacts use an ad hoc signature and have no Developer ID signature or notarization. Do not describe compilation or a local launch as distribution-signing/notarization verification.
