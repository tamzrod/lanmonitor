# Changelog

## v2 - unreleased

Adaptive monitoring. A full `/24` sweep every 15 seconds discovers hosts; hosts
that answer are then probed every second until two sweeps in a row miss them.

### Added

* **Monitor mode** check box that enables adaptive monitoring.
* Promotion on a discovery reply and demotion after two consecutive discovery
  misses, with automatic recovery on the next sweep that reaches the host.
* A `Suspect` state (orange) for a promoted host that missed its most recent
  probe, so a single dropped echo is visible without dropping the host.
* `ADAPTIVE.md`, describing the cadence, the state machine, concurrency,
  cancellation and stale-result handling.

### Changed

* The window title carries the version: `LAN Monitor — Version 2`.
* One round runs at a time per kind. A sweep requested during a sweep is queued
  once instead of being dropped or run in parallel.
* Status line reports the monitoring phase, queued sweeps and ICMP handle
  failures.

### Fixed

* **Stop** no longer lets a queued sweep start afterwards.
* Cancelling a round no longer clears the cancellation state for rounds that are
  still draining, so probes stop promptly instead of resuming after **Stop**.
* Results from a round started on a previous subnet are discarded instead of
  being written into the new map.
* A stale round completion can no longer clear the busy flag of the round that
  replaced it.
* An address demoted during a round is no longer updated by that round.
* A sweep in which every probe failed to obtain an ICMP handle is reported as a
  failure instead of looking like "no replies".

### Removed

* The fixed polling interval box; the adaptive schedule replaces it.

## v0.1.0

Initial portable release.

* Single portable Windows AMD64 executable, native Win32 GUI, no runtime
  dependencies.
* Manual **Scan** and **Stop**, fixed map of `.1`–`.254`, nothing scanned at
  startup, continuous polling off by default.
* Win32 fixes: string arguments stay reachable for the duration of each call,
  hit testing is bounds-checked, `IcmpSendEcho` sends a real payload, layout
  scales with the system DPI.
* Platform-independent logic split out with unit tests, plus Win32 ABI tests that
  run on Windows.
* GitHub Actions build and test workflow, `BUILD.md` verification and acceptance
  instructions.
