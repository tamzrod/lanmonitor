# Source and Win32 audit — Version 3

## Scope

This audit covers the Version 3 source on `feature/version-3`, including the
Windows-specific `main_windows.go`, the platform-independent `logic.go`,
`networks.go`, `subnet.go` and `qr.go`, the asset embedding and icon resource,
the tests, the CI workflow and the release artifact checks. It also covers the
network-selector correction, the committed-subnet correction and the asset
integration that republished the v3 prerelease.
No Windows runtime or GUI acceptance was performed for Version 3; that is the
purpose of the pre-release.

## Release source reconciliation

* The Version 2 release tag `v2` resolves to commit `80d2e82` on
  `feature/adaptive-monitoring`.
* The open Version 2 pull request (#2) was marked ready and merged into `main`
  as `ef633d3`, so `main` now has the same tree as the `v2` release source.
* `feature/version-3` merged that source before any Version 3 change.
* The adaptive monitoring code is preserved: `hostRecord.observe`, `targets`,
  the 15 s discovery timer, the 1 s active timer, the two-miss demotion rule,
  the cancellation token and the Stop path are unchanged apart from the version
  constant. Their tests still pass.
* The original v3 prerelease commit was
  `5d9cd8ebec408566f9dce20189dfabde7bf1f0af`; its `lanmonitor-v3.exe` SHA256 was
  `5411833641483be162ab457eeecced5331ddc0ee14e46fd0e90f0582858a0d90`.
* The first network-selector correction was commit
  `41e169bfbc73389ee33e6799c424c6a9bd55193c`; its `lanmonitor-v3.exe` SHA256 was
  `f89559c1fde94d10519414f919df0f674ac4d7e9d92ea60a90939af9f843e536`.
  The ComboBox notification fix republishes the same `v3` tag; no `v4` tag or
  release was created.

## Win32 API inventory

| DLL | Entry points used | Purpose |
| --- | --- | --- |
| `kernel32.dll` | `GetModuleHandleW`, `GlobalAlloc`, `GlobalLock`, `GlobalUnlock`, `GlobalFree`, `RtlMoveMemory` | module handle and clipboard memory |
| `user32.dll` | window, menu, dialog, message, painting, clipboard, timer, `LoadImageW` and `LoadIconW` calls | native GUI and icon loading |
| `gdi32.dll` | `CreateFontW`, `CreateSolidBrush`, `SelectObject`, `SetBkMode`, `SetTextColor`, `CreateDIBSection`, `DeleteObject` | font, GDI painting and QR bitmaps |
| `iphlpapi.dll` | `IcmpCreateFile`, `IcmpSendEcho`, `IcmpCloseHandle` | ICMP echo |
| `shell32.dll` | `ShellExecuteW` | open a validated HTTP/HTTPS URL with its registered handler |
| `comctl32.dll` | `InitCommonControlsEx` | register the native SysLink control |

All are Windows system DLLs. The built executable's import table contains only
`kernel32.dll`; the other DLLs are resolved at runtime through `NewLazyDLL`.
There is no third-party runtime, installer or database.

The network selector is a standard `COMBOBOX` control created with
`CreateWindowExW`. It uses the existing `user32.dll` message calls
(`CB_ADDSTRING`, `CB_RESETCONTENT`, `CB_GETCURSEL`, `SetWindowTextW`), so it adds
no DLL entry point and no new ABI structure.

## unsafe.Pointer review

* The structs passed to Win32 by pointer are ABI-tested: `rect`, `point`, `msg`,
  `wndClass`, `paintStruct`, `icmpEchoReply`, `nmhdr` and
  `initCommonControlsEx`.
* String arguments are converted to NUL-terminated UTF-16 buffers in the `call`
  helper, pinned for the duration of the call, and kept alive with
  `runtime.KeepAlive`.
* `ptrFromUintptr` recovers the `WM_NOTIFY` `LPARAM` that Windows supplies and
  the pixel pointer returned by `CreateDIBSection`. The `WM_NOTIFY` pointer is
  valid only while that message is handled. The DIB pointer is owned by the
  bitmap handle and is copied immediately into a Go slice of exactly
  `width*height*4` bytes; `runtime.KeepAlive` keeps the `BITMAPINFO` reachable
  across the call.
* The icon and bitmap handles are opaque Windows handles. Shared icons loaded
  with `LR_SHARED` are not destroyed. The two About bitmaps are DIB sections and
  are deleted with `DeleteObject` in `WM_DESTROY`. No Go pointer is stored in a
  GDI object or in a window property.
* Clipboard text is copied with `GlobalAlloc` plus `RtlMoveMemory`; no Go pointer
  is stored in the clipboard allocation.
* The ICMP reply is parsed from a fixed 128-byte stack buffer after
  `IcmpSendEcho` reports a reply.
* No pointer arithmetic is performed on an untrusted length. Buffer sizes are
  either fixed constants or the length of a Go slice.

## Command construction and injection review

* `validateIPv4` trims whitespace, rejects `/` and `:`, parses with
  `net.ParseIP`, requires an IPv4 result, rejects `0.0.0.0`, broadcast and
  multicast, and returns the canonical dotted-quad form. Table tests cover valid
  and invalid inputs.
* `hostAddress` accepts only a parsed IPv4 `/24` and a host index from 1 to 254,
  then validates the constructed address.
* `parsePort` accepts digits only and enforces 1–65535. Tests cover `0`, `65536`,
  negatives, signs, decimals and non-numeric input.
* `normalizeScheme` accepts only `http`, `https` and `ssh`.
* `buildClientCommand` and `buildCustomPortCommand` put the validated address in
  a URL or in one argument of an argument slice. No command line is assembled by
  string concatenation.
* `launch` resolves an executable with `exec.LookPath` and starts it with
  `exec.Command(path, args...)`. No `cmd.exe`, `powershell`, `/c` or shell
  interpreter is used. `ShellExecuteW` is used only for HTTP/HTTPS URLs with the
  fixed `open` verb.
* `TestCommandsHaveNoShellSyntax` checks every menu action for `cmd`, `/c`, `&`,
  `|`, `;`, backtick, `$`, `>`, `<`, CR and LF in the target or arguments.
* A missing executable or a failed `ShellExecuteW` produces a message box with a
  client-specific message. The UI does not fail silently.

## Context menu and About review

* `WM_CONTEXTMENU` uses `GetCursorPos`, `ScreenToClient` and `hostAt`. It does
  not inspect the ping state, so the menu is available on every valid cell.
* `TestContextMenuContract` pins the exact order and labels: HTTP, HTTPS, SSH,
  Telnet, RDP, SMB, Custom Port, Continuous Ping, Copy IP Address. It also
  rejects traceroute, port-scan, donation, payment and QR wording.
* **Help > About** is a native owned window with a SysLink control. A click sends
  `WM_NOTIFY`; the handler opens the fixed project URL. If SysLink is not
  available, a native button with the same URL is used.
* `projectURL`, `aboutQuote` and `supportMessage` are exact constants with tests.
  The quote is `Gwapong Pilipino Taas Noo Kahit Kanino!`; the support message is
  `This software is completely free. If you find it useful, consider supporting its development.`
* The About window shows the application icon in a static icon control and sets
  `WM_SETICON` for the title bar. The icon is loaded from `RT_GROUP_ICON`
  resource 1, which `cmd/verify-pe` checks in the built executable.
* The Maya and GCash WebP cards are embedded with `//go:embed`. At dialog open,
  `decodeQRImage` decodes the original bytes, crops the full QR code plus its
  white quiet zone, composites over white and scales with `ApproxBiLinear` to
  the DPI-scaled display size. The original files are not modified and the QR
  codes are not regenerated.
* The scaled images are converted to top-down 32-bit DIB sections and assigned
  to `SS_BITMAP` static controls with `STM_SETIMAGE`. The two bitmap handles are
  deleted on `WM_DESTROY`; static controls do not own their bitmaps. If decoding
  or DIB creation fails, the dialog shows a text fallback instead of crashing.
* The dialog uses `px()` for every position and size, and scales the About icon
  and QR bitmaps with the same system DPI. No external image file is read at
  runtime.
* The QR codes are static donation images. There is no payment processing,
  tracking, network request or Bitcoin integration.
* Custom Port is a native owned window with a protocol combo box, a numeric edit
  and OK/Cancel. The port is validated before the dialog closes, and the scheme
  is restricted to HTTP, HTTPS and SSH.

## Network selector review

* The old subnet edit was replaced with an editable `COMBOBOX`
  (`CBS_DROPDOWN`), so manual CIDR entry remains possible. The control is never
  disabled during discovery or monitoring; the previous `EnableWindow` calls
  were removed.
* `detectedNetworks` calls `net.Interfaces` and `Interface.Addrs`, which are
  local adapter enumeration APIs. It sends no ICMP and performs no name
  resolution, so startup silence is preserved.
* `candidateSubnet` derives the `/24` containing each adapter address rather
  than requiring the adapter mask to be `/24`. This is necessary for Tailscale,
  WireGuard and other VPN adapters that expose `/32` addresses. It rejects
  `0.0.0.0/8`, loopback `127/8`, multicast and reserved first octets, and IPv6.
* `classifyAdapter` includes Ethernet, Wi-Fi and VPN adapters. VPN matching covers
  WireGuard (`wireguard`, `wg*`), OpenVPN (`openvpn`, `tap*`), Tailscale,
  ZeroTier, Cloudflare WARP, generic `vpn`, `tun*`, `ppp*`, `utun*`, IPsec,
  L2TP, SSTP, AnyConnect, GlobalProtect and common vendor names.
* Exclusion matching covers Docker and container bridges (`docker`, `veth`,
  `br-`, `virbr`, Podman, CNI, Flannel, Calico), WSL, Hyper-V virtual switches
  (`hyper-v`, `hyperv`, `vethernet`, `default switch`), VMware (`vmware`,
  `vmnet`), VirtualBox (`virtualbox`, `vbox`), loopback and pseudo interfaces,
  Teredo, ISATAP, Npcap/WinPcap, Bluetooth, Wi-Fi Direct and hosted-network
  adapters. The loopback flag is also checked.
* Candidates are sorted Ethernet, Wi-Fi, VPN, Other, then by adapter name and
  CIDR. Duplicate CIDRs are removed after sorting, so a physical Ethernet entry
  wins over a VPN entry on the same `/24`.
* The ComboBox is populated with `CB_RESETCONTENT` and `CB_ADDSTRING`. The edit
  text is set with `SetWindowTextW`; `controlText` reads it with
  `GetWindowTextW`. **Refresh** captures and restores a manually typed CIDR.
* The editable ComboBox is input only. `subnet.go` owns the authoritative
  committed subnet (`subnetModel`) and the host map. **Scan**, the 15 s and 1 s
  timers, the hover status and the context menu read the committed value; no scan
  path parses the transient edit text.
* A drop-down choice captures its CIDR from `networkList` at `CBN_SELENDOK` and
  posts `wmComboCommit`. The commit uses that captured CIDR, not `CB_GETCURSEL`
  after native processing and not the edit text, so a control that is still
  rewriting its field cannot change the result.
* `CBN_DROPDOWN` records the pre-drop text and the current session.
  `wmComboRestore` restores the pre-drop text only when no selection was
  committed while the list was open; otherwise it shows the committed CIDR. This
  fixes the reported case where the list showed the new subnet but
  `CBN_SELENDCANCEL` restored the old text and **Scan** or **Monitor mode**
  reverted to the previous subnet.
* **Scan**, **Monitor mode** and any context-menu command call
  `commitPendingSubnet`, which parses the edit text (or maps a ComboBox label back
  to its CIDR) and commits it before doing any work. Invalid text is reported and
  leaves the committed subnet unchanged.
* The 15 s sweep calls `syncPendingSubnet`, which commits a valid manually typed
  CIDR; invalid or partial text is ignored and monitoring continues on the
  committed subnet.
* `subnetModel.Commit` increments the session and clears the host map only when
  the canonical subnet changes. Committing the same subnet is a no-op.
  `applySubnetChange` closes the cancellation token, invalidates both sequence
  numbers and clears the busy and queued flags, so an old round cannot write into
  the new map or clear the busy state of the round that replaced it.
* `subnetModel.Observe` applies a result only when the round's session matches
  and, for an active round, the host is still promoted.
* **Refresh** rebuilds the adapter list without committing, changing the session
  or touching the host map; it preserves pending text and maps a selected label
  to its CIDR. **Stop** remains the explicit cancellation path.
* Tests cover adapter classification, exclusion, `/24` derivation from `/24`,
  `/32` and shorter masks, ordering, duplicate preference, label text, the real
  `detectedNetworks` output and the committed subnet: A-to-B-to-A switching,
  manual entry, **Scan** and **Monitor** commits, **Refresh**, cancellation
  (including cancellation after a committed selection) and late old-subnet
  results. No shell or command construction is involved.
* Manual entry can still target a network excluded from the list. Exclusion is a
  default list policy, not a security boundary.

## Forbidden features

A source search found no traceroute, tracert, automatic port scan or payment
integration. Donation QR images are present only in the About dialog, as static
embedded assets; they do not process payments, track scans or make network
requests. The context-menu contract test still rejects donation, payment and QR
wording from the host menu. Manual **Scan**, startup silence, adaptive monitoring
and **Stop** are preserved.

## Static analysis and automated checks

The candidate was checked with:

```sh
gofmt -l .
go vet ./...
GOOS=windows GOARCH=amd64 go vet ./...
staticcheck ./...
GOOS=windows GOARCH=amd64 staticcheck ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go test -c -o /dev/null .
```

The binary was checked for PE32+ AMD64, the Windows GUI subsystem, a
`kernel32.dll`-only import table, `RT_GROUP_ICON` resource 1 with seven images,
size and SHA256. The corrections added tests for network classification, `/24`
derivation, filtering, ordering, duplicate preference, labels and the committed
subnet (A-to-B-to-A switching, manual entry, **Scan** and **Monitor** commits,
**Refresh**, cancellation and late old-subnet results); they pass alongside the
existing Version 3 tests. The asset tests pin the original WebP and ICO
checksums, check the ICO image sizes, verify the QR crop quiet zones and decode
both QR codes with a real decoder at 240, 300, 360 and 480 px. Native ComboBox
notification ordering is still exercised by the Windows acceptance checklist,
but the commit path no longer depends on it: the selected CIDR is captured from
the list and the committed state is authoritative. The results are recorded in
the release notes and the release evidence.

## Residual risks and limitations

* Windows GUI runtime acceptance, DPI behaviour, icon rendering and interaction
  with real clients are not performed. The pre-release is for that acceptance.
* The QR codes decode in automated tests at the display sizes, but actual
  scanning from a Windows screen with a phone camera still needs acceptance.
* The About dialog is fixed-layout and DPI-scaled with `px()`. It has not been
  visually checked at 100 %, 150 % or 200 % scaling on Windows.
* The committed-subnet model is unit-tested, but native ComboBox notifications
  still need real Windows acceptance. The design no longer depends on their
  ordering: **Scan**, **Monitor mode** and the context menu commit the visible
  text or the captured list CIDR, and the committed state is the only value the
  scanner reads.
* Network detection uses adapter friendly names. A VPN or physical adapter with
  an unusual name may be classified as `Other`, and a virtual adapter whose name
  does not match the exclusion list may appear. Manual CIDR entry remains the
  fallback.
* `ShellExecuteW` depends on a registered browser handler. If none is registered,
  the UI reports it.
* SSH and Telnet depend on optional Windows features. Missing clients are
  reported.
* SMB opens Explorer at the UNC path and may prompt for credentials.
* `exec.LookPath` uses the normal Windows `PATH` resolution. There is no shell,
  but executable resolution follows the operating system's rules.
* `ptrFromUintptr` is a narrow unsafe conversion for the `WM_NOTIFY` payload; the
  pointer comes from Windows and is used only inside the message handler.
* Releases are not code-signed, so SmartScreen may warn.
