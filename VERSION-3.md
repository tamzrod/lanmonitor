# LANMONITOR-003 — Version 3

Use Operation CWAL on feature/version-3. First reconcile main with the actual Version 2 release source; do not regress adaptive monitoring. If the release source cannot be established, stop and report.

## Features
- Native Help > About dialog: clickable https://github.com/tamzrod/lanmonitor and exact quote "Gwapong Pilipino Taas Noo Kahit Kanino!"
- Right-click any valid IP cell, regardless of ping status: HTTP, HTTPS, SSH, Telnet, RDP, SMB, Custom Port, Continuous Ping, Copy IP Address.
- Custom port: validate 1–65535 and allow HTTP/HTTPS/SSH as appropriate.
- Use native Windows handlers, validated IP addresses, direct process arguments, no shell interpolation. Report unavailable clients gracefully.
- No traceroute, automatic port scanning, donation QR codes or payment integration.
- Preserve manual scan, startup silence, optional 15-second full discovery, 1-second promoted-host polling, two consecutive discovery misses for demotion, and Stop behavior.
- Single portable Windows AMD64 GUI executable; MIT license; no new external runtime.
- Integer release version 3, tag v3, window title LAN Monitor — Version 3, artifact lanmonitor-v3.exe.

## Gates
Audit source and Win32 API usage; add tests for IP cell hit testing, address and port validation, and command construction. Run tests, static analysis and Windows build. Update README and changelog. Preserve previous releases. Build an unpublished candidate EXE and STOP at AWAITING_WINDOWS_ACCEPTANCE. Require documented actual Windows acceptance testing and explicit human approval before merging, tagging or publishing any release, including prereleases. Include SHA256SUMS.txt and release notes only for the approved release. Report commit, checks, size, checksum and download URL. Do not claim unperformed runtime testing.
