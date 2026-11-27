# LAN Monitor

A single-file Windows GUI utility that shows which hosts in one IPv4 `/24`
subnet answer ICMP echo requests, then keeps watching the ones that do.

Version 3 adds a native **Help > About** dialog, a right-click menu on every IP
cell, and an editable network selector. A discovered host can be opened in HTTP,
HTTPS, SSH, Telnet, RDP, SMB, a custom port, a continuous ping, or copied to the
clipboard. The selector auto-detects IPv4 `/24` networks on active Ethernet,
Wi-Fi and VPN adapters, can be refreshed, and accepts a manually typed CIDR.
Version 2's adaptive monitoring is unchanged: a full sweep every 15 seconds finds
the hosts, and only the hosts that answered are probed every second, so a live
network is not flooded with 254 probes per second.

* Portable Windows AMD64 executable (`lanmonitor-v3.exe`), MIT licensed.
* Native Win32 GUI drawn with GDI; only system DLLs are used.
* Editable network ComboBox with automatic `/24` detection, a refresh button and
  manual CIDR entry.
* Detects physical Ethernet, Wi-Fi and VPN adapters including WireGuard, OpenVPN
  and Tailscale. Docker, WSL, Hyper-V virtual switches, VMware, VirtualBox,
  loopback and similar virtual adapters are excluded by default.
* The network input stays editable during discovery and monitoring.
* No installer, no browser, no runtime to install, no external database.
* Nothing is scanned at startup, and monitoring is off by default.
* Manual **Scan** and **Stop** buttons.
* No traceroute, automatic port scanning, donation QR codes or payment features.

## Download

Grab `lanmonitor-v3.exe` from the [latest release](https://github.com/tamzrod/lanmonitor/releases),
verify it against `SHA256SUMS.txt`, and run it. Windows 10 or 11, 64-bit.

## Usage

1. Start the executable. The network ComboBox is prefilled with the best
   detected `/24` (Ethernet first, then Wi-Fi, VPN and other adapters), or left
   empty with a prompt to type one if there is none. Opening the dropdown lists
   the detected networks.
2. Choose a detected network, press **Refresh** after connecting a VPN, or type a
   CIDR manually (`192.168.1.0/24`, `10.0.0.0/24`, ...). Only `/24` networks are
   accepted. Manual entry can target a network that is excluded from the list.
   The map always covers `.1`–`.254`.
3. Press **Scan** for one sweep. A sweep sends one echo request to each of the
   254 usable addresses and needs no monitoring.
4. Hover a cell to see the address, its state and the last round-trip time.
5. Tick **Monitor mode** to start adaptive monitoring. The first sweep starts
   immediately, then every 15 seconds; hosts that answered are probed every
   second.
6. Right-click any cell, whatever its colour, to open the client menu.
7. Use **Help > About** for the project URL and the release quote.

**Stop** ends monitoring, cancels probes in flight, discards a queued sweep and
unticks the box. The map keeps the last known state until the next scan.

Changing the subnet and scanning again clears the previous map. The selector
stays editable while monitoring: choosing a different network starts a sweep
immediately, while a manually typed CIDR takes effect on the next **Scan** or
the next 15-second sweep.

Selecting a dropdown entry writes that entry's CIDR into the edit field; the
adapter label stays in the list. Cancelling the dropdown restores the text that
was in the field before it opened, so a manual CIDR is not lost. A selection
never leaves the edit field blank.

## Network selector

The network input is an editable ComboBox, not a locked text box. At startup it
enumerates the active IPv4 adapters and lists one entry per `/24`. The `/24` is
the one that contains the adapter address, so a VPN `/32` still yields a usable
`/24`. Duplicate networks are removed, and the list is sorted Ethernet, Wi-Fi,
VPN, then other adapters. The first entry is filled into the edit field
automatically.

Included by default:

* Physical Ethernet adapters.
* Wi-Fi adapters.
* VPN adapters, including WireGuard, OpenVPN, Tailscale, ZeroTier and Cloudflare
  WARP.

Excluded by default:

* Loopback and pseudo interfaces.
* Docker and container bridges (`docker0`, `veth`, `br-`, `virbr`, Podman, CNI,
  Flannel and Calico).
* WSL and Hyper-V virtual switches (`vEthernet`, `Default Switch`).
* VMware (`VMnet`) and VirtualBox host-only adapters.
* Wi-Fi Direct and hosted-network virtual adapters, Bluetooth, Teredo, ISATAP and
  Npcap loopback adapters.

**Refresh** rebuilds the list from the current adapters. It keeps a manually
typed CIDR and sends no ICMP. Manual entry is always available, so an excluded
virtual network can still be scanned when that is deliberate.

The selection is committed from `CBN_SELENDOK` through a posted message after
the native ComboBox has finished updating. The edit field is set to the CIDR
with `SetWindowTextW`; `CB_SETCURSEL(-1)` is not used because it clears the edit
control. `CBN_SELENDCANCEL` restores the pre-drop text.

## IP cell context menu

The menu is available on every valid host cell (`.1`–`.254`), whether the host
is responding, suspect, lost or not yet scanned.

| Item | Action |
| --- | --- |
| **HTTP** | Opens `http://<ip>/` with the default browser |
| **HTTPS** | Opens `https://<ip>/` with the default browser |
| **SSH** | Starts `ssh.exe <ip>` in a new console |
| **Telnet** | Starts `telnet.exe <ip>` in a new console |
| **RDP** | Starts `mstsc.exe /v:<ip>` |
| **SMB** | Opens `explorer.exe \\<ip>` |
| **Custom Port** | Prompts for HTTP, HTTPS or SSH and a port from 1 to 65535 |
| **Continuous Ping** | Starts `ping.exe -t <ip>` in a new console |
| **Copy IP Address** | Copies the validated address to the clipboard |

Every action validates the IPv4 address first and passes each argument directly
to the client executable. No command shell, `cmd.exe` or string interpolation is
used. If a client is not installed, LAN Monitor shows a message instead of
failing silently.

Custom Port accepts only HTTP, HTTPS and SSH. HTTP and HTTPS build a URL on the
chosen port; SSH uses `-p <port>`.

## About dialog

**Help > About** shows the release title, the MIT licence, a clickable
[project URL](https://github.com/tamzrod/lanmonitor) and this exact quote:

> Gwapong Pilipino Taas Noo Kahit Kanino!

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
* Network detection uses Windows adapter friendly names. Known virtual and
  container adapters are excluded; an unusually named adapter may appear as
  `Other` or require manual entry.
* The ICMP requests leave through the interface chosen by the OS routing table.
* SSH needs the Windows OpenSSH Client. Telnet needs the Windows Telnet Client
  feature. RDP uses Remote Desktop Connection. SMB uses Windows Explorer.
* No MAC or hostname discovery, no history, no IPv6, no export.
* A sweep is a burst of 254 echo requests. Use the program only on networks you
  are allowed to probe.

## Build

Go 1.22 or newer, no C toolchain needed:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor-v3.exe .
```

See [BUILD.md](BUILD.md) for verification steps, the checksum procedure and the
acceptance checklist, [ADAPTIVE.md](ADAPTIVE.md) for the monitoring design, and
[AUDIT.md](AUDIT.md) for the source and Win32 audit.

## Tests

```sh
go test ./...                                          # unit tests on the host
go test -race ./...                                    # with the race detector
GOOS=windows GOARCH=amd64 go vet ./...                 # type-checks the Windows code
GOOS=windows GOARCH=amd64 go test -c -o /dev/null .    # windows-only ABI tests compile
```

Unit tests cover subnet parsing, network adapter classification and filtering,
`/24` derivation from adapter addresses, candidate ordering and de-duplication,
the address map, grid geometry and hit testing, the promotion/demotion state
machine, target selection per round, scan-session transitions and the monitoring
cadence, IPv4 and port validation, client command construction, the context menu
contract, and the Win32 struct layout checks that run on the Windows CI runner.
The native ComboBox notification ordering is covered by the Windows acceptance
checklist in [BUILD.md](BUILD.md). CI builds the executable on `windows-latest`,
runs the tests on both Linux and Windows, and checks the PE header of the
artifact.

## License

MIT, see [LICENSE](LICENSE).
