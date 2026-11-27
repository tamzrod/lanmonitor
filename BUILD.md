# LAN Monitor

Portable Windows AMD64 IPv4 /24 ICMP monitor, MIT licensed. No installer, browser, third-party runtime or external database.

## Build
Install Go 1.22 or newer on the build machine. In PowerShell:

```powershell
go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor.exe .
```

On Linux:
```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o lanmonitor.exe .
```

## Usage
Launch the executable. The first active non-loopback /24 IPv4 interface is suggested; edit the CIDR as needed. Startup does not send ping traffic. Click Scan for one sweep. Continuous polling is unchecked by default; enabling it starts scanning. Set interval before enabling (5–3600 seconds). Stop cancels remaining jobs and disables continuous polling. In-flight ICMP requests can take up to 500 ms to return.

Cells 1–254 are fixed in address order. Gray = unscanned; green = responding; red = previously responding but now no response; dark gray = no response yet. Hover over a cell for status and last successful scan latency when responding. Changing subnet and starting a scan clears prior state. ICMP nonresponse does not prove a device is offline.

## Local acceptance checklist
- Launch with Wireshark capture: no startup ICMP.
- Scan an authorized /24: only usable host addresses are probed.
- Confirm known responding and nonresponding devices.
- Stop during scan and confirm no repeated scans.
- Enable continuous polling and verify repeat scans, then disable it.
- Disconnect a responding device and verify red after next scan.
- Change subnet and verify state resets.
- Close during scan and confirm process exits.

## MVP limitations
Windows runtime testing is pending. No MAC/hostname discovery, persistent history, selectable interface binding, or IPv6 yet. OS routing selects the outgoing interface. Fixed-size native GDI window; no SVG dependency. Sixteen concurrent workers, 500 ms timeout. Scan duration may exceed the polling interval; overlapping scans are skipped. Use only on authorized networks.
