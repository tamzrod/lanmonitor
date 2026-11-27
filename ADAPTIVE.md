# Adaptive monitoring

Monitor mode is off at startup. Scan runs a full /24 sweep even when monitoring is off. Enabling monitor mode starts discovery immediately, then every 15 seconds. Promoted hosts receive active probes every second. Each round launches one goroutine per target, with per-IP locking to prevent overlapping probes to the same address. A manual scan requested during discovery queues one additional sweep. Stop disables timers and cancels pending work; outstanding synchronous ICMP calls may take 500 ms or longer to finish.

Discovery success promotes and resets discovery misses. Two consecutive discovery misses demote. Active misses change status but do not count toward demotion. Discovery continues to include demoted hosts. Orange indicates suspect promoted hosts. Subnet editing is locked while monitoring. Disable monitoring before changing subnet.

Build using BUILD.md; its older polling description applies to main only. Run go test ./... for platform-independent state tests. Windows build and live acceptance are pending. Verify startup silence, 15-second discovery, 1-second active probes, manual scan, two-miss demotion, recovery, cancellation and no overlapping per-IP requests with packet capture. Actual scheduling and ICMP timing depend on Windows and network conditions. Full parallel sweeps may create bursts of up to 254 probes; use only on authorized networks.

Known limitations: no persistent history, MAC discovery, packet-loss statistics or per-cell RBE rendering yet. GUI redraws after each round. Local ICMP handle creation failures are skipped, not counted as misses.
