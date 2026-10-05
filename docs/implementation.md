# Initial SM-BCR2 USB milestone

This is the historical brief for the initial USB and protocol investigation. The current scope includes a native Mac app and guarded X/Y paddle editing; see the [README](../README.md) for capabilities and [protocol evidence](protocol.md) for write checks and hardware limitations.

Build a Go CLI for a Shimano SM-BCR2 attached to an older Specialized Diverge with Di2. Implement actual USB inspection and offline protocol investigation now, then extend live functionality only as primary-source evidence supports it. The bicycle model does not establish its installed Di2 components.

## Decisions

- Use Go with libusb through gousb. Native USB access is necessary: the currently attached adapter has vendor-specific class `ff` and no macOS serial port.
- Identify the adapter as `1e44:7220`. Select explicitly when multiple adapters are attached.
- Normal commands must not send unknown vendor requests, shift gears, change stored settings, or flash firmware. Descriptor inspection is available without opening a bike session.
- Driver inspection establishes a Texas Instruments USB serial controller. USB-controller startup firmware and bicycle-component firmware are different things; do not conflate them.
- Preserve unknown payloads as bytes. Never label inferred fields as verified, or present a demonstration as a bike reading.
- Do not redistribute Shimano binaries or firmware. Accept locally obtained files when needed for inspection.

## Work units

1. USB discovery and inspection: enumerate matching adapters and report configurations, interfaces and endpoints; support machine-readable output. Verify on the connected SM-BCR2 without changing configuration.
2. Offline capture analysis: import USBPcap/Wireshark data, normalize USB transfers, compare captures and retain unknown bytes. Use synthetic fixtures clearly labeled as such to verify parsing, direction, device filtering and comparison.
3. CLI and research handoff: expose implemented capabilities honestly, document supported OEM capabilities and adapter limitations, add installation instructions and CI, and record protocol evidence and next experiments.

Controller initialization and live Di2 commands are additional units only if verified evidence permits them during this work. Unknown application commands must remain unavailable rather than guessed.

## Verification and completion

- Format, test, vet and build all code. Test malformed/untrusted capture input and the invariant that inspection and offline commands cannot write to the adapter.
- Exercise the CLI on the actual adapter for read-only USB discovery; save a reproducible descriptor snapshot, without serial numbers from unrelated devices.
- Review the integrated implementation, fix actionable defects, commit and push, and open a GitHub PR when available.
- Report exactly which commands work, which functions need protocol evidence, and any unavailable tracking integration. Live remapping and firmware updates are incomplete until validated against the actual components.
