# Changelog

## 0.1.0-alpha.2 — 2026-10-04

### Fixed

- Read SM-BCR2 bridge status notifications while the UART is open, including during close. Leaving these notifications unread caused intermittent UART close stalls and bridge resets on the tested Mac.
- Keep interrupt-reader cancellation and completion ahead of USB handle teardown, and serialize trace recording across readers.

## 0.1.0-alpha.1 — 2026-10-04

### Added

- Native macOS app and Go CLI for Shimano SM-BCR2, with structured JSON results.
- USB descriptor discovery and selection by bus/address.
- Fingerprinted OEM controller RAM-image loading and TI UART initialization.
- Adapter link and firmware queries, verified on macOS with SM-BCR2 firmware 3.0.1 revision 0.
- Legacy reset, reconnection, and power-unlock preparation, followed by role-aware bicycle discovery. The adapter reached ready master mode and returned an empty component bitmap in the live test.
- Experimental bicycle discovery, component identity/version queries, battery-level reads, and model-specific paddle reads.
- Offline capture import, analysis, comparison, and legacy serial-frame decoding.
- Synthetic tests for parsers, framing, firmware validation, and bicycle response handling.
- Native Mac tests for helper timeouts, concurrent output, and report ownership after USB reconnection.
- macOS/Linux build and release packaging with bundled dynamic libusb, corresponding library source, and license notices.

### Fixed

- macOS USB shutdown hang by applying the upstream libusb hotplug teardown fix.
- Trace-storage failures no longer interrupt USB communication or session cleanup.
- Reports clear when a different physical adapter is selected after a read.
- Slave-mode cleanup ends occupied components before the master component.

### Known limitations

- The current connection reports zero bicycle components. The cause is unconfirmed; actual component, battery-level, and paddle reads have not been verified live.
- Stored-setting writes, adapter/component flash updates, and error checks are not implemented.
- Linux hardware access is not verified. Windows is not implemented.
- Mac app and packaged CLI require macOS Tahoe 26 or later and have no Developer ID signature or notarization.
- OEM controller firmware must be supplied locally and is not distributed.
