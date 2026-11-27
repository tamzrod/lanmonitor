package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseSubnet(t *testing.T) {
	tests := []struct {
		in        string
		wantBase  [4]byte
		wantCanon string
		wantErr   error
	}{
		{"192.168.1.0/24", [4]byte{192, 168, 1, 0}, "192.168.1.0/24", nil},
		{"  10.0.0.0/24  ", [4]byte{10, 0, 0, 0}, "10.0.0.0/24", nil},
		{"10.1.2.3/24", [4]byte{10, 1, 2, 0}, "10.1.2.0/24", nil},
		{"172.16.0.0/24", [4]byte{172, 16, 0, 0}, "172.16.0.0/24", nil},
		{"", [4]byte{}, "", errSubnetSyntax},
		{"   ", [4]byte{}, "", errSubnetSyntax},
		{"not-a-subnet", [4]byte{}, "", errSubnetSyntax},
		{"192.168.1.0", [4]byte{}, "", errSubnetSyntax},
		{"192.168.1.0/33", [4]byte{}, "", errSubnetSyntax},
		{"10.0.0.0/16", [4]byte{}, "", errSubnetSize},
		{"10.0.0.0/25", [4]byte{}, "", errSubnetSize},
		{"10.0.0.0/32", [4]byte{}, "", errSubnetSize},
		{"fd00::/24", [4]byte{}, "", errSubnetIPv4},
		{"fd00::/64", [4]byte{}, "", errSubnetIPv4},
	}
	for _, tc := range tests {
		base, canon, err := parseSubnet(tc.in)
		if err != tc.wantErr {
			t.Errorf("parseSubnet(%q) error = %v, want %v", tc.in, err, tc.wantErr)
			continue
		}
		if base != tc.wantBase || canon != tc.wantCanon {
			t.Errorf("parseSubnet(%q) = %v, %q; want %v, %q", tc.in, base, canon, tc.wantBase, tc.wantCanon)
		}
	}
}

func TestHostAddr(t *testing.T) {
	base := [4]byte{192, 168, 1, 0}
	for _, i := range []int{1, 2, 100, 254} {
		got := hostAddr(base, i)
		if want := [4]byte{192, 168, 1, byte(i)}; got != want {
			t.Errorf("hostAddr(%v, %d) = %v, want %v", base, i, got, want)
		}
	}
}

func TestCellSlot(t *testing.T) {
	tests := []struct {
		host int
		x, y int
	}{
		{1, gridX, gridY},
		{16, gridX + 15*cellWidth, gridY},
		{17, gridX, gridY + cellHeight},
		{254, gridX + 13*cellWidth, gridY + 15*cellHeight},
	}
	for _, tc := range tests {
		x, y := cellSlot(tc.host)
		if x != tc.x || y != tc.y {
			t.Errorf("cellSlot(%d) = (%d,%d), want (%d,%d)", tc.host, x, y, tc.x, tc.y)
		}
	}
}

func TestGridFitsClientArea(t *testing.T) {
	for _, host := range []int{hostFirst, hostLast} {
		x, y := cellSlot(host)
		if x+cellFillWidth > clientWidth || y+cellFillHeight > clientHeight {
			t.Errorf("host %d at (%d,%d) overflows %dx%d client area", host, x, y, clientWidth, clientHeight)
		}
	}
}

func TestHostAt(t *testing.T) {
	tests := []struct {
		name   string
		x, y   int
		want   int
		wantOK bool
	}{
		{"first cell", gridX, gridY, 1, true},
		{"first cell middle", gridX + cellWidth/2, gridY + cellHeight/2, 1, true},
		{"last column first row", gridX + 15*cellWidth, gridY, 16, true},
		{"second row first column", gridX, gridY + cellHeight, 17, true},
		{"last host", gridX + 13*cellWidth, gridY + 15*cellHeight, 254, true},
		{"right of last column", gridX + 16*cellWidth, gridY, 0, false},
		{"far right", clientWidth - 1, gridY, 0, false},
		{"below grid", gridX, gridY + 16*cellHeight, 0, false},
		{"left of grid", gridX - 1, gridY, 0, false},
		{"above grid", gridX, gridY - 1, 0, false},
		{"unused slot 255", gridX + 14*cellWidth, gridY + 15*cellHeight, 0, false},
		{"unused slot 256", gridX + 15*cellWidth, gridY + 15*cellHeight, 0, false},
	}
	for _, tc := range tests {
		got, ok := hostAt(tc.x, tc.y)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: hostAt(%d,%d) = %d,%v; want %d,%v", tc.name, tc.x, tc.y, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestHostAtRoundTrip checks every painted cell maps back to its own host.
func TestHostAtRoundTrip(t *testing.T) {
	for host := hostFirst; host <= hostLast; host++ {
		x, y := cellSlot(host)
		got, ok := hostAt(x+cellFillWidth/2, y+cellFillHeight/2)
		if !ok || got != host {
			t.Fatalf("host %d at (%d,%d) hit-tested as %d,%v", host, x, y, got, ok)
		}
	}
}

func TestObservePromotesOnDiscoveryReply(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 3)
	if !h.promoted || !h.seen || h.state != stateResponding || h.rttMS != 3 || h.misses != 0 {
		t.Fatalf("after discovery reply: %+v", h)
	}
}

func TestObserveNeverSeenHostIsNoResponse(t *testing.T) {
	var h hostRecord
	h.observe(true, false, 0)
	if h.promoted || h.state != stateNoResponse || h.misses != 0 {
		t.Fatalf("never seen host: %+v", h)
	}
}

func TestObserveOneDiscoveryMissIsSuspect(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	h.observe(true, false, 0)
	if !h.promoted || h.misses != 1 || h.state != stateSuspect {
		t.Fatalf("after one discovery miss: %+v", h)
	}
}

func TestObserveTwoDiscoveryMissesDemote(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	h.observe(true, false, 0)
	h.observe(true, false, 0)
	if h.promoted || h.state != stateLost || h.misses != 0 {
		t.Fatalf("after two discovery misses: %+v", h)
	}
}

func TestObserveActiveMissDoesNotDemote(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	for i := 0; i < 10; i++ {
		h.observe(false, false, 0)
	}
	if !h.promoted || h.misses != 0 {
		t.Fatalf("active misses demoted the host: %+v", h)
	}
	if h.state != stateSuspect {
		t.Fatalf("active miss state = %v, want %v", h.state, stateSuspect)
	}
}

func TestObserveActiveReplyRestoresResponding(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	h.observe(false, false, 0)
	h.observe(false, true, 7)
	if h.state != stateResponding || h.rttMS != 7 || !h.promoted {
		t.Fatalf("after active reply: %+v", h)
	}
}

// TestObserveMissesMustBeConsecutive pins the "two consecutive discovery misses"
// rule: a reply in between resets the counter.
func TestObserveMissesMustBeConsecutive(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	for i := 0; i < 5; i++ {
		h.observe(true, false, 0)
		h.observe(true, true, 1)
	}
	if !h.promoted || h.misses != 0 {
		t.Fatalf("interleaved misses demoted the host: %+v", h)
	}
}

func TestObserveRepromotesAfterRecovery(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	h.observe(true, false, 0)
	h.observe(true, false, 0)
	if h.promoted {
		t.Fatalf("host not demoted: %+v", h)
	}
	h.observe(true, true, 4)
	if !h.promoted || h.state != stateResponding || h.misses != 0 {
		t.Fatalf("host not re-promoted: %+v", h)
	}
}

// TestObserveDemotedHostStaysLost covers a demoted host that keeps missing: it
// must not drift back to "never seen" and must not regain promotion.
func TestObserveDemotedHostStaysLost(t *testing.T) {
	var h hostRecord
	h.observe(true, true, 1)
	h.observe(true, false, 0)
	h.observe(true, false, 0)
	for i := 0; i < 5; i++ {
		h.observe(true, false, 0)
	}
	if h.promoted || h.state != stateLost {
		t.Fatalf("demoted host: %+v", h)
	}
}

func TestTargets(t *testing.T) {
	states := make([]hostRecord, hostLast+1)

	all := targets(true, states)
	if len(all) != hostLast {
		t.Fatalf("discovery targets = %d, want %d", len(all), hostLast)
	}
	if all[0] != hostFirst || all[len(all)-1] != hostLast {
		t.Fatalf("discovery targets span %d..%d", all[0], all[len(all)-1])
	}
	if got := targets(false, states); len(got) != 0 {
		t.Fatalf("active targets without promotion = %v, want none", got)
	}

	states[1].observe(true, true, 1)
	states[7].observe(true, true, 2)
	states[254].observe(true, true, 3)
	states[9].observe(true, true, 4)
	states[9].observe(true, false, 0)
	states[9].observe(true, false, 0)

	got := targets(false, states)
	want := []int{1, 7, 254}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("active targets = %v, want %v", got, want)
	}
	if len(targets(true, states)) != hostLast {
		t.Fatal("discovery targets changed with promotion state")
	}
}

func TestFormatStatus(t *testing.T) {
	var rec hostRecord
	rec.observe(true, true, 12)

	got := formatStatus("10.0.0.0/24", 7, rec)
	for _, want := range []string{"10.0.0.0/24", "Host .7", "Responding", "12 ms", "Promoted"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatStatus = %q, missing %q", got, want)
		}
	}

	var quiet hostRecord
	if got := formatStatus("", 3, quiet); !strings.Contains(got, "(no subnet)") || !strings.Contains(got, "Not scanned") {
		t.Errorf("formatStatus for empty subnet = %q", got)
	}
	if strings.Contains(formatStatus("10.0.0.0/24", 3, quiet), "Promoted") {
		t.Error("unpromoted host reported as promoted")
	}

	rec.observe(true, false, 0)
	if got := formatStatus("10.0.0.0/24", 7, rec); !strings.Contains(got, "Suspect") {
		t.Errorf("suspect status = %q", got)
	}
}

// TestMonitoringCadence pins the documented schedule: a 15 s discovery sweep and
// 1 s probes of promoted hosts, with demotion after two missed sweeps.
func TestMonitoringCadence(t *testing.T) {
	if discoveryIntervalMS != 15000 {
		t.Errorf("discovery interval = %d ms, want 15000", discoveryIntervalMS)
	}
	if activeIntervalMS != 1000 {
		t.Errorf("active interval = %d ms, want 1000", activeIntervalMS)
	}
	if demotionMisses != 2 {
		t.Errorf("demotion threshold = %d, want 2", demotionMisses)
	}
	if pingTimeoutMS >= activeIntervalMS {
		t.Errorf("probe timeout %d ms must stay below the active interval %d ms", pingTimeoutMS, activeIntervalMS)
	}
}

func TestStateColorsAreDistinct(t *testing.T) {
	states := []hostState{stateUnscanned, stateResponding, stateLost, stateNoResponse, stateSuspect}
	seen := map[uintptr]hostState{}
	for _, st := range states {
		c := stateColor(st)
		if prev, dup := seen[c]; dup {
			t.Errorf("states %v and %v share colour %#x", prev, st, c)
		}
		seen[c] = st
	}
}

func TestVersionIdentity(t *testing.T) {
	if appVersion != 2 {
		t.Errorf("appVersion = %d, want 2", appVersion)
	}
	if appName != "LAN Monitor" {
		t.Errorf("appName = %q", appName)
	}
	title := fmt.Sprintf("%s — Version %d", appName, appVersion)
	if title != "LAN Monitor — Version 2" {
		t.Errorf("title = %q", title)
	}
}
