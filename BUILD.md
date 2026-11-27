# Building LAN Monitor

## Requirements

* Go 1.22 or newer. No C compiler and no Windows SDK.
* The application uses `golang.org/x/image` to decode and scale the embedded WebP
  QR images. It is compiled into the executable; there is no external runtime,
  image file or installer. Tests also use `github.com/makiuchi-d/gozxing` to
  decode the displayed QR codes; that module is test-only.
* The program imports only `kernel32.dll` at load time; `user32.dll`, `gdi32.dll`,
  `iphlpapi.dll`, `shell32.dll` and `comctl32.dll` are resolved at runtime, so
  nothing else has to be present.
* Optional client programs used by the IP cell context menu: `ssh.exe` (Windows
  OpenSSH Client), `telnet.exe` (Telnet Client feature), `mstsc.exe` (Remote
  Desktop Connection), `explorer.exe` and `ping.exe`. A missing client is
  reported in the UI; the rest of the program keeps working.

## Build (Windows, PowerShell)

```powershell
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor-v3.exe .
```

## Build (Linux or macOS, cross-compile)

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor-v3.exe .
```

`-trimpath` keeps build paths out of the binary, `-s -w` strips the symbol and
DWARF tables, and `-H=windowsgui` marks the PE subsystem as GUI so Windows does
not open a console window. The release asset is this binary under the name
`lanmonitor-v3.exe`.

## Assets and icon

The source assets live under `assets/`:

| File | Purpose | SHA256 |
| --- | --- | --- |
| `maya.webp` | Maya donation QR card, original upload | `0480510c7fe673a8e769dccd37c5f65f1bacdfae52893b7c0600bcdc06461112` |
| `gcash.webp` | GCash donation QR card, original upload | `6189ac97a550288f697a496d9039844fa45330d2512ce92f4782823d50af962b` |
| `lanmonitor.ico` | Retro network icon, 16–256 px | `15d1bd6937b8cbb20dc5d735e013571aee859f9b159353ce2567cd6198346aaa` |

The WebP files are embedded with `//go:embed`. The About dialog crops each image
to the QR code plus its white quiet zone and scales it with the system DPI. The
source files are not modified and the QR codes are not regenerated. Tests pin the
checksums and decode both QR codes at the display sizes.

The icon is embedded through `rsrc_windows_amd64.syso`, which is committed so a
normal `go build` includes it. Regenerate it only when the icon changes:

```sh
go install github.com/akavel/rsrc@latest
rsrc -ico assets/lanmonitor.ico -o rsrc_windows_amd64.syso
```

The icon resource ID is 1. `cmd/verify-pe` checks the PE header and confirms that
`RT_GROUP_ICON` 1 contains the seven icon images.

## Tests

```sh
go test ./...                                   # unit tests for platform-independent logic
go test -race ./...                             # same, with the race detector
GOOS=windows GOARCH=amd64 go vet ./...          # type-checks the Windows-only code
GOOS=windows GOARCH=amd64 go test -c -o /dev/null .   # windows-only ABI tests compile
gofmt -l .                                      # must print nothing
staticcheck ./...                               # host static analysis
GOOS=windows GOARCH=amd64 staticcheck ./...     # Windows static analysis
```

`abi_windows_test.go` runs only on Windows. It asserts the size and field offsets
of the Win32 structs that are passed to the API by pointer, including `NMHDR`
and `INITCOMMONCONTROLSEX`.

Unit tests cover subnet parsing, network adapter classification and filtering,
`/24` derivation from adapter addresses, candidate ordering and de-duplication,
the address map, grid geometry and hit testing, the promotion/demotion state
machine, target selection per round, the authoritative committed subnet state,
the monitoring cadence, IPv4 and port validation, command construction for every
context menu action, the exact context menu contract, and the About identity.
The committed-subnet tests cover A-to-B-to-A switching, manual entry, **Scan**
and **Monitor** commits, **Refresh**, drop-down cancellation, and late results
from an old subnet. Native ComboBox notification ordering is still exercised by
the Windows acceptance checklist; the commit path no longer depends on it because
the selected CIDR is captured from the list and the committed state is the only
value the scanner reads.

## Verify the artifact

```sh
go run ./cmd/verify-pe lanmonitor-v3.exe       # PE32+ AMD64 GUI and icon resource 1
file lanmonitor-v3.exe                         # PE32+ executable (GUI) x86-64
go version -m lanmonitor-v3.exe                # build settings: CGO_ENABLED=0, -trimpath, -s -w
objdump -p lanmonitor-v3.exe | grep "DLL Name" # kernel32.dll only
ls -l lanmonitor-v3.exe                        # size in bytes
sha256sum lanmonitor-v3.exe                    # checksum
```

Without `file`/`objdump`, read the PE header directly:

```sh
python3 - <<'PY'
import struct
d = open("lanmonitor-v3.exe", "rb").read()
pe = struct.unpack_from("<I", d, 0x3c)[0]
print("machine   0x%04x (0x8664 = amd64)" % struct.unpack_from("<H", d, pe + 4)[0])
print("magic     0x%03x (0x20b = PE32+)" % struct.unpack_from("<H", d, pe + 24)[0])
print("subsystem %d (2 = Windows GUI)" % struct.unpack_from("<H", d, pe + 24 + 68)[0])
print("size      %d" % len(d))
PY
```

Publish a checksum file next to the binary:

```sh
sha256sum lanmonitor-v3.exe > SHA256SUMS.txt
```

On Windows, use `Get-FileHash .\lanmonitor-v3.exe -Algorithm SHA256`.

## Local acceptance checklist

Run these on a real Windows machine on a network you are allowed to probe, with a
packet capture running.

Startup

- Launch the program: no ICMP is sent until **Scan** or **Monitor mode** is used.
- The title bar reads `LAN Monitor — Version 3`.
- **Monitor mode** is unticked and the status line reads `Ready - monitoring off`,
  or `No IPv4 networks detected - type a /24 manually` when no eligible adapter
  exists.
- All 254 cells are light grey.
- The network selector is an editable ComboBox prefilled with the best detected
  `/24` or empty. A non-`/24` entry such as `10.0.0.0/16` is rejected with a
  message.
- Open the dropdown: active Ethernet, Wi-Fi and VPN adapters (including
  WireGuard, OpenVPN and Tailscale) are listed. Docker, WSL, Hyper-V, VMware,
  VirtualBox and loopback are absent.
- **Help > About** opens a native dialog with the application icon, a clickable
  `https://github.com/tamzrod/lanmonitor` link, the MIT licence and the exact
  quote `Gwapong Pilipino Taas Noo Kahit Kanino!`. Clicking the link opens the
  default browser.
- The About dialog shows the Support Development message and the Maya and GCash
  donation QR codes side by side. Both codes scan with the normal phone apps.
  The QR squares, quiet zones and aspect ratio are not distorted.
- The executable, title bar, taskbar, Alt+Tab entry and About dialog all show the
  retro network icon.

Network selector

- On a host with both Ethernet and Wi-Fi, both `/24` networks appear, Ethernet
  first.
- With an active WireGuard, OpenVPN or Tailscale adapter, its `/24` appears as a
  VPN entry. A `/32` VPN address still produces a usable `/24`.
- With Docker, WSL, Hyper-V, VMware or VirtualBox installed, their virtual
  networks do not appear.
- Connect or disconnect a VPN, press **Refresh**, and confirm the list changes
  while a manually typed CIDR is preserved.
- Type the `/24` of an excluded virtual network and scan it: manual entry is an
  explicit override and still works.
- Select a dropdown entry: the edit field shows that CIDR, not blank and not the
  adapter label. The status line names the same CIDR. While monitoring, the
  selection starts a sweep immediately.
- Open the dropdown and press Esc, or click outside it: the edit field returns
  to the text it held before the dropdown opened, including a manually typed
  CIDR.
- After selecting an entry, click the edit field and type a different CIDR:
  manual editing still works. **Scan** and the right-click actions use the
  displayed CIDR.
- Select an entry and immediately press **Scan**: the sweep targets the selected
  CIDR, not the previous one.
- Select an entry and immediately tick **Monitor mode**: the first sweep targets
  the selected CIDR.
- Select A, then B, then A again: each switch clears the map, and no result from
  the earlier A or B round appears in the map.
- Select an entry, then cancel the drop-down with Esc or by clicking outside:
  the edit field keeps the selected CIDR, and **Scan** and **Monitor mode** use
  it. Cancelling without a selection restores a manually typed CIDR.
- Type a CIDR while monitoring and wait for the 15-second sweep: the sweep uses
  the typed CIDR. Partial or invalid text does not stop monitoring.
- Hover a cell after a selection: the status line names the committed CIDR.
- Right-click a cell after typing a CIDR but before **Scan**: the action uses the
  typed CIDR.
- Switch from one network to another: the previous colours clear, and results
  from the old sweep do not appear in the new map.
- Type a different CIDR while monitoring: the next **Scan** or 15-second sweep
  uses it, and the selector stays editable throughout.
- Watch the packet capture while starting the program and while pressing
  **Refresh**: detection and refresh send no ICMP.

Manual scan

- Press **Scan** on an authorized `/24`: exactly 254 addresses (`.1`–`.254`) are
  probed; network and broadcast addresses are not.
- Responders turn green, silent hosts dark grey, and the status line ends with
  `Scan complete`.
- Hover a green cell: the status line shows the address, `Responding`, the
  round-trip time and `Promoted`.
- Press **Scan** again during a sweep: the status line says one more sweep is
  queued, and the second sweep starts after the first finishes.
- Change the subnet and scan: the previous colours are cleared.

Adaptive monitoring

- Tick **Monitor mode**: a sweep starts immediately, then repeats every 15
  seconds; the status line reports the next sweep.
- Count echo requests in the capture for one minute: about 254 every 15 seconds
  plus one per second per promoted host.
- Power off a device that was answering: its cell turns orange (suspect) after the
  first missed sweep and red (lost response) after the second, roughly 15 and 30
  seconds later. Active probes to that address stop; it stays in the sweeps.
- Bring the device back: within 15 seconds its cell turns green again and it is
  promoted without user action.
- Tick **Monitor mode** off: timers stop, no further echo requests appear.
- Press **Stop** during a sweep: probes stop within the 500 ms timeout, the
  checkbox clears, the network selector remains editable and the status line
  reads `Stopped`.
- Press **Stop** and then **Scan**: the manual sweep still runs.
- Close the window during a sweep: the process exits.

IP cell context menu

- Right-click a responding cell, a suspect cell, a lost cell and an unscanned
  cell. The same menu appears every time: HTTP, HTTPS, SSH, Telnet, RDP, SMB,
  Custom Port, Continuous Ping, Copy IP Address.
- Right-click a gap or outside the grid: no menu appears.
- With an empty or invalid subnet, choosing an item shows a message instead of
  opening a client.
- HTTP and HTTPS open the default browser at the selected address.
- SSH opens a console running `ssh.exe <ip>`, or shows a message when the
  OpenSSH Client is not installed.
- Telnet opens a console running `telnet.exe <ip>`, or shows a message when the
  Telnet Client feature is not installed.
- RDP starts `mstsc.exe /v:<ip>`.
- SMB opens Explorer at `\\<ip>`.
- Custom Port rejects `0`, `65536`, `-1` and non-numeric input; accepts `1` and
  `65535`; and allows HTTP, HTTPS and SSH.
- Continuous Ping opens a console running `ping.exe -t <ip>`.
- Copy IP Address puts the exact validated address on the clipboard.
- No `cmd.exe` window appears for any action. The client receives the address as
  one argument.
- There is no traceroute or automatic port scanning. The About dialog contains
  static Maya and GCash donation QR images, but there is no payment processing,
  tracking or automatic network request.

Display

- Check that text and controls are legible at 100 %, 150 % and 200 % display
  scaling.

## Verification performed for v3

The v3 candidate was verified with automated checks only, on a Linux build host:

* `gofmt -l .` printed nothing.
* `go vet ./...` and `GOOS=windows GOARCH=amd64 go vet ./...` passed.
* `staticcheck ./...` and `GOOS=windows GOARCH=amd64 staticcheck ./...` passed.
* `go test -race ./...` passed.
* `GOOS=windows GOARCH=amd64 go test -c` compiled the Windows ABI tests.
* The cross-compiled binary was checked as PE32+ AMD64 with the Windows GUI
  subsystem, a `kernel32.dll`-only import table and `RT_GROUP_ICON` resource 1
  containing the seven icon images.

The network-selector correction adds tests for adapter classification, `/24`
derivation, filtering, ordering, duplicate preference and labels. Those tests,
the existing Version 3 tests and the Windows ABI compile all pass.

The committed-subnet correction adds `subnet.go`, which owns the authoritative
subnet and the host map, and `subnet_test.go`, which covers A-to-B-to-A
switching, manual entry, **Scan** and **Monitor** commits, **Refresh**,
drop-down cancellation (including cancellation after a committed selection), and
late results from an old subnet. The ComboBox text is pending input only; every
scan, monitor round, hover status and context-menu action reads the committed
subnet. A change cancels the old rounds, invalidates their session and clears the
host map. The current candidate's commit and SHA256 are recorded in the v3
release notes and the final release report.

The assets correction adds the application icon, the enlarged About dialog and
the Maya/GCash donation QR images. `assets_test.go` pins the original WebP and
ICO checksums, verifies the ICO has 16, 24, 32, 48, 64, 128 and 256 px images,
checks the QR crops keep a white quiet zone, and decodes both QR codes at 240,
300, 360 and 480 px. `cmd/verify-pe` checks the built executable's PE header and
`RT_GROUP_ICON` 1. The committed `.syso` is built from `assets/lanmonitor.ico`.
The original assets are byte-for-byte unchanged; only display crops are made at
runtime.

No Windows runtime or GUI acceptance was performed for v3. The pre-release
exists so that this checklist can be run on real Windows hardware. The v2 Wine
verification below is historical and does not cover the new v3 UI.

### Release correction record

The original v3 prerelease was built from commit
`5d9cd8ebec408566f9dce20189dfabde7bf1f0af` with `lanmonitor-v3.exe` SHA256
`5411833641483be162ab457eeecced5331ddc0ee14e46fd0e90f0582858a0d90`.

The first network-selector correction was built from commit
`41e169bfbc73389ee33e6799c424c6a9bd55193c` with `lanmonitor-v3.exe` SHA256
`f89559c1fde94d10519414f919df0f674ac4d7e9d92ea60a90939af9f843e536`.

The ComboBox notification correction republished the same `v3` prerelease from
the corrected commit. It commits a selected entry from `CBN_SELENDOK` through a
posted message and removes `CB_SETCURSEL(-1)`, which cleared the edit field. It
was built from commit `561a6eff961dda5d4a3695885b0429007c440cce` with
`lanmonitor-v3.exe` SHA256
`302bb13e32de8c298460e5cfaff5b210c24001f49b28893e6db69ca445b42e33`.

The committed-subnet correction replaces the notification-driven selector with
an authoritative committed subnet in `subnet.go`. A selected CIDR is captured
from the list and committed independently of the transient edit text; **Scan**,
**Monitor mode**, the 15-second sweep, hover and the context menu all read the
committed value. A subnet change cancels old rounds, invalidates their scan
session and clears the host map. Cancelling a drop-down after a committed
selection keeps the selected subnet, and cancelling without a selection restores
the pre-drop text. Regression tests cover A-to-B-to-A switching, manual entry,
**Scan**, **Monitor**, **Refresh**, drop-down cancellation and late old-subnet
results. The `v3` tag is moved to this correction and both assets are replaced.
No `v4` tag or release was created. The current commit and checksum are recorded
in the v3 release notes and the final release report.

The assets correction moves the uploaded images into `assets/`, adds the
application icon, embeds the icon resource and the WebP QR images, and enlarges
the About dialog with the Support Development section. It replaces the previous
`lanmonitor-v3.exe` asset (SHA256
`abf16250f48f2cf3b201f754d93eec60b9c49b51d96bd2e00a820810bbcae793`) and moves
the `v3` tag to the new correction commit. No `v4` tag or release is created.

## Verification performed for v2 (historical)

The Windows-specific behaviour of v2 was exercised on Linux with Wine and a
virtual interface, not on Windows:

* A dummy interface carried `10.99.99.1/24`, so exactly one address answered.
* `IcmpSendEcho` through `iphlpapi.dll` reached it, and the map, the promotion
  flag and the round-trip time matched.
* `/proc/net/snmp` counted the echo requests: 1 per second while one host was
  promoted, and bursts of 254 every 15 seconds.
* Removing the address turned the cell orange after the first missed sweep and
  red after the second; the one-per-second requests stopped while the 15-second
  sweeps continued.
* Restoring the address promoted the host again on the next sweep.
* **Stop** ended all traffic: zero echo requests in the following 17 seconds.

Real Windows GUI acceptance, including DPI scaling and behaviour with a mixed
network, is still pending.

## Release procedure

1. Build with the command above from a clean checkout.
2. Run the verification commands, including `go run ./cmd/verify-pe`, and record
   the size and SHA256.
3. Name the binary `lanmonitor-v3.exe`.
4. Create `SHA256SUMS.txt` with `sha256sum lanmonitor-v3.exe > SHA256SUMS.txt`.
5. Attach both files to the release and mark it as a pre-release while Windows
   acceptance testing is outstanding.
6. Promote to stable only after documented Windows acceptance and explicit
   human approval.
7. For a correction, move the existing `v3` tag to the corrected commit and
   replace both assets. Record the previous commit and checksum in the release
   notes. Do not create a new major version for a correction.

## Known build notes

* Windows SmartScreen warns about unsigned executables; releases are not
  code-signed.
* Antivirus software occasionally flags freshly built Go binaries; the artifact is
  reproducible from source with the command above.
