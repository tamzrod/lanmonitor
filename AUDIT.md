# Source and Win32 audit — Version 3

## Scope

This audit covers the Version 3 source on `feature/version-3`, including the
Windows-specific `main_windows.go`, the platform-independent `logic.go`, the
tests, the CI workflow and the release artifact checks. No Windows runtime or GUI
acceptance was performed for Version 3; that is the purpose of the pre-release.

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

## Win32 API inventory

| DLL | Entry points used | Purpose |
| --- | --- | --- |
| `kernel32.dll` | `GetModuleHandleW`, `GlobalAlloc`, `GlobalLock`, `GlobalUnlock`, `GlobalFree`, `RtlMoveMemory` | module handle and clipboard memory |
| `user32.dll` | window, menu, dialog, message, painting, clipboard and timer calls | native GUI |
| `gdi32.dll` | `CreateFontW`, `CreateSolidBrush`, `SelectObject`, `SetBkMode`, `SetTextColor`, `DeleteObject` | font and GDI painting |
| `iphlpapi.dll` | `IcmpCreateFile`, `IcmpSendEcho`, `IcmpCloseHandle` | ICMP echo |
| `shell32.dll` | `ShellExecuteW` | open a validated HTTP/HTTPS URL with its registered handler |
| `comctl32.dll` | `InitCommonControlsEx` | register the native SysLink control |

All are Windows system DLLs. The built executable's import table contains only
`kernel32.dll`; the other DLLs are resolved at runtime through `NewLazyDLL`.
There is no third-party runtime, installer or database.

## unsafe.Pointer review

* The structs passed to Win32 by pointer are ABI-tested: `rect`, `point`, `msg`,
  `wndClass`, `paintStruct`, `icmpEchoReply`, `nmhdr` and
  `initCommonControlsEx`.
* String arguments are converted to NUL-terminated UTF-16 buffers in the `call`
  helper, pinned for the duration of the call, and kept alive with
  `runtime.KeepAlive`.
* `ptrFromUintptr` is used only to recover the `WM_NOTIFY` `LPARAM` that Windows
  supplies. The pointer is valid only while that message is being handled.
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
* `projectURL` and `aboutQuote` are exact constants with tests. The quote is
  `Gwapong Pilipino Taas Noo Kahit Kanino!`.
* Custom Port is a native owned window with a protocol combo box, a numeric edit
  and OK/Cancel. The port is validated before the dialog closes, and the scheme
  is restricted to HTTP, HTTPS and SSH.

## Forbidden features

A source search found no traceroute, tracert, automatic port scan, donation QR
code or payment integration. The only occurrences of those words are in the
mission document and in the negative test that rejects them from the menu.
Manual **Scan**, startup silence, adaptive monitoring and **Stop** are preserved.

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
`kernel32.dll`-only import table, size and SHA256. The results are recorded in
the release notes and the release evidence.

## Residual risks and limitations

* Windows GUI runtime acceptance, DPI behaviour and interaction with real clients
  are not performed. The pre-release is for that acceptance.
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
