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
* the scan session, which is incremented whenever the subnet changes. A result
  from a round started on the previous subnet is discarded instead of being
  written into the new map.

The promoted flag is re-checked under the map lock before a result is applied, so
an address that was demoted during the round is not updated by it.

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

The network ComboBox stays editable while monitoring. Selecting a detected
network starts a sweep immediately; a manually typed CIDR is read by the next
**Scan** or the next 15-second sweep. A selection is committed from
`CBN_SELENDOK` through a posted message, so the edit field holds the CIDR before
`startDiscovery` reads it. Cancelling the drop-down restores the pre-drop text.
When the canonical subnet changes, `startDiscovery` uses `nextScanSession` to
increment the scan session and clears the map, so results from a round started
on the previous subnet are discarded. A sweep already in flight finishes on its
old subnet; a queued sweep picks up the new one. **Stop** is no longer required
to change networks.

## Known behaviour worth knowing during acceptance

* A sweep is a burst of 254 echo requests; on a busy or fragile network this is
  visible in a packet capture and may briefly exceed an ICMP rate limit.
* Windows may suppress replies to a host that is in sleep, and firewalls may drop
  echo requests, so a demoted host is not necessarily offline.
* The first sweep after enabling monitoring starts immediately; the second starts
  15 seconds later, measured from the moment the timer was armed.
* No history, packet-loss statistics, MAC or hostname discovery, IPv6 or export.
