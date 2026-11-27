# LAN Monitor

A single-file Windows GUI utility that shows which hosts in one IPv4 `/24` subnet
answer ICMP echo requests. No installer, no browser, no runtime to install, no
external database.

* Portable Windows AMD64 executable (`lanmonitor.exe`), MIT licensed.
* Native Win32 GUI drawn with GDI; only system DLLs are used.
* Nothing is scanned at startup, and continuous polling is off by default.
* Manual **Scan** and **Stop** buttons.

## Download

Grab `lanmonitor.exe` from the [latest release](https://github.com/tamzrod/lanmonitor/releases),
verify it against `SHA256SUMS.txt`, and run it. Windows 10 or 11, 64-bit.

## Usage

1. Start the executable. The subnet box is prefilled with the first active
   non-loopback IPv4 `/24`, or left empty if there is none.
2. Edit the subnet if needed (`192.168.1.0/24`, `10.0.0.0/24`, ...). Only `/24`
   networks are accepted.
3. Press **Scan** for one sweep of `1`–`254`.
4. Hover a cell to see the address, its state and the last round-trip time.

Continuous polling stays off until you tick the box. The interval accepts 5 to
3600 seconds and is clamped into that range; the timer starts as soon as the box
is ticked and the first scan starts immediately. **Stop** cancels the remaining
probes, disables continuous polling and unticks the box.

Changing the subnet and scanning again clears the previous map.

## Cell colours

| Colour | Meaning |
| --- | --- |
| Light grey | Not scanned yet |
| Green | Responding to ICMP echo |
| Red | Answered before, silent in the latest scan |
| Dark grey | No answer yet |

A silent probe is not proof that a device is offline: firewalls, ICMP rate limits
and sleep states all suppress replies.

## Requirements and limits

* Windows 10/11 AMD64. The window layout scales with the system DPI.
* One `/24` at a time, 16 concurrent probes, 500 ms timeout per probe. A sweep of
  unreachable addresses takes about 8 seconds; if a sweep is still running when
  the polling timer fires, the overlapping run is skipped.
* The ICMP requests leave through the interface chosen by the OS routing table.
* No MAC or hostname discovery, no history, no IPv6, no export.

## Build

Go 1.22 or newer, no C toolchain needed:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor.exe .
```

See [BUILD.md](BUILD.md) for verification steps, the checksum procedure and the
local acceptance checklist.

## Tests

```sh
go test ./...                                   # unit tests on the host
GOOS=windows GOARCH=amd64 go test -c -o /dev/null .   # windows-only ABI tests compile
```

Unit tests cover subnet parsing, the address map, the state machine, interval
clamping, grid geometry and hit testing, plus the Win32 struct layout checks that
run on the Windows CI runner. CI builds the executable on `windows-latest` and
runs the tests on both Linux and Windows.

## License

MIT, see [LICENSE](LICENSE).
