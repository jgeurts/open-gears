# SM-BCR2 protocol evidence

This document records protocol facts needed to implement an independent client for older Shimano Di2 systems. It is not an official Shimano specification. USB initialization is documented by Texas Instruments and corroborated by the Linux driver. Bicycle commands were recovered by inspecting the archived Shimano E-Tube Project 3.4.5 application. No proprietary source or firmware is included in this repository.

## Evidence and verification levels

- **Documented:** explicitly described by a manufacturer document or the public Linux driver.
- **Recovered:** visible in the inspected E-Tube 3.4.5 assemblies, without executing that software.
- **Inferred:** combining the above sources to implement a different host operating system.
- **Live:** an actual device observation; it does not imply the rest of the protocol has been validated.

Initial live enumeration on macOS identified `1e44:7220`, device class `ff`, USB version `0110`, device version `0101`, no serial port, and no active interface children. That is consistent with the TI bridge awaiting host firmware. Successful descriptor reads alone do not establish UART or bicycle communication. Current hardware validation belongs in the implementation notes; recovered commands below must not be described as live tested solely because they exist in this document.

On 2026-10-04, a live macOS probe successfully loaded the recognized bridge image with header `00 38 f8`, reset/reopened the device, and reached configuration 2. One SET_CONFIG, modem-register clear, OPEN, and START sequence then produced valid framed adapter replies: link state `bb 24 00 dc bb` and firmware `bb 25 30 01 00 aa bb`, identifying adapter application firmware **3.0.1, revision 0**. This proves the bridge boot, serial transport, frame checksum, and adapter queries on this device. Bicycle commands and setting writes require separate validation.

A later live run on the same date added the recovered reset/reconnection/power-unlock preparation and got past the earlier mode-transition failure. The role probe (`03`, payload `00`) returned `23`, payload `10`, selecting adapter-master mode. Explicit master selection (`03/20`) returned `23/00`; the state query returned `24/a0`, ready DCAS-A/master with PC slot 0. Adapter slot query `1b` returned `3b`, payload `00 00 00 00`: **zero bicycle components were visible on that connection**. This does not establish that a physical connector was unplugged or identify the cause.

The completed power-preparation flow was then exercised on firmware 3.0.1: `1a/01` returned `01 00`, status `1a/02` returned `02 02`, and supply start `1a/03` returned `03 00`. The first master-role result triggered the full preparation retry; both passes succeeded. Discovery still returned an empty bitmap. Final adapter reset was acknowledged and USB close completed without error. This verifies adapter power commands and cleanup on this connection, not an occupied bicycle service session.

Component identity, paddle assignments, battery-level reads, and actual component service-session startup/cleanup remain experimental and unverified live, because the slot bitmap was empty. X/Y paddle changes now have a restricted preview/apply path with identity checks and readback; live setting writes remain unverified. Adapter/component flash updates and error checks are not exposed. Queries to an occupied component require transient service mode; failed cleanup requires disconnecting SM-BCR2 from the bike and USB and confirming that normal bicycle operation resumes.

## Sources and reproducibility

Primary sources:

1. [Shimano SM-BCR2 product information](https://bike.shimano.com/en-NZ/products/components/pdp.P-SM-BCR2.html) describes charging and PC connectivity for firmware updates and customization.
2. [Shimano USB driver installation manual](https://si.shimano.com/en/pdfs/um/7J4WU/UM-7J4WU-002-ENG.pdf) describes SM-BCR2 as a COM-port device under the supplied Windows driver.
3. [Texas Instruments TUSB3410 datasheet, revision J](https://www.ti.com/lit/ds/symlink/tusb3410.pdf), section 5.6.7 and Table 5-23, documents the host firmware header. It says that the driver generates the header and the user supplies a binary image.
4. [Linux TI USB 3410/5052 driver](https://github.com/torvalds/linux/blob/master/drivers/usb/serial/ti_usb_3410_5052.c) documents control requests, UART structures, firmware transfer, configuration changes, and modem control. It is GPL-2.0-or-later; its code is not copied here. Constants, message layouts, and observed behavior are recorded independently.
5. Shimano E-Tube Project 3.4.5 and SM-BCR2 driver binaries, identified below. These are proprietary binaries inspected locally; no redistribution permission was established.

The archived binaries were obtained from [BetterShifting's E-Tube archive](https://bettershifting.com/e-tube-project-archive/), a third-party mirror. The mirror is not proof of authenticity; hashes make the inspected inputs reproducible. Driver INF copyright is Shimano 2013, `DriverVer=09/06/2013,17.1.7.834`, service `umpusbwin8`, serial-port class, with firmware file `umpf3410.i51` and driver `umpusbvista.sys`. The two inspected managed assemblies report assembly version `0.0.3.7443`.

| Inspected input | SHA-256 |
| --- | --- |
| [SM-BCR2_Win10-64.zip](https://assets.bettershifting.com/drivers/SM-BCR2_Win10-64.zip) | `a33dc443da4c66782a553c7e3db97b079cb4b426efd9e7c7a5dd32f196f18a53` |
| [E-tube_Proj_V_3_4_5.zip](https://assets.bettershifting.com/archive/E-tube_Proj_V_3_4_5.zip) | `62266eedf48e9a8f6ec25dd68c9c899f405bf41d9bf9fe3931786877644d4787` |
| Extracted E-tube_Proj_V_3_4_5.exe | `cb1ff4ef8e8f5cc4382ffa26df6ba9426dc03d2a4809df345d468eb51fdd96ef` |
| Extracted umpf3410.i51 | `2a392186cf3d93b6a56514cdcd483a26097bf20b2bfc744bad7bb2fa1fd8bcd6` |
| Extracted smpce1com.dll | `c34eab626b4d31bb5dd1084569902fe43f0db205ea4572d16ec472e4c7c61e2b` |
| Extracted etubedatalinks.dll | `814d8096d9f6e5552d8131ff840d3bf407b0f0b089c9a0a821cbb34f141e5ab5` |
| Extracted e_tube_project.exe | `e0f16523dcaef90aedd8f271161f7f8747bb7109414ce55950ce7bffdb3979fb` |

Extraction did not run the Windows application or installer. The InstallShield overlay contains a Windows Installer package whose `Data1.cab` holds the assemblies. Transport behavior is in `smpce1com.dll` (`Common`, `DataLinksMain`, `WinSerialPort`, `DataLinksPce`); unit discovery and customization are in `etubedatalinks.dll` (`EtubeDataLinksDccCommand`, `EtubeDataLinksUnitCommand`, `SwitchFunctionalUnit`, and model definitions). Method names are locators, not redistributed source.

Related public projects do not establish this USB protocol: [reven-plugin-etube](https://github.com/reven-project/reven-plugin-etube) is Apache-2.0 firmware-format tooling, [di2-downgrade](https://github.com/janmarques/di2-downgrade) is a firmware downgrade guide, and [shimano-di2-media-controls](https://github.com/NitorCreations/shimano-di2-media-controls) uses wireless control. None supplies a verified SM-BCR2 transport implementation. BLE and ANT+ support must not be confused with wired USB settings access.

## USB bridge startup

**Documented, with an inferred SM-BCR2 application:** SM-BCR2 uses a TI TUSB3410/UMP USB-to-UART bridge. It is not a HID interface or a standard CDC ACM serial device. The Windows INF matches `USB\\VID_1E44&PID_7220` and installs a proprietary serial driver.

The TI driver treats a device with one configuration and one endpoint as boot mode. Configuration 1 is boot; configuration 2 is the active UART. Discover endpoint addresses from descriptors rather than assuming that every compatible adapter exposes the same addresses.

### RAM firmware download

The Shimano `umpf3410.i51` file is raw 8051 binary code, despite its suffix. Its first bytes are `02 00 1e 02 1a fb`, recognizable jump vectors. They must remain intact.

Prepend the TI header:

```text
length low, length high, sum8(raw firmware), raw firmware bytes...
```

The checksum is the additive sum modulo 256, not the negated E-Tube frame checksum. Length excludes the three header bytes. TI's datasheet describes byte 0 as the low length byte; byte 1 is the high byte (the table's repeated low-byte label is a typographical inconsistency with its two-byte length field and the Linux implementation).

For the inspected image:

```text
Raw length:       14336 / 0x3800
Raw checksum:     f8
Header:           00 38 f8
Transfer length:  14339
Wrapped SHA-256:  8628a24189e198e9d9f67e61d5dda1ed63809375585397c0f3c6d231500f7a7a
```

Linux firmware files reserve their first three bytes for a header which that driver overwrites. Shimano's raw `.i51` file does not reserve those bytes: **prepend**, do not overwrite. Reject unrecognized images rather than guessing their layout.

Send the wrapped image through the boot bulk OUT endpoint in packets of at most 64 bytes with a 1-second transfer timeout. Linux waits 100 ms and resets the TUSB3410, then the host reopens the device and selects configuration 2. This sequence successfully reached active mode in the live macOS probe; timing and disconnect handling on other hosts remain unverified. This loads volatile bridge RAM; it is distinct from updating adapter or bicycle flash firmware. Do not substitute the generic Linux `ti_3410.fw` for Shimano's image without testing interoperability.

### Active UART control requests

All following vendor OUT requests use `bmRequestType=40`, endpoint 0. IN requests use `c0`. For UART 1, `wIndex=0003`. Linux uses a 1-second request timeout.

| Request | bRequest | wValue | Data |
| --- | --- | --- | --- |
| GET_VERSION | `01` | `0000` | IN; structure depends on bridge firmware |
| GET_PORT_STATUS | `02` | `0000` | IN; command, module, error, modem status, line status |
| GET_CONFIG | `04` | `0000` | IN; UART structure |
| SET_CONFIG | `05` | `0000` | UART structure below |
| OPEN_PORT | `06` | `0089` | None |
| CLOSE_PORT | `07` | `0000` | None |
| START_PORT | `08` | `0000` | None |
| STOP_PORT | `09` | `0000` | None |
| PURGE_PORT | `0b` | `0080` input / `0000` output | None |

The 10-byte UART structure is `be16 divisor, be16 flags, dataBits, parity, stopBits, Xon, Xoff, uartMode`. TUSB3410 divisor is `(923077 + baud/2)/baud`; 38400 gives 24. Eight data bits is `03`, no parity `00`, one stop bit `00`, RS-232 UART mode `00`. Required flags are modem-status interrupts `2000` and automatic DMA start `4000`. With software and hardware flow control off, deterministic Xon/Xoff zero bytes produce:

```text
00 18 60 00 03 00 00 00 00 00
```

The E-Tube application explicitly selects **38400 baud, 8N1, RTS disabled**, write timeout 500 ms, for SM-BCR2. It does not assign DTR, so disabled DTR is inferred from the .NET SerialPort default. Do not use the SM-PCE02 settings: that adapter uses 2,000,000 baud, RTS enabled, and different padding.

The successful live SET_CONFIG used Xon=`11` and Xoff=`13`, the conventional serial defaults, while the flow-control flags remained disabled: `00 18 60 00 03 00 00 11 13 00`. The zero-Xon/Xoff variant above is a deterministic construction from the documented fields, not the tested payload.

TI SET_CONFIG asserts RTS and DTR internally. Linux corrects modem state after SET_CONFIG by a masked XDATA write to UART1 MCR at `ffa4`. For RTS, DTR, and loopback all cleared, use `bRequest=80`, `wIndex=0005` (RAM), `wValue=0000`, data:

```text
30 01 01 00 00 ff a4 34 00
```

Layout: XDATA address type `30`, byte type `01`, count `01`, address high word `0000`, address low word `ffa4`, mask `34`, value `00`. This is a bridge register write, not a bicycle EEPROM write. This request succeeded in the live minimal startup sequence.

Linux startup orders configuration, modem correction, OPEN, START, purge input, purge output, clear bulk endpoint halts, then configuration/modem correction/OPEN/START a second time before bulk IN reads. It also runs an interrupt IN reader for modem/error events. A user-space implementation should handle or record those events and must not treat interrupt packets as E-Tube UART data. USB bulk payloads themselves carry the serial byte stream; there is no recovered per-packet FTDI-style status prefix.

The live minimal sequence omitted both purges and the repeated OPEN/START and successfully queried the adapter. A preceding trial accepted input purge but stalled on output purge and was observed back in boot mode; its cause is unresolved. The inspected Windows driver also contains input/output purge requests `0b` with values `0080`/`0000`, so that failure does not establish unsupported command semantics. Use the successful minimal sequence until a reproducible need for additional steps is established.

macOS teardown exposed a separate host-library trap: stock libusb 1.0.30 can deadlock around hotplug shutdown. Project builds use that version with the upstream fix from [libusb PR #1780](https://github.com/libusb/libusb/pull/1780), merged as `94a5224`. The patched build stopped the observed host shutdown hang. The later mode-transition success followed the added OEM preparation sequence; it does not establish component communication. Release bundles dynamically link the patched library and provide its corresponding source and LGPL license; changing the library requires repeating packaged startup/teardown checks.

### Status interrupts are required for reliable CLOSE

**Documented:** the [Linux TI driver](https://github.com/torvalds/linux/blob/master/drivers/usb/serial/ti_usb_3410_5052.c) starts its interrupt reader before UART setup and leaves it running through CLOSE. CLOSE is vendor OUT `40`, request `07`, value `0000`, index `0003`, with no payload. No extra STOP or purge is required for that teardown sequence.

**Recovered:** the fingerprinted `umpf3410.i51` image listed above sends two-byte modem-status notifications when UART flag `2000` is enabled. Its notification routine (image offset `1c5c`, modem-status caller `096e`) waits for the previous interrupt IN packet to be consumed before queuing another. That wait does not service the watchdog. START enables the watchdog; normal firmware execution services it. These are behavioral observations from local image inspection, not redistributed firmware instructions.

The [TI datasheet, revision J](https://www.ti.com/lit/ds/symlink/tusb3410.pdf), sections 5.5.4.7–5.5.4.9, identifies XDATA `ff5a` as endpoint 3's input byte-count register: bit 7 marks an empty buffer. Section 5.5.2.1.3 documents the 128 ms watchdog reset. **Inferred from those documented registers and the recovered wait:** leaving a status packet unread can block the next notification, prevent watchdog servicing, and reset the bridge into boot mode. A CLOSE stall is therefore not evidence that the port was already closed.

**Live, 2026-10-04:** adapter-only Open → link/version queries → CLOSE cycles reproduced this on macOS Tahoe 26 arm64 with gousb 1.1.3, patched libusb 1.0.30, and adapter application firmware 3.0.1 revision 0. No bicycle service session was entered. Starting in boot mode, four cycles without an interrupt reader alternated successful CLOSE after RAM loading with CLOSE `PIPE` after reopening. The failed control transfers took 136–138 ms and the same physical USB path returned from runtime configuration 2/address 12 to boot configuration 1/address 11. Waiting 250 ms before CLOSE did not prevent the failure.

Changing only the host to continuously read interrupt IN `83` yielded four successful CLOSE transfers in 3.1–3.6 ms, with packets `34 d9` initially and `34 d0` on reopening; all four retained runtime configuration 2. Removing the reader reproduced two failures in the next four cycles. With the production reader in place, the original failing-trace/read/CLOSE/reopen probe and six further ordinary cycles all closed successfully and remained in runtime mode. The injected trace failure still surfaced at final Close.

The transport now discovers the interrupt endpoint, starts its reader before UART configuration, records notifications separately from bulk UART data, and keeps reading through CLOSE even if an operation context or trace output fails. It cancels and joins the reader before releasing USB handles. Trace writes are serialized with bulk/control records, and unexpected interrupt-read failures remain visible at Close. This change adds host reads; the existing UART control requests are unchanged. Linux hardware, other firmware images, and occupied bicycle service-session cleanup still need separate validation.

## E-Tube serial framing

**Recovered:** decoded body is `control, payload..., FCS`. The FCS makes the additive sum of every decoded body byte zero modulo 256. Compute it over control and payload before escaping.

Escape `bb` and `bd` as `bd` followed by the original byte XOR `20`. Other bytes remain unchanged. The delimiter is `bb`.

```text
Initial/adapter probe frame: bb, escaped body, bb
Connected legacy frame:     escaped body, bb
```

The application omits the leading delimiter after its connected flag becomes true for legacy adapters; it always emits the final delimiter. Its decoder accepts either representation. Use a bounded streaming decoder which retains incomplete escape pairs, tolerates USB packet splits, verifies FCS, and rejects malformed frames. Do not require every USB transfer to end at a frame boundary.

Adapter request control normally receives `request | 20` as response control. Unit commands use outer control `48` in both directions. Received `48` includes an extra sender/slot byte before group and command; the official application's lower layer strips it. Retaining that byte in an independent client allows stricter correlation than the official cache.

## Adapter queries and operating mode

**Recovered:** all numbers below are hexadecimal unless stated otherwise.

| Operation | Control | Payload | Response |
| --- | --- | --- | --- |
| Protocol state / adapter link | `04` | empty | `24`, at least 1 byte |
| Adapter firmware version | `05` | empty | `25`, at least 3 bytes |
| Set protocol | `03` | 1 byte | `23`, followed by state query |
| Set target slot | `06` | target slot | `26`, byte 0 = `00` for success |
| Occupied slots in adapter-master mode | `1b` | empty | `3b`, four bitmap bytes |

Exact initial link/version query frames are `bb 04 fc bb` and `bb 05 fb bb`. The protocol-state reply byte has ready bit `80`, update/DCAS bit `40`, master/slave bit `20`, and low five bits equal to the PC application's assigned slot. DCAS-A is `00`; UPDATE is `40`; slave is `00`; master is `20`.

The live initial state was `00`: it proves adapter communication, but the ready bit is clear. A caller must initialize the protocol before expecting a usable PC slot and bicycle session; it must not interpret an idle link reply as ready/DCAS/slave merely because its mode bits are zero.

Probe the DCAS-A role with control `03`, payload `00`. The recovered `CheckSMPCE1RunMode` method interprets response `23` payload `00` as slave and payload `10` as adapter master. This acknowledgment is distinct from the protocol-state byte: `10` here is a probe result, while state bit `20` denotes master. For adapter master, explicitly select control `03`, payload `20`; then wait 1 second and check control `04` for ready DCAS-A/master. For slave, wait 1 second and check ready DCAS-A/slave. A system whose battery supplies the master follows the slave route. Do not assume that role from the bicycle model alone. UPDATE/slave additionally ORs in the PC slot; that path is for firmware operations and is not needed for settings reads.

The latest live exchange was:

| Purpose | Outgoing frame | Incoming frame |
| --- | --- | --- |
| Probe role | `bb 03 00 fd bb` | `bb 23 10 cd bb` |
| Select adapter master | `bb 03 20 dd bb` | `bb 23 00 dd bb` |
| Read ready state | `bb 04 fc bb` | `bb 24 a0 3c bb` |
| Read occupied slots | `bb 1b e5 bb` | `bb 3b 00 00 00 00 c5 bb` |

No unit-control `48` commands were sent in that run because there were no occupied component slots.

Firmware reply bytes encode major in byte 0's high nibble, minor in its low nibble, subminor in byte 1, and revision in byte 2. The archived application requires SM-BCR2 firmware at least 3.0.0 before normal connection. USB `bcdDevice` is not this application firmware version.

The application also has adapter reset `10`, update-mode `0e`, power-management `1a`, and PnP retry `1d` commands. They are not required for a first read-only adapter probe; resetting SM-BCR2 can cause USB disconnect/reconnect. Avoid copying a generic SM-PCE1 startup sequence without accounting for those differences.

## Bicycle commands and service session

**Recovered:** send adapter target selection for a slot before sending that slot's unit request. The official application caches the successfully selected target and invalidates that cache on mode changes.

```text
Request body:  48, target slot, group, command, parameters..., FCS
Reply body:    48, sender slot, group, reply command, parameters..., FCS
```

Normal unit reply command is `requestCommand | 02`; errors set its low bit as well (`requestCommand | 03`). The official cache matches group and `command & fe`, then distinguishes success/error. Preserve error parameters, typically a little-endian error code. Match sender slot too, serialize requests, and keep unsolicited packets separate from command replies. Default request timeout is 3 seconds, with two retries; settings writes should not automatically retry after an ambiguous timeout.

### Application link preparation

The recovered `InitDataLink` method opens the serial port, clears the connection flag/cache, and calls `SendResetAndPowerLimitUnlock` before normal discovery. Reset is adapter control `10`, empty payload, expecting control `30` and first response byte `00`. For SM-BCR2, the wrapper waits for reconnection and sends reset again if its wait succeeds, even if the first reset failed. The wait may succeed when the old serial port remains open, so an observed disconnect is not required for that second reset.

After successful reset, wait 1 second, then send power-limit unlock: control `1a`, payload `01`, response control `3a`, at least two reply bytes, with byte 1 equal to `00`. This is adapter preparation, not a stored paddle setting. It can cause USB re-enumeration; Windows' driver restores controller RAM automatically, while a native client must reopen and restore the supplied image if boot mode reappears. The recovered Windows reconnect search checks an open port for up to 3 seconds, then searches for a matching VID/PID for up to 10 seconds after closure.

This preparation was identified after the initial bicycle-mode timeout. Adding it allowed the later live run to reach ready master mode and read the empty slot bitmap. Both resets returned `30/00`; power unlock returned `3a/01 00`. A first adapter-info probe works without this preparation. The native `Prepare` step follows the same physical USB port through re-enumeration and reopens/reloads the recognized controller image if necessary. Recovery experiments must distinguish an adapter application reset from a host USB reset and keep their traces separate.

Normal bicycle discovery also needs the recovered application's power-detection phase. For SM-BCR2 firmware 3.0.0 and later, wait 3 seconds after initial preparation, send `1a/01`, then query `1a/02`. The second response byte is the status: `01` is ready, `02` requires supply start `1a/03`, `03` and `04` indicate charging states, and `05` is busy. Operations `01` and `03` require at least two response bytes with byte 1 zero. The recovered attempts have a 5-second deadline and at least 2 seconds between starts, with three attempts normally and up to 31 after busy status. Wait 1 second after the power phase before the `03/00` role probe. If SM-BCR2 reports master, repeat reset, unlock, and this power phase once. `PrepareBicycle` follows this sequence and requires the OEM's normal-connection adapter minimum of 3.0.0; this is separate from component firmware compatibility.

The live 3.0.1 adapter returned `3a/02 02`, accepted supply start with `3a/03 00`, and still reported an empty bitmap on that probe. Thus omitting supply start is a real preparation gap, but its presence alone does not prove why components were absent. End component service sessions first, then send adapter reset `10` with a fresh bounded cleanup context, and finally close the UART and USB resources. The live final reset returned `30/00` and USB close succeeded. These power and reset operations change transient adapter state, not stored paddle assignments.

### Discovery and session order

After link preparation and role selection, the recovered application has two discovery routes. The implementation handles both:

- **Adapter master:** check ready DCAS-A/master, wait 2 seconds, and obtain the four-byte bitmap from adapter control `1b`. Enter PC communication mode separately for each occupied component slot before reading it. Exclude the PC application's slot. This route reached the empty bitmap in the latest live run; its component service sessions remain unverified.
- **Battery master / adapter slave:** check ready DCAS-A/slave, select target slot `00`, wait 500 ms, and read the six-byte unit slot result using group `01`, command `14`. Enter PC communication mode with master slot `00`, then read master information and occupied components, excluding the PC application's slot. This route remains unverified live.

Read optional serial numbers and model-specific settings only after discovery and service startup. End every started PC communication session during cleanup before closing the transport. An empty bitmap is a discovery result; it is not successful component or paddle validation.

The PC service session is transient and changes how the bicycle communicates while connected. It is required before normal configuration access even if no persistent settings are being written. Cleanup must be attempted on success, error, interruption, and a partially completed start.

Start sends group `32`, command `10`, parameters `01 pcSlot 00 00` without waiting for its reply. Wait 1 second with a battery, then send group `32`, command `30` five times, each with four parameters:

```text
a2 2b 00 00
30 0e 00 00
7a 4d 00 00
62 2b 00 00
85 b4 00 00
```

Wait 100 ms between those five packets. Cache responses before sending: the final confirmation is group `32`, command `12`, corresponding to the original communication-mode command. A separate `32` reply is not the session success criterion. The official application retries the full start sequence up to three attempts.

End sends group `32`, command `10`, parameters `00 pcSlot 00 00`; wait for group `32`, command `12`. The official stop path explicitly reselects the target, uses a 300 ms timeout per attempt, and allows up to three attempts. Avoid entering update mode or running firmware commands during discovery.

In slave mode, the recovered `EndPCConnectModeForm.SendAllPCConnectModeEndCommand` sends OFF to each occupied non-PC slot 1–30, then slot 0 last. Starting the master component's service session therefore requires tracking those occupied components for cleanup, including interrupted startup. Adapter-master sessions end every individually started slot. Diagnostic trace failures must not interrupt either teardown path.

START and END acknowledgments both use group `32`, command `12`, without an evidenced sequence identifier or required mode echo. The OEM clears matching cached replies before sending. A delayed START reply could still be mistaken for END confirmation if it arrives after target selection. This timing remains unverified on occupied hardware; a successful synthetic cleanup test does not establish that the bicycle has left service mode after an interrupted start. Disconnect recovery remains necessary when service state is uncertain.

### Read-only unit queries

| Query | Group | Command | Request parameters | Minimum response parameters |
| --- | --- | --- | --- | --- |
| Connected slot bitmap, master slot 0 | `01` | `14` | `00` | 6; bitmap is bytes 2..5, little bit order, 32 slots |
| Stock identity | `01` | `1c` | `00` | 2; series, unit |
| Shifter/switch part identity | `04` | `0c` | `00` | 1; part |
| Other unit part identity, when model defines a part | `32` | `9c` | `00` | 1; part |
| Firmware version | `01` | `2c` | `00` | 3; same encoding as adapter version |
| Unit serial | `01` | `3c` | `00` | 6; preserve raw until format validated |
| Genre flags | `01` | `0c` | `00` | 4; little-endian bitmap |
| Key function flags | `01` | `24` | `00` | 4; little-endian bitmap |
| Required application version | `32` | `b4` | `00` | 3; major, minor, patch |
| Switch type, only for models with switch-type identity | `04` | `1c` | `00` | 1 |

Do not assume the brand/model/year of the bicycle proves which components are fitted. Preserve unknown series/unit/part values, use raw identities in output, and only enable model-specific configuration for identified compatible components.

## Paddle assignment

**Recovered:** group `04`, command `14` (`ST_CND_GET`) reads assignments. Two-channel models send one zero parameter; models with C/S channels, including dummy channels, send four zeros. Decode byte 0 high nibble as A, low nibble as B; byte 1 high nibble as C, low nibble as S.

The GET request length is not the minimum reply length. The recovered loader accepts at least one byte and decodes C/S when a second byte is present. For the four-channel models below, this implementation requires two reply bytes so every channel is available to preserve; it retains any trailing reply bytes as raw diagnostics.

Writing uses group `04`, command `10` (`ST_CND_SET`):

```text
Two-channel form:  (A << 4) | B
Four-channel form: (A << 4) | B, (C << 4) | S, 00, 00
```

Model definitions determine the form, not physical button count. Read first, preserve every unselected channel, reject out-of-range values, show an explicit before/after change, send once, and verify by a new read. A timeout after a write leaves the result uncertain until a read establishes the current assignment.

| Condition | Meaning in the common road-shifter definition |
| --- | --- |
| `0` | Front shift up |
| `1` | Front shift down |
| `2` | Rear shift up |
| `3` | Rear shift down |
| `f` | Unassigned / no function where supported |

Other values depend on component and firmware. For example R8050 definitions list cycle-computer functions 4/5, while GRX RX815 definitions use 4/5 for display/light actions and 6/7 for assist. D-Fly functions depend on master, junction, and shifter firmware support. Do not label arbitrary 4..e values with one universal mapping. Even `f` must be restricted to models/buttons supporting no function.

| Model | Series/unit/part (hex) | Channel mapping | Assignment form |
| --- | --- | --- | --- |
| ST-R785-L/R | `05/01/01`, `05/01/02` | X=A, Y=B; dummy C and S | Four bytes |
| ST-6870-L/R | `07/01/01`, `07/01/02` | X=A, Y=B, sprinter=C, non-customizable cycle-computer channel=S | Four bytes |
| ST-R8050-L/R | `14/01/01`, `14/01/02` | X=A, Y=B, hood button=S, sprinter=C | Four bytes |
| ST-R8070-L/R | `14/01/0d`, `14/01/0c` | X=A, Y=B, hood button=S, dummy C | Four bytes |
| ST-RX815-L/R | `16/01/0d`, `16/01/0c` | X=A, Y=B, hood button=S, dummy C | Four bytes |
| ST-R9150-L/R | `11/01/01`, `11/01/02` | X=A, Y=B, hood button=S, sprinter=C | Four bytes |
| ST-R9170-L/R | `11/01/0d`, `11/01/0c` | X=A, Y=B, hood button=S, dummy C | Four bytes |

Default road assignments are left X=0/Y=1 and right X=3/Y=2. They are defaults, not proof of current settings. Physical X/Y terminology comes from Shimano's application; a CLI should avoid guessing the rider's preferred paddle names.

Other relevant identity examples: SM-BTR2 `05/00`, BT-DN110 `11/00`, FD-6870 `07/03`, RD-6870 `07/04` (part 0 SS/1 GS), FD-R8050 `14/03`, RD-R8050 `14/04` (part 0 SS/1 GS/3 RX805-GS), FD-RX815 `16/03/00`, RD-RX815 `16/04/04`, RD-RX817 `16/04/01`, EW-WU111 `10/08`, EW-WU101 `11/08`. These are recovered lookup facts, not a complete compatibility matrix.

### Preview, apply, and recovery

**Recovered:** `SwitchFunctionalUnit.SetSwitchCondition` preserves unspecified channels and sends one SET command. `UnitCommandSetting` permits a success reply with zero parameter bytes: outgoing `48 slot 04 10 AB CS 00 00` expects incoming `48 slot 04 12`; `04/13` is an error reply. The application UI's `Customize.SwitchSetProgressPanel` calls that setter and then collects log data. There is no separate save/commit command or reset in that apply sequence. These are observations of the fingerprinted E-Tube Project 3.4.5 assemblies, not live persistence verification.

**Implemented restriction:** editing is limited to the identified models in the table, with a successfully read firmware version and current assignments. The inventory must contain an identified SM-BTR2 or BT-DN110 controller; an additional unknown controller with unit number `00` disables editing. The controller may occupy a nonzero slot when the adapter is master. This establishes a conventional battery-controller context without treating an unknown drive system as a road bicycle. It is an application support boundary, not a claim that other systems cannot use SET.

Editing also excludes slot 31. The recovered slave cleanup explicitly visits occupied slots 1–30 and then slot 0, separately excluding the observed PC slot. This establishes the implemented cleanup range; it does not establish that slot 31 is always reserved for the PC. Discovery may report that slot, but writing it requires separate service-cleanup evidence.

New X/Y assignments are restricted to values `0`–`3`; an unchanged X/Y value anywhere in `0`–`f` is preserved. C/S are preserved, including dummy and non-customizable channels. OEM condition patterns S1/S15 support the four basic shift functions in conventional Di2 operation. No separate minimum firmware version was found for those basic functions; the recovered `3.1.0` shifter threshold concerns D-Fly, which has additional system checks and is not offered here.

`bike paddles plan --slot N --a ACTION --b ACTION` reads the bicycle and emits a version 1 JSON preview; either action may be omitted to preserve that paddle. The app can build the same preview from its most recent inspection. `bike paddles apply --plan FILE` validates bounded JSON before opening USB, pins the observed physical bus/port path through adapter preparation, re-reads every requested shifter, and checks identity, firmware, and assignments before any SET. A changed precondition requires a new preview. Comparison uses the first two GET reply bytes: the OEM ignores trailing GET bytes and explicitly sends zeros in the corresponding SET positions. USB addresses may change during controller initialization, so they are informational rather than the plan's physical identity.

An unchanged preview sends no SET. Each changed shifter receives at most one SET, followed by an independent bounded GET even if the caller was cancelled or the acknowledgment was lost. Matching readback verifies the requested assignments and retains any ambiguous write error as a warning; original readback establishes unchanged settings; a mismatch or unavailable readback prevents a success claim. Multi-shifter changes are sequential, not atomic. Processing stops on failure and reports already verified changes separately. Service cleanup, final adapter reset, and USB-close failures remain visible and prevent a global `verified` result.

To restore previous assignments, read the bicycle again and create a new preview using the saved pre-change values. Do not automatically replay a timed-out SET or blindly roll back another shifter. If an original model-specific value cannot be selected by this restricted editor, preserve the saved raw report and use the appropriate supported configuration tool for that function. A failed service cleanup still requires disconnect recovery. Actual component writes and persistence after a full disconnect/reconnect require live validation; fixture tests cannot establish them.

## Additional recovered capabilities

The application exposes far more than paddles. The following command pairs are useful next steps; support and valid ranges vary by unit/firmware, so they are a research catalog rather than authorization to execute writes.

| Capability | Group / read command | Read parameters / response | Write command and parameters |
| --- | --- | --- | --- |
| Battery state-of-charge level | `26/14` | `00`; 1 raw level byte | None |
| Alternate battery residual value | `26/44` | `00`; 1 raw byte | None |
| Rear multi-shift pattern | `0b/54` | `00`; 1 byte | `50`; `[pattern,00,00,00]`, echoed pattern |
| Multi-shift up/down limits, battery/master helper | `0b/64` | `00`; 2 bytes up/down | `60`; `[up,down,00,00]`, echoed up/down |
| Rear multi-shift switch enable | `0b/6c` | `00`; 1 byte | `68`; `[value,00,00,00]`, echoed value |
| FD/RD adjustment | FD `0a/14`, RD `0b/14` | `00`; current and maximum, signed bytes | `10`; `[target,max,00,00]`, echoed target; may move a derailleur |
| FD/RD adjustment bounds | FD `0a/24`, RD `0b/24` | `00`; signed minimum/maximum | None |
| Synchronized-shift interval | `0a/9c` | `00`; 1 byte | `98`; `[interval]`, echoed interval |
| Synchronized-shift mapping | `32/ac` | `[mode OR direction,frontPosition,00,00]`; 4 bytes | `a8`; same selector plus rear/target positions; echoed 4 bytes |
| Front/rear tooth-pattern IDs | FD `0a/b4`, RD `0b/b4` | `00`; 2 nibbles, combined `p0 OR p1<<4` | `b0`; `[pattern&0f,pattern>>4]` |
| Sensor error state | `32/74` | `00`; 1 byte | None |
| Movement-time record | `01/74` | `00`; 2 bytes | None |
| Sensor A/D values | `01/44` | `00`; 4 bytes | None |
| Failure history | `32/cc` | `[recordIndex]`; echoed index then record data | None |

A value named SOC/voltage level in the application is not necessarily a percentage. Multi-shift pattern enums, synchronization selector bits, gear tooth-pattern lookup tables, diagnostics layouts, and exact compatibility/version limits require further extraction and live read checks. A firmware updater additionally needs validated image formats, target identity checks, integrity checks, rollback/recovery procedures, and interruption testing. It must not be inferred from the existence of a transport or command names.

## Boundaries and remaining work

- TI USB startup, interrupt handling, and adapter queries succeeded live on macOS with the recognized Shimano bridge image. Additional host platforms and exceptional disconnect behavior remain unverified.
- Mac app, CLI, and bundled USB-library builds target macOS Tahoe 26 or later, on Apple silicon and Intel.
- OEM link preparation and adapter-master mode selection also succeeded live, followed by a zero component bitmap. The connection's physical state and reason for missing units are unconfirmed. Both role routes are implemented; neither has produced a genuine component or paddle read yet.
- Framing, service mode, discovery, identity queries, and paddle formats are directly recovered from one archived application version. They do not establish support for every Di2 generation or firmware release.
- The installer and firmware are proprietary. Keep user-supplied firmware outside version control, validate the recognized hash, and do not commit extracted DLLs, decompiled source, installers, or bridge images.
- An adapter firmware query is a first milestone. Bicycle enumeration is a separate milestone. Verified paddle reads are another. Settings writes require before/after readback on the actual identified component.
- Preserve traces of both USB setup and decoded serial traffic, with direction and timing, so errors can be investigated without guessing new commands. Built-in traces exclude bridge firmware-upload payloads. Review external captures before publishing: a complete boot trace reconstructs the proprietary image.
- Bus-active reset, firmware-update commands, derailleur motion, and diagnostics that change configuration are outside read-only discovery.
