# Security

Open Gears is an early hardware client. The supported boundary is USB discovery and adapter information; experimental bicycle reads enter a transient PC service session. The current implementation does not expose stored-setting writes, flash firmware updates, or arbitrary USB-command execution.

## Reporting

Use the repository's private **Report a vulnerability** option in the GitHub Security tab when available. If private reporting is unavailable, open an issue requesting a private contact without posting exploit details or private captures. Include the affected version, OS/architecture, reproduction steps, and expected behavior.

Ordinary compatibility problems, including empty component results, belong in GitHub Issues. There is no guaranteed response time or security support window at this stage.

## Files and hardware

- Only the fingerprinted OEM controller RAM image is accepted. This narrows the input; it does not make the proprietary image open source or establish compatibility with other adapters.
- Keep Shimano binaries local. Before sharing a capture, remove firmware-upload bytes and identifying data. Built-in traces exclude the controller image payload; externally imported captures may contain it.
- Treat capture files as untrusted inputs. Offline parsing/decoding must not initiate hardware operations.
- Use one client per adapter. If service-session cleanup fails, stop the client, disconnect the charger from the bicycle and USB, and verify normal operation before riding.
- Mac releases have no Developer ID signature or notarization; packaging uses an ad hoc signature. Verify the published checksum and source; use a per-app launch exception rather than disabling OS protections globally.

Changes that introduce persistent settings writes, flash firmware updates, automatic retries of ambiguous writes, new firmware acceptance, or broader USB access need explicit review and hardware recovery evidence.
