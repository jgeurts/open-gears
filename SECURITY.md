# Security

Open Gears is an early hardware client. USB discovery and adapter information are verified on macOS. Experimental bicycle reads and X/Y paddle assignment changes enter a transient PC service session. Paddle changes require an identified supported shifter, a fresh preview, and independent readback. Actual component writes remain unverified on hardware. Flash firmware updates and arbitrary USB-command execution are not exposed.

## Reporting

Use the repository's private **Report a vulnerability** option in the GitHub Security tab when available. If private reporting is unavailable, open an issue requesting a private contact without posting exploit details or private captures. Include the affected version, OS/architecture, reproduction steps, and expected behavior.

Ordinary compatibility problems, including empty component results, belong in GitHub Issues. There is no guaranteed response time or security support window at this stage.

## Files and hardware

- Only the fingerprinted OEM controller RAM image is accepted. This narrows the input; it does not make the proprietary image open source or establish compatibility with other adapters.
- Keep Shimano binaries local. Before sharing a capture, remove firmware-upload bytes and identifying data. Built-in traces exclude the controller image payload; externally imported captures may contain it.
- Treat capture files as untrusted inputs. Offline parsing/decoding must not initiate hardware operations.
- Paddle plans are bounded inputs tied to a physical USB port, component identity, firmware, and observed assignments. The client checks all selected shifters before the first write and never automatically repeats an ambiguous setting write. Multi-shifter changes can finish partially; retain the result and refresh before trying again.
- Use one client per adapter. If service-session cleanup fails, stop the client, disconnect the charger from the bicycle and USB, and verify normal operation before riding.
- Mac releases have no Developer ID signature or notarization; packaging uses an ad hoc signature. Verify the published checksum and source; use a per-app launch exception rather than disabling OS protections globally.

Changes that expand setting writes, introduce flash firmware updates or automatic retries of ambiguous writes, accept new firmware, or broaden USB access need explicit review and hardware recovery evidence.
