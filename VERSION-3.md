# LANMONITOR-003 — Version 3

Use Operation CWAL on feature/version-3. First reconcile main with the actual Version 2 release source; do not regress adaptive monitoring. If the release source cannot be established, stop and report.

## Features
- Native Help > About dialog: application icon, clickable https://github.com/tamzrod/lanmonitor and exact quote "Gwapong Pilipino Taas Noo Kahit Kanino!".
- Support Development section in About with the exact message "This software is completely free. If you find it useful, consider supporting its development." and Maya and GCash donation QR images side by side. Bitcoin is deferred.
- Embedded retro network icon for the executable, title bar, taskbar, Alt+Tab and About dialog.
- Right-click any valid IP cell, regardless of ping status: HTTP, HTTPS, SSH, Telnet, RDP, SMB, Custom Port, Continuous Ping, Copy IP Address.
- Custom port: validate 1–65535 and allow HTTP/HTTPS/SSH as appropriate.
- Use native Windows handlers, validated IP addresses, direct process arguments, no shell interpolation. Report unavailable clients gracefully.
- No traceroute, automatic port scanning, payment processing, tracking or automatic network requests. Donation QR images are static About-dialog assets.
- Preserve manual scan, startup silence, optional 15-second full discovery, 1-second promoted-host polling, two consecutive discovery misses for demotion, and Stop behavior.
- Single portable Windows AMD64 GUI executable; MIT license; no new external runtime.
- Integer release version 3, tag v3, window title LAN Monitor — Version 3, artifact lanmonitor-v3.exe.

## Gates
Audit source and Win32 API usage; add tests for IP cell hit testing, address and port validation, and command construction. Run tests, static analysis and Windows build. Update README and changelog. Preserve previous releases. After automated tests, audit, build and required CWAL approvals, publish v3 as a GitHub prerelease with lanmonitor-v3.exe, SHA256SUMS.txt and release notes for user download and Windows acceptance. STOP at AWAITING_WINDOWS_ACCEPTANCE. Promote to stable only after documented Windows acceptance and explicit human approval. If corrections are needed, rebuild and re-test the corrected candidate; preserve evidence and previous stable releases. Report commit, checks, size, checksum and download URL. Do not claim unperformed runtime testing.
