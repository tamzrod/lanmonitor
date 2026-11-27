# Building LAN Monitor

## Requirements

* Go 1.22 or newer. No C compiler, no SDK, no third-party Go modules.
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

Unit tests cover subnet parsing, the address map, grid geometry and hit testing,
the promotion/demotion state machine, target selection per round and the
monitoring cadence, IPv4 and port validation, command construction for every
context menu action, the exact context menu contract, and the About identity.

## Verify the artifact

```sh
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
- **Monitor mode** is unticked and the status line reads `Ready - monitoring off`.
- All 254 cells are light grey.
- The subnet box is prefilled with an active `/24` or empty; a non-`/24` entry
  such as `10.0.0.0/16` is rejected with a message.
- **Help > About** opens a native dialog with a clickable
  `https://github.com/tamzrod/lanmonitor` link, the MIT licence and the exact
  quote `Gwapong Pilipino Taas Noo Kahit Kanino!`. Clicking the link opens the
  default browser.

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
  checkbox clears, the subnet box re-enables and the status line reads `Stopped`.
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
- There is no traceroute, automatic port scanning, donation QR code or payment
  feature anywhere in the UI.

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
  subsystem and a `kernel32.dll`-only import table.

No Windows runtime or GUI acceptance was performed for v3. The pre-release
exists so that this checklist can be run on real Windows hardware. The v2 Wine
verification below is historical and does not cover the new v3 UI.

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
2. Run the verification commands and record the size and SHA256.
3. Name the binary `lanmonitor-v3.exe`.
4. Create `SHA256SUMS.txt` with `sha256sum lanmonitor-v3.exe > SHA256SUMS.txt`.
5. Attach both files to the release and mark it as a pre-release while Windows
   acceptance testing is outstanding.
6. Promote to stable only after documented Windows acceptance and explicit
   human approval.

## Known build notes

* Windows SmartScreen warns about unsigned executables; releases are not
  code-signed.
* Antivirus software occasionally flags freshly built Go binaries; the artifact is
  reproducible from source with the command above.
