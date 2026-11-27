# Adaptive monitoring

This document describes how LAN Monitor decides what to probe and when. It is
the reference for the acceptance checklist in [BUILD.md](BUILD.md).

## Two kinds of round

| Round | Started by | Targets | Period |
| --- | --- | --- | --- |
| Discovery | **Scan**, **Monitor mode**, or the discovery timer | all 254 usable addresses | 15 s while monitoring |
| Active | the active timer | promoted hosts only | 1 s while monitoring |

Monitoring is off at startup and stays off until **Monitor mode** is ticked. A
manual **Scan** runs a discovery round without enabling monitoring.

Enabling monitoring starts a discovery round immediately, then arms two Win32
timers: 15000 ms for discovery and 1000 ms for active rounds. Disabling
monitoring kills both timers.

## Promotion and demotion

Each address holds a small record: state, last round-trip time, whether it has
ever answered, whether it is promoted, and a count of consecutive discovery
misses.

```
discovery reply   -> promoted, misses = 0, state = Responding
discovery miss    -> misses++  (only while promoted)
                     misses >= 2  -> demoted, misses = 0, state = Lost response
                     misses == 1  -> state = Suspect
active reply      -> state = Responding, round-trip time updated
active miss       -> state = Suspect (promoted) or Lost response (seen before)
```

Consequences:

* Active misses never demote. A single dropped echo on a busy link must not stop
  the one-second polling that exists to detect exactly that kind of blip.
* Demotion is driven by discovery only, so a host needs two full sweeps (about
  30 seconds) to be dropped.
* A demoted host stays in every discovery sweep, so recovery needs no user
  action: the next sweep that reaches it promotes it again.
* A manual **Scan** promotes whatever answers, because it is a discovery round.

## Concurrency

* One goroutine per target, so a sweep of 254 addresses runs in parallel with a
  short per-probe timeout (500 ms) rather than 254 sequential waits.
* Each address has its own mutex. A discovery round and an active round can
  therefore overlap without probing the same address twice at the same time, and
  a late result cannot overwrite a newer one.
* Only one discovery round runs at a time. If a sweep is requested while one is
  still draining, one additional sweep is queued and started when the running
  round finishes; further requests are folded into that single queued sweep.
* Active rounds are independent of discovery rounds and of each other: a slow
  probe does not hold up the one-second schedule beyond skipping a tick.
* The IP map is protected by one mutex; the UI reads a snapshot while painting.

## Cancellation

Every round captures the cancellation token that is current when it starts.

* **Stop**, unticking **Monitor mode** and closing the window close the current
  token, which cancels every round that is still draining.
* A cancelled round stops launching probes. Probes already inside
  `IcmpSendEcho` finish within their 500 ms timeout, so cancellation is bounded.
* The next round started afterwards receives a fresh token, which is why a round
  that is still draining cannot be resumed by a newer one.
* A cancelled round reports itself as cancelled, so the status line keeps
  "Stopped" instead of switching to "Scan complete".

## Stale results

Rounds carry two identifiers:

* a per-kind sequence number, so a completion message from a superseded round
  cannot clear the busy flag of the round that replaced it;
* the scan session from the authoritative subnet model. The session increases on
  every canonical subnet change, including A to B and back to A. A result from a
  round started before a change is discarded, even after the user returns to the
  same subnet.

When the canonical subnet changes, `applySubnetChange` closes the cancellation
token, invalidates both sequence numbers and clears the busy and queued flags.
The model clears the host map at the same time. A sweep already in flight stops
launching probes; probes already inside `IcmpSendEcho` finish within their
500 ms timeout and their results are dropped by the session check. The promoted
flag is re-checked under the model lock before a result is applied, so an address
that was demoted during the round is not updated by it.

## Status line

| Text | Meaning |
| --- | --- |
| `Ready - monitoring off` | Startup state, nothing has been probed |
| `Discovery scanning <subnet> ...` | A sweep is running |
| `Discovery running - one more sweep queued` | **Scan** pressed during a sweep |
| `Scan complete` | Sweep finished, monitoring off |
| `Monitoring - discovery complete, next sweep in 15 s` | Sweep finished, monitoring on |
| `Discovery failed - could not open an ICMP handle` | Every probe in the sweep failed to get an ICMP handle |
| `Stopped` | **Stop** pressed |
| `Network list refreshed - <n> network(s)` | **Refresh** rebuilt the adapter list |
| `No IPv4 networks detected - type a /24 manually` | No eligible adapter network was found |
| `Selected <subnet> - press Scan` | A dropdown entry was chosen while monitoring was off |
| `<subnet> \| Host .<n> \| <state> \| <rtt> ms \| Promoted` | Hover over a cell |

Hover text is only a report; it does not change monitoring state.

## Subnet changes

The committed subnet is the only value used by **Scan**, the discovery and
active timers, the hover status line and the context menu. The editable ComboBox
is input: its text is pending until an explicit action commits it. The committed
state lives in `subnet.go` and is independent of the transient ComboBox text.

Commit points:

* choosing a drop-down entry: the CIDR is captured from the list at
  `CBN_SELENDOK` and committed by a posted message; the edit text is never
  parsed for this path;
* **Scan**, **Monitor mode** and any context-menu command: the edit text, or a
  ComboBox label that maps back to its CIDR, is parsed and committed first;
* the 15 s discovery timer: a valid manually typed CIDR is committed before the
  sweep; invalid or partial text is ignored so monitoring continues on the
  committed subnet.

A successful commit of a different canonical subnet:

* increments the scan session;
* clears the host map;
* cancels rounds in flight and invalidates their completions and any queued
  sweep;
* rewrites the edit field with the canonical CIDR.

Committing the same canonical subnet is a no-op: the map, session and rounds are
kept. Selecting a network while monitoring starts a sweep immediately; selecting
one while monitoring is off only commits it and asks for **Scan**.

Cancelling the drop-down restores the pre-drop text unless a selection was
committed while the list was open. In that case the committed CIDR wins, so
cancelling can never restore the previous subnet. **Refresh** rebuilds the
adapter list without changing the committed subnet, the session or the host map;
it preserves pending text and maps a selected label back to its CIDR. **Stop** is
not required to change networks.

## Known behaviour worth knowing during acceptance

* A sweep is a burst of 254 echo requests; on a busy or fragile network this is
  visible in a packet capture and may briefly exceed an ICMP rate limit.
* Windows may suppress replies to a host that is in sleep, and firewalls may drop
  echo requests, so a demoted host is not necessarily offline.
* The first sweep after enabling monitoring starts immediately; the second starts
  15 seconds later, measured from the moment the timer was armed.
* No history, packet-loss statistics, MAC or hostname discovery, IPv6 or export.
