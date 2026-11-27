# Changelog

## v3 - unreleased

Host actions and release identity. Version 2's adaptive monitoring is preserved
unchanged.

### Added

* **Help > About** dialog with the application icon, a clickable
  `https://github.com/tamzrod/lanmonitor` link, the MIT licence and the exact
  quote `Gwapong Pilipino Taas Noo Kahit Kanino!`.
* **Support Development** section in About with the exact message
  `This software is completely free. If you find it useful, consider supporting its development.`
  and Maya and GCash donation QR images side by side. The QR codes are embedded
  and displayed with their quiet zones; Bitcoin support is deferred.
* Embedded retro network icon used by Windows Explorer, the title bar, the
  taskbar, Alt+Tab and the About dialog. The icon is compiled into the executable
  through `rsrc_windows_amd64.syso`.
* Original WebP and ICO assets under `assets/`, with tests that pin their
  SHA256 checksums and decode the displayed QR codes at multiple DPI sizes.
* Right-click menu on every valid IP cell, regardless of ping status: HTTP,
  HTTPS, SSH, Telnet, RDP, SMB, Custom Port, Continuous Ping and Copy IP
  Address.
* **Custom Port** dialog for HTTP, HTTPS or SSH on a validated port from 1 to
  65535.
* Editable network ComboBox with automatic `/24` detection, a **Refresh** button
  and manual CIDR entry.
* Adapter detection includes physical Ethernet, Wi-Fi and VPN adapters such as
  WireGuard, OpenVPN and Tailscale.
* Validated IPv4 and port parsing, command construction tests, and graceful
  messages when a client or default browser is unavailable.
* `AUDIT.md`, describing the Win32 API, unsafe-pointer and network-selector
  review.

### Changed

* The window title carries the version: `LAN Monitor — Version 3`.
* Client commands pass each argument directly to the executable. No command
  shell or `cmd.exe` is used.
* The window accounts for the new Help menu when sizing the client area.
* The network input is an editable ComboBox instead of a disabled subnet box. It
  stays editable during discovery and monitoring.
* The About dialog is larger and lays out the icon, release identity, quote,
  Support Development message and two QR images side by side. All layout units
  scale with the system DPI.
* The WebP decoder and QR images are compiled into the executable. No external
  image file, installer or runtime is required.

### Fixed

* Replaced the locked network input with an editable ComboBox.
* Automatic detection lists active physical Ethernet, Wi-Fi and VPN `/24`
  networks, including WireGuard, OpenVPN and Tailscale.
* Docker, WSL, Hyper-V virtual switches, VMware, VirtualBox, loopback, Wi-Fi
  Direct, hosted-network and common container adapters are excluded by default.
* **Refresh** rebuilds the network list without clearing a manually typed CIDR.
* Selecting a network while monitoring starts a sweep; manual CIDR entry is used
  by the next **Scan** or 15-second sweep.
* Selecting a ComboBox entry no longer leaves the edit field blank. The chosen
  CIDR is captured from the list at `CBN_SELENDOK` and committed independently
  of the transient edit text.
* **Scan**, **Monitor mode**, the 15-second sweep, the hover status and the
  context menu all use one authoritative committed subnet instead of reading the
  ComboBox text. A selected entry, a manual entry and a refresh cannot disagree
  about the target.
* A subnet change cancels rounds in flight, invalidates their scan session and
  clears the stale host map. Results from an old subnet, including after an
  A-to-B-to-A switch, are discarded.
* Cancelling the ComboBox drop-down after a selection keeps the selected CIDR;
  cancelling without a selection restores the pre-drop text, so a manual CIDR is
  preserved.
* **Refresh** rebuilds the network list without changing the committed subnet,
  its session or the host map, and still preserves a manually typed CIDR.
* Adapter detection performs no ICMP and preserves startup silence.

### Release correction

* The v3 prerelease was republished from the corrected network-selector commit.
* Original v3 prerelease commit:
  `5d9cd8ebec408566f9dce20189dfabde7bf1f0af`.
* Original `lanmonitor-v3.exe` SHA256:
  `5411833641483be162ab457eeecced5331ddc0ee14e46fd0e90f0582858a0d90`.
* First network-selector correction commit:
  `41e169bfbc73389ee33e6799c424c6a9bd55193c`.
* First correction `lanmonitor-v3.exe` SHA256:
  `f89559c1fde94d10519414f919df0f674ac4d7e9d92ea60a90939af9f843e536`.
* ComboBox notification correction commit:
  `561a6eff961dda5d4a3695885b0429007c440cce`.
* ComboBox notification correction `lanmonitor-v3.exe` SHA256:
  `302bb13e32de8c298460e5cfaff5b210c24001f49b28893e6db69ca445b42e33`.
* Committed-subnet correction: `subnet.go` now owns the authoritative subnet and
  the host map. The reported failure (a selected entry displayed but **Scan** or
  **Monitor mode** reverting to the previous subnet) is fixed, including when
  `CBN_SELENDCANCEL` arrives after the selection. Regression tests cover
  A-to-B-to-A switching, manual entry, **Scan**, **Monitor**, **Refresh**,
  drop-down cancellation and late old-subnet results. The `v3` tag and both
  assets are replaced; the current commit and checksum are in the v3 release
  notes.
* Assets correction: the uploaded WebP cards and ICO were moved into `assets/`
  with their bytes unchanged, the icon was embedded in the executable and the
  About dialog gained the Support Development section and both donation QR
  images. The previous `lanmonitor-v3.exe` SHA256 was
  `abf16250f48f2cf3b201f754d93eec60b9c49b51d96bd2e00a820810bbcae793`; the
  replacement commit and checksum are recorded in the v3 release notes. The
  `v3` tag is moved to the assets correction and both assets are replaced.
* No v4 tag or release was created; v3 remains a pre-release pending Windows
  acceptance.

### Security

* IP addresses are validated as unicast IPv4 before use and are never
  interpolated into a shell command line.
* The only URL launch uses `ShellExecuteW` with a fixed `open` verb and a
  validated URL.
* The project contains no traceroute, automatic port scanning, payment
  processing, tracking or automatic network request. The About dialog contains
  static Maya and GCash donation QR images; they do not process payments.

## v2

Adaptive monitoring. A full `/24` sweep every 15 seconds discovers hosts; hosts
that answer are then probed every second until two sweeps in a row miss them.

### Added

* **Monitor mode** check box that enables adaptive monitoring.
* Promotion on a discovery reply and demotion after two consecutive discovery
  misses, with automatic recovery on the next sweep that reaches the host.
* A `Suspect` state (orange) for a promoted host that missed its most recent
  probe, so a single dropped echo is visible without dropping the host.
* `ADAPTIVE.md`, describing the cadence, the state machine, concurrency,
  cancellation and stale-result handling.

### Changed

* The window title carries the version: `LAN Monitor — Version 2`.
* One round runs at a time per kind. A sweep requested during a sweep is queued
  once instead of being dropped or run in parallel.
* Status line reports the monitoring phase, queued sweeps and ICMP handle
  failures.

### Fixed

* **Stop** no longer lets a queued sweep start afterwards.
* Cancelling a round no longer clears the cancellation state for rounds that are
  still draining, so probes stop promptly instead of resuming after **Stop**.
* Results from a round started on a previous subnet are discarded instead of
  being written into the new map.
* A stale round completion can no longer clear the busy flag of the round that
  replaced it.
* An address demoted during a round is no longer updated by that round.
* A sweep in which every probe failed to obtain an ICMP handle is reported as a
  failure instead of looking like "no replies".

### Removed

* The fixed polling interval box; the adaptive schedule replaces it.

## v0.1.0

Initial portable release.

* Single portable Windows AMD64 executable, native Win32 GUI, no runtime
  dependencies.
* Manual **Scan** and **Stop**, fixed map of `.1`–`.254`, nothing scanned at
  startup, continuous polling off by default.
* Win32 fixes: string arguments stay reachable for the duration of each call,
  hit testing is bounds-checked, `IcmpSendEcho` sends a real payload, layout
  scales with the system DPI.
* Platform-independent logic split out with unit tests, plus Win32 ABI tests that
  run on Windows.
* GitHub Actions build and test workflow, `BUILD.md` verification and acceptance
  instructions.
