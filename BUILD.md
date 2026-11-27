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
not open a console window.

## Tests

```sh
go test ./...                              # unit tests for platform-independent logic
GOOS=windows GOARCH=amd64 go vet ./...     # type-checks the Windows-only code
gofmt -l .                                 # must print nothing
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

Publish a checksum file next to the binary:

```sh
sha256sum lanmonitor.exe > SHA256SUMS.txt
```

On Windows, use `Get-FileHash .\lanmonitor.exe -Algorithm SHA256`.

## Local acceptance checklist

Run these on a real Windows machine on a network you are allowed to probe.

- Start the program with a packet capture running: no ICMP is sent until **Scan**
  is pressed.
- Confirm the subnet box is prefilled or empty, and that a non-`/24` entry such as
  `10.0.0.0/16` is rejected with a message.
- Scan an authorized `/24`: exactly 254 addresses (`.1`–`.254`) are probed, the
  network and broadcast addresses are not.
- Compare the map against known devices: responders green, silent hosts dark grey.
- Press **Stop** mid-scan: probes stop, the button pair re-enables, and no further
  probes appear in the capture.
- Tick **Continuous polling** with an interval of 5 seconds: sweeps repeat, the
  interval is honoured, and unticking or pressing **Stop** ends them.
- Disconnect a responding device: its cell turns red on the next sweep.
- Change the subnet and scan: the previous colours are cleared.
- Close the window during a sweep: the process exits.
- Check that text and controls are legible at 100 %, 150 % and 200 % display
  scaling.

## Release procedure

1. Build with the command above from a clean checkout.
2. Run the verification commands and record the size and SHA256.
3. Create `SHA256SUMS.txt` and attach it together with the executable to the
   release.

## Known build notes

* Windows SmartScreen warns about unsigned executables; releases are not
  code-signed.
* Antivirus software occasionally flags freshly built Go binaries; the artifact is
  reproducible from source with the command above.
