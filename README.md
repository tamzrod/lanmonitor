# LAN Monitor

A single-file Windows GUI utility that shows which hosts in one IPv4 `/24`
subnet answer ICMP echo requests, then keeps watching the ones that do.

Version 2 adds adaptive monitoring. A full sweep every 15 seconds finds the
hosts, and only the hosts that answered are probed every second, so a live
network is not flooded with 254 probes per second. Two missed sweeps demote a
host back to sweep-only discovery.

* Portable Windows AMD64 executable (`lanmonitor-v2.exe`), MIT licensed.
* Native Win32 GUI drawn with GDI; only system DLLs are used.
* No installer, no browser, no runtime to install, no external database.
* Nothing is scanned at startup, and monitoring is off by default.
* Manual **Scan** and **Stop** buttons.

## Download

Grab `lanmonitor-v2.exe` from the [latest release](https://github.com/tamzrod/lanmonitor/releases),
verify it against `SHA256SUMS.txt`, and run it. Windows 10 or 11, 64-bit.

## Usage

1. Start the executable. The subnet box is prefilled with the first active
   non-loopback IPv4 `/24`, or left empty if there is none.
2. Edit the subnet if needed (`192.168.1.0/24`, `10.0.0.0/24`, ...). Only `/24`
   networks are accepted. The map always covers `.1`–`.254`.
3. Press **Scan** for one sweep. A sweep sends one echo request to each of the
   254 usable addresses and needs no monitoring.
4. Hover a cell to see the address, its state and the last round-trip time.
5. Tick **Monitor mode** to start adaptive monitoring. The first sweep starts
   immediately, then every 15 seconds; hosts that answered are probed every
   second.

**Stop** ends monitoring, cancels probes in flight, discards a queued sweep and
unticks the box. The map keeps the last known state until the next scan.

Changing the subnet and scanning again clears the previous map.

## Adaptive monitoring

| Interval | What happens |
| --- | --- |
| 15 s | Discovery sweep: all 254 addresses are probed in parallel |
| 1 s | Active round: only the promoted hosts (those that answered a sweep) are probed |

Promotion and demotion:

* An address that answers a discovery sweep is promoted and probed every second.
* Two consecutive discovery sweeps without an answer demote the host. It is no
  longer probed every second but stays in every sweep, so it comes back
  automatically when it answers again.
* A missed active probe only changes the displayed state. One lost echo on a
  busy link is not evidence that a device has gone away, so active misses never
  demote.

## Cell colours

| Colour | State | Meaning |
| --- | --- | --- |
| Light grey | Not scanned | No sweep has covered this address yet |
| Green | Responding | Answered the most recent probe |
| Orange | Suspect | Promoted host that missed the most recent probe; still probed every second |
| Red | Lost response | Answered before, silent now, including demoted hosts |
| Dark grey | No response | Never answered |

A silent probe is not proof that a device is offline: firewalls, ICMP rate limits
and sleep states all suppress replies.

## Requirements and limits

* Windows 10/11 AMD64. The window layout scales with the system DPI.
* One `/24` at a time, up to 32 concurrent probes, 500 ms timeout per probe. A
  sweep of unreachable addresses takes about 4 seconds.
* One sweep at a time. A sweep requested while one is still running is queued
  once, not run in parallel.
* The ICMP requests leave through the interface chosen by the OS routing table.
* No MAC or hostname discovery, no history, no IPv6, no export.
* A sweep is a burst of 254 echo requests. Use the program only on networks you
  are allowed to probe.

## Build

Go 1.22 or newer, no C toolchain needed:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor.exe .
```

The release asset is that same binary named `lanmonitor-v2.exe`. See
[BUILD.md](BUILD.md) for verification steps, the checksum procedure and the
acceptance checklist, and [ADAPTIVE.md](ADAPTIVE.md) for the monitoring design.

## Tests

```sh
go test ./...                                          # unit tests on the host
go test -race ./...                                    # with the race detector
GOOS=windows GOARCH=amd64 go vet ./...                 # type-checks the Windows code
GOOS=windows GOARCH=amd64 go test -c -o /dev/null .    # windows-only ABI tests compile
```

Unit tests cover subnet parsing, the address map, grid geometry and hit testing,
the promotion/demotion state machine, target selection per round and the
monitoring cadence, plus the Win32 struct layout checks that run on the Windows
CI runner. CI builds the executable on `windows-latest`, runs the tests on both
Linux and Windows, and checks the PE header of the artifact.

## License

MIT, see [LICENSE](LICENSE).
