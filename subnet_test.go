package main

import (
	"errors"
	"fmt"
	"testing"
)

func mustCommit(t *testing.T, m *subnetModel, text string) commitResult {
	t.Helper()
	r := m.Commit(text)
	if r.Err != nil {
		t.Fatalf("Commit(%q) error = %v", text, r.Err)
	}
	return r
}

func TestSubnetCommitNormalisesAndSetsSession(t *testing.T) {
	m := newSubnetModel()
	r := mustCommit(t, m, " 10.1.2.3/24 ")
	if r.Base != [4]byte{10, 1, 2, 0} {
		t.Errorf("base = %v, want [10 1 2 0]", r.Base)
	}
	if r.Canonical != "10.1.2.0/24" {
		t.Errorf("canonical = %q, want 10.1.2.0/24", r.Canonical)
	}
	if r.Session != 1 || !r.Changed {
		t.Errorf("session = %d, changed = %v; want 1, true", r.Session, r.Changed)
	}

	base, canonical, session, ok := m.Current()
	if !ok || base != r.Base || canonical != r.Canonical || session != r.Session {
		t.Errorf("Current() = %v, %q, %d, %v", base, canonical, session, ok)
	}
}

func TestSubnetCommitSameCanonicalKeepsSessionAndHosts(t *testing.T) {
	m := newSubnetModel()
	first := mustCommit(t, m, "10.1.2.0/24")
	if !m.Observe(first.Session, 5, true, true, 3) {
		t.Fatal("current-session result rejected")
	}

	r := mustCommit(t, m, "10.1.2.99/24")
	if r.Changed || r.Session != first.Session {
		t.Errorf("same subnet commit = changed %v, session %d; want false, %d", r.Changed, r.Session, first.Session)
	}
	if got := m.Host(5); got.state != stateResponding || got.rttMS != 3 {
		t.Errorf("same subnet commit changed the host map: %+v", got)
	}
}

func TestSubnetCommitInvalidKeepsAuthoritativeState(t *testing.T) {
	m := newSubnetModel()
	first := mustCommit(t, m, "192.168.1.0/24")
	if !m.Observe(first.Session, 2, true, true, 1) {
		t.Fatal("current-session result rejected")
	}

	cases := []struct {
		text string
		err  error
	}{
		{"", errSubnetSyntax},
		{"not a subnet", errSubnetSyntax},
		{"10.0.0.0/16", errSubnetSize},
		{"fd00::/24", errSubnetIPv4},
	}
	for _, tc := range cases {
		r := m.Commit(tc.text)
		if !errors.Is(r.Err, tc.err) {
			t.Errorf("Commit(%q) error = %v, want %v", tc.text, r.Err, tc.err)
		}
		if r.Session != first.Session || r.Canonical != first.Canonical || r.Changed {
			t.Errorf("Commit(%q) changed state: %+v", tc.text, r)
		}
	}
	if got := m.Host(2); got.state != stateResponding {
		t.Errorf("invalid commit cleared the host map: %+v", got)
	}
}

func TestSubnetChangeClearsHostMap(t *testing.T) {
	m := newSubnetModel()
	first := mustCommit(t, m, "192.168.1.0/24")
	if !m.Observe(first.Session, 5, true, true, 4) {
		t.Fatal("discovery reply rejected")
	}
	if !m.Observe(first.Session, 6, true, false, 0) {
		t.Fatal("discovery miss rejected")
	}

	r := mustCommit(t, m, "10.0.0.0/24")
	if !r.Changed || r.Session != first.Session+1 {
		t.Fatalf("switch = changed %v, session %d", r.Changed, r.Session)
	}
	for _, i := range []int{5, 6} {
		if got := m.Host(i); got != (hostRecord{}) {
			t.Errorf("host %d survived the subnet change: %+v", i, got)
		}
	}
}

// TestSubnetAToBToALateResultsAreRejected covers the case that matters most:
// returning to a subnet must not revive a round that was started before the
// first switch.
func TestSubnetAToBToALateResultsAreRejected(t *testing.T) {
	m := newSubnetModel()
	a1 := mustCommit(t, m, "192.168.1.0/24")
	if !m.Observe(a1.Session, 10, true, true, 2) {
		t.Fatal("A discovery reply rejected")
	}

	b := mustCommit(t, m, "10.0.0.0/24")
	if got := m.Host(10); got != (hostRecord{}) {
		t.Fatalf("A host survived the switch to B: %+v", got)
	}
	if m.Observe(a1.Session, 10, true, true, 2) {
		t.Fatal("late A result accepted while B is committed")
	}
	if !m.Observe(b.Session, 10, true, true, 3) {
		t.Fatal("current B result rejected")
	}

	a2 := mustCommit(t, m, "192.168.1.0/24")
	if a2.Session != b.Session+1 || !a2.Changed {
		t.Fatalf("return to A = session %d, changed %v", a2.Session, a2.Changed)
	}
	if m.Observe(a1.Session, 10, true, true, 2) {
		t.Fatal("old A result accepted after returning to A")
	}
	if m.Observe(b.Session, 10, true, true, 3) {
		t.Fatal("B result accepted after returning to A")
	}
	if !m.Observe(a2.Session, 10, true, true, 4) {
		t.Fatal("current A result rejected after returning to A")
	}
	if got := m.Host(10); got.rttMS != 4 || got.state != stateResponding {
		t.Errorf("current A result not applied: %+v", got)
	}
}

func TestSubnetObserveRequiresCurrentSessionAndPromotion(t *testing.T) {
	m := newSubnetModel()
	first := mustCommit(t, m, "192.168.1.0/24")

	for _, i := range []int{0, hostLast + 1} {
		if m.Observe(first.Session, i, true, true, 1) {
			t.Errorf("out-of-range host %d accepted", i)
		}
	}
	if m.Observe(first.Session, 4, false, true, 1) {
		t.Error("active result accepted for an unpromoted host")
	}
	if !m.Observe(first.Session, 3, true, true, 5) {
		t.Fatal("discovery reply rejected")
	}
	if !m.Observe(first.Session, 3, false, false, 0) {
		t.Fatal("active miss rejected for a promoted host")
	}
	if got := m.Host(3); got.state != stateSuspect || !got.promoted {
		t.Errorf("active miss state = %+v, want suspect and promoted", got)
	}
}

func TestSubnetTargetsFollowCommittedMap(t *testing.T) {
	m := newSubnetModel()
	first := mustCommit(t, m, "10.0.0.0/24")

	all := m.Targets(true)
	if len(all) != hostLast || all[0] != hostFirst || all[len(all)-1] != hostLast {
		t.Fatalf("discovery targets = %d entries spanning %v", len(all), all)
	}
	if got := m.Targets(false); len(got) != 0 {
		t.Fatalf("active targets before promotion = %v", got)
	}

	m.Observe(first.Session, 7, true, true, 1)
	m.Observe(first.Session, 9, true, true, 1)
	if got := m.Targets(false); fmt.Sprint(got) != fmt.Sprint([]int{7, 9}) {
		t.Errorf("active targets = %v, want [7 9]", got)
	}
}

func TestSubnetControllerManualEntry(t *testing.T) {
	c := newSubnetController()
	c.SetText(" 10.0.0.7/24 ")

	r := c.CommitPending()
	if r.Err != nil || r.Canonical != "10.0.0.0/24" || !r.Changed || r.Session != 1 {
		t.Fatalf("manual commit = %+v", r)
	}
	if got := c.Text(); got != "10.0.0.0/24" {
		t.Errorf("pending text = %q, want the canonical CIDR", got)
	}
	if _, canonical, _, ok := c.Model().Current(); !ok || canonical != "10.0.0.0/24" {
		t.Errorf("committed subnet = %q, %v", canonical, ok)
	}
}

func TestSubnetControllerInvalidManualEntryIsNotCommitted(t *testing.T) {
	c := newSubnetController()
	first := c.Select("192.168.1.0/24")

	c.SetText("10.0.0.0/16")
	r := c.CommitPending()
	if !errors.Is(r.Err, errSubnetSize) {
		t.Fatalf("invalid manual entry error = %v", r.Err)
	}
	if got := c.Text(); got != "10.0.0.0/16" {
		t.Errorf("invalid text was replaced with %q", got)
	}
	if _, canonical, session, _ := c.Model().Current(); canonical != first.Canonical || session != first.Session {
		t.Errorf("invalid manual entry changed the committed subnet to %q session %d", canonical, session)
	}
}

// TestSubnetControllerScanAndMonitorCommitPending pins that both explicit
// actions use the same commit path and that a repeated action does not
// invalidate the running session.
func TestSubnetControllerScanAndMonitorCommitPending(t *testing.T) {
	for _, action := range []string{"scan", "monitor"} {
		t.Run(action, func(t *testing.T) {
			c := newSubnetController()
			first := c.Select("192.168.1.0/24")

			c.SetText("10.0.0.7/24")
			r := c.CommitPending()
			if r.Err != nil || r.Canonical != "10.0.0.0/24" || !r.Changed {
				t.Fatalf("commit before %s = %+v", action, r)
			}
			if r.Session != first.Session+1 {
				t.Errorf("session = %d, want %d", r.Session, first.Session+1)
			}
			if got := c.Text(); got != "10.0.0.0/24" {
				t.Errorf("pending text = %q", got)
			}

			again := c.CommitPending()
			if again.Err != nil || again.Changed || again.Session != r.Session {
				t.Errorf("repeated %s commit = %+v, want no change", action, again)
			}
		})
	}
}

func TestSubnetControllerAToBToASwitching(t *testing.T) {
	c := newSubnetController()
	a1 := c.Select("192.168.1.0/24")
	b := c.Select("10.0.0.0/24")
	a2 := c.Select("192.168.1.0/24")

	if a1.Session != 1 || b.Session != 2 || a2.Session != 3 {
		t.Fatalf("sessions = %d, %d, %d; want 1, 2, 3", a1.Session, b.Session, a2.Session)
	}
	if !b.Changed || !a2.Changed {
		t.Errorf("switch flags = B %v, A2 %v; want both true", b.Changed, a2.Changed)
	}
	if got := c.Text(); got != "192.168.1.0/24" {
		t.Errorf("pending text = %q, want the returned subnet", got)
	}
	if c.Model().Observe(a1.Session, 4, true, true, 1) {
		t.Error("A1 result accepted after A-to-B-to-A")
	}
	if !c.Model().Observe(a2.Session, 4, true, true, 1) {
		t.Error("A2 result rejected")
	}
}

func TestSubnetControllerCancelDropRestoresTypedText(t *testing.T) {
	c := newSubnetController()
	c.SetText("172.16.4.0/24")
	c.BeginDrop(c.Text())

	if got := c.CancelDrop(); got != "172.16.4.0/24" {
		t.Errorf("CancelDrop() = %q, want the pre-drop text", got)
	}
	if c.Model().Session() != 0 {
		t.Errorf("cancel changed the session to %d", c.Model().Session())
	}
	if _, _, _, ok := c.Model().Current(); ok {
		t.Error("cancel committed a subnet")
	}
}

// TestSubnetControllerCancelAfterSelectionKeepsCommittedSubnet reproduces the
// reported acceptance failure: the list shows the newly selected subnet, then
// CBN_SELENDCANCEL arrives and used to restore the old text, so Scan and
// Monitor reverted to the previous subnet.
func TestSubnetControllerCancelAfterSelectionKeepsCommittedSubnet(t *testing.T) {
	c := newSubnetController()
	a := c.Select("192.168.1.0/24")

	c.BeginDrop(c.Text())
	b := c.Select("10.0.0.0/24")
	if got := c.Text(); got != b.Canonical {
		t.Fatalf("selected text = %q, want %q", got, b.Canonical)
	}

	if got := c.CancelDrop(); got != b.Canonical {
		t.Fatalf("CancelDrop() = %q, want the committed %q", got, b.Canonical)
	}

	r := c.CommitPending()
	if r.Err != nil || r.Canonical != b.Canonical {
		t.Fatalf("Scan after cancel committed %+v, want %q", r, b.Canonical)
	}
	if r.Session != b.Session {
		t.Errorf("Scan after cancel session = %d, want %d", r.Session, b.Session)
	}
	if _, canonical, _, _ := c.Model().Current(); canonical != b.Canonical {
		t.Errorf("committed subnet after cancel = %q, want %q", canonical, b.Canonical)
	}
	if a.Session == b.Session {
		t.Error("selection did not start a new session")
	}
}

func TestSubnetControllerCancelBeforeLateSelectionCommit(t *testing.T) {
	c := newSubnetController()
	a := c.Select("192.168.1.0/24")

	// Model CBN_SELENDCANCEL being processed before the posted CBN_SELENDOK
	// commit: the cancellation restores the pre-drop text, then the captured
	// selection still commits and wins.
	c.BeginDrop(c.Text())
	if got := c.CancelDrop(); got != a.Canonical {
		t.Fatalf("CancelDrop() = %q, want %q", got, a.Canonical)
	}
	b := c.Select("10.0.0.0/24")
	if got := c.Text(); got != b.Canonical {
		t.Fatalf("late selection text = %q, want %q", got, b.Canonical)
	}
	r := c.CommitPending()
	if r.Err != nil || r.Canonical != b.Canonical || r.Session != b.Session {
		t.Errorf("late selection commit = %+v, want %q", r, b.Canonical)
	}
}

func TestSubnetControllerRefreshPreservesPendingAndState(t *testing.T) {
	c := newSubnetController()
	a := c.Select("192.168.1.0/24")
	if !c.Model().Observe(a.Session, 5, true, true, 7) {
		t.Fatal("discovery reply rejected")
	}

	c.SetText("10.9.9.0/24")
	if got := c.Refresh(); got != "10.9.9.0/24" {
		t.Errorf("Refresh() = %q, want the typed text", got)
	}
	if c.Model().Session() != a.Session {
		t.Errorf("refresh changed the session to %d", c.Model().Session())
	}
	if _, canonical, _, _ := c.Model().Current(); canonical != a.Canonical {
		t.Errorf("refresh changed the committed subnet to %q", canonical)
	}
	if got := c.Model().Host(5); got.state != stateResponding || got.rttMS != 7 {
		t.Errorf("refresh changed the host map: %+v", got)
	}

	r := c.CommitPending()
	if r.Canonical != "10.9.9.0/24" || r.Session != a.Session+1 {
		t.Errorf("commit after refresh = %+v", r)
	}
	if got := c.Model().Host(5); got != (hostRecord{}) {
		t.Errorf("subnet change after refresh kept a stale host: %+v", got)
	}
}

func TestSubnetControllerRefreshFallsBackToCommittedText(t *testing.T) {
	c := newSubnetController()
	a := c.Select("10.0.0.0/24")
	c.SetText("   ")
	if got := c.Refresh(); got != a.Canonical {
		t.Errorf("Refresh() = %q, want %q", got, a.Canonical)
	}
}

func TestSubnetControllerTimerCommitIgnoresInvalidText(t *testing.T) {
	c := newSubnetController()
	a := c.Select("192.168.1.0/24")

	for _, text := range []string{"", "10.0.0.", "10.0.0.0/16", "not a subnet"} {
		c.SetText(text)
		if r, ok := c.CommitPendingIfValid(); ok {
			t.Errorf("CommitPendingIfValid(%q) = %+v, want no commit", text, r)
		}
		if c.Model().Session() != a.Session {
			t.Errorf("CommitPendingIfValid(%q) changed the session", text)
		}
	}

	c.SetText("10.5.0.9/24")
	r, ok := c.CommitPendingIfValid()
	if !ok || r.Canonical != "10.5.0.0/24" || r.Session != a.Session+1 {
		t.Errorf("valid timer commit = %+v, ok %v", r, ok)
	}
}

func TestResolveSubnetInput(t *testing.T) {
	candidates := []networkCandidate{
		{Name: "Ethernet", Kind: adapterEthernet, CIDR: "192.168.1.0/24"},
		{Name: "Tailscale", Kind: adapterVPN, CIDR: "100.64.1.0/24"},
	}
	cases := []struct {
		in   string
		want string
	}{
		{candidates[0].Label(), "192.168.1.0/24"},
		{candidates[1].Label(), "100.64.1.0/24"},
		{" 10.0.0.5/24 ", "10.0.0.5/24"},
		{"10.0.0.0/16", "10.0.0.0/16"},
		{"", ""},
		{"nonsense", "nonsense"},
	}
	for _, tc := range cases {
		if got := resolveSubnetInput(tc.in, candidates); got != tc.want {
			t.Errorf("resolveSubnetInput(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	c := newSubnetController()
	c.SetText(candidates[0].Label())
	c.SetText(resolveSubnetInput(c.Text(), candidates))
	r := c.CommitPending()
	if r.Err != nil || r.Canonical != candidates[0].CIDR {
		t.Errorf("label commit = %+v, want %q", r, candidates[0].CIDR)
	}
}
