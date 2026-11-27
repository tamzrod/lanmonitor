# Building LAN Monitor

## Requirements

* Go 1.22 or newer. No C compiler, no SDK, no third-party Go modules.
* The program imports only `kernel32.dll` at load time; `user32.dll`, `gdi32.dll`
  and `iphlpapi.dll` are resolved at runtime, so nothing else has to be present.

## Build (Windows, PowerShell)

```powershell
$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"
go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor.exe .
```

## Build (Linux or macOS, cross-compile)

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor.exe .
```

`-trimpath` keeps build paths out of the binary, `-s -w` strips the symbol and
DWARF tables, and `-H=windowsgui` marks the PE subsystem as GUI so Windows does
not open a console window. The release asset is this binary under the name
`lanmonitor-v2.exe`.

## Tests

```sh
go test ./...                                   # unit tests for platform-independent logic
go test -race ./...                             # same, with the race detector
GOOS=windows GOARCH=amd64 go vet ./...          # type-checks the Windows-only code
GOOS=windows GOARCH=amd64 go test -c -o /dev/null .   # windows-only ABI tests compile
gofmt -l .                                      # must print nothing
```

`abi_windows_test.go` runs only on Windows. It asserts the size and field offsets
of the Win32 structs that are passed to the API by pointer.

## Verify the artifact

```sh
file lanmonitor.exe                            # PE32+ executable (GUI) x86-64
go version -m lanmonitor.exe                   # build settings: CGO_ENABLED=0, -trimpath, -s -w
objdump -p lanmonitor.exe | grep "DLL Name"    # kernel32.dll only
ls -l lanmonitor.exe                           # size in bytes
sha256sum lanmonitor.exe                       # checksum
```

Without `file`/`objdump`, read the PE header directly:

```sh
python3 - <<'PY'
import struct
d = open("lanmonitor.exe", "rb").read()
pe = struct.unpack_from("<I", d, 0x3c)[0]
print("machine   0x%04x (0x8664 = amd64)" % struct.unpack_from("<H", d, pe + 4)[0])
print("magic     0x%03x (0x20b = PE32+)" % struct.unpack_from("<H", d, pe + 24)[0])
print("subsystem %d (2 = Windows GUI)" % struct.unpack_from("<H", d, pe + 24 + 68)[0])
PY
```

Publish a checksum file next to the binary:

```sh
sha256sum lanmonitor-v2.exe > SHA256SUMS.txt
```

On Windows, use `Get-FileHash .\lanmonitor-v2.exe -Algorithm SHA256`.

## Local acceptance checklist

Run these on a real Windows machine on a network you are allowed to probe, with a
packet capture running.

Startup

- Launch the program: no ICMP is sent until **Scan** or **Monitor mode** is used.
- The title bar reads `LAN Monitor — Version 2`.
- **Monitor mode** is unticked and the status line reads `Ready - monitoring off`.
- All 254 cells are light grey.
- The subnet box is prefilled with an active `/24` or empty; a non-`/24` entry
  such as `10.0.0.0/16` is rejected with a message.

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
- Check that text and controls are legible at 100 %, 150 % and 200 % display
  scaling.

## Verification performed for v2

The Windows-specific behaviour above was exercised on Linux with Wine and a
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
network, is still pending and is the purpose of the pre-release.

## Release procedure

1. Build with the command above from a clean checkout.
2. Run the verification commands and record the size and SHA256.
3. Rename or copy the binary to `lanmonitor-v2.exe`.
4. Create `SHA256SUMS.txt` with `sha256sum lanmonitor-v2.exe > SHA256SUMS.txt`.
5. Attach both files to the release and mark it as a pre-release while Windows
   acceptance testing is outstanding.

## Known build notes

* Windows SmartScreen warns about unsigned executables; releases are not
  code-signed.
* Antivirus software occasionally flags freshly built Go binaries; the artifact is
  reproducible from source with the command above.
