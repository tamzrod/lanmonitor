package main

import (
	"errors"
	"fmt"
	"reflect"
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
	if appVersion != 3 {
		t.Errorf("appVersion = %d, want 3", appVersion)
	}
	if appName != "LAN Monitor" {
		t.Errorf("appName = %q", appName)
	}
	if title := windowTitle(); title != "LAN Monitor — Version 3" {
		t.Errorf("title = %q", title)
	}
}

func TestHostAddress(t *testing.T) {
	tests := []struct {
		subnet string
		host   int
		want   string
		wantOK bool
	}{
		{"192.168.1.0/24", 1, "192.168.1.1", true},
		{"192.168.1.0/24", 254, "192.168.1.254", true},
		{"10.1.2.3/24", 7, "10.1.2.7", true},
		{"192.168.1.0/24", 0, "", false},
		{"192.168.1.0/24", 255, "", false},
		{"", 1, "", false},
		{"10.0.0.0/16", 1, "", false},
	}
	for _, tc := range tests {
		got, err := hostAddress(tc.subnet, tc.host)
		if (err == nil) != tc.wantOK {
			t.Errorf("hostAddress(%q,%d) error = %v, wantOK %v", tc.subnet, tc.host, err, tc.wantOK)
			continue
		}
		if got != tc.want {
			t.Errorf("hostAddress(%q,%d) = %q, want %q", tc.subnet, tc.host, got, tc.want)
		}
	}
}

func TestValidateIPv4(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"192.168.1.1", "192.168.1.1", true},
		{"10.0.0.254", "10.0.0.254", true},
		{" 172.16.5.4 ", "172.16.5.4", true},
		{"0.0.0.0", "", false},
		{"0.0.0.1", "", false},
		{"255.255.255.255", "", false},
		{"224.0.0.1", "", false},
		{"239.255.255.255", "", false},
		{"192.168.1", "", false},
		{"192.168.1.256", "", false},
		{"192.168.1.1/24", "", false},
		{"fd00::1", "", false},
		{"::ffff:192.168.1.1", "", false},
		{"192.168.001.001", "", false},
		{"", "", false},
		{"not-an-ip", "", false},
	}
	for _, tc := range tests {
		got, err := validateIPv4(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("validateIPv4(%q) error = %v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if got != tc.want {
			t.Errorf("validateIPv4(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		in   string
		want uint16
		ok   bool
	}{
		{"1", 1, true},
		{"80", 80, true},
		{"65535", 65535, true},
		{" 8080 ", 8080, true},
		{"00080", 80, true},
		{"0", 0, false},
		{"65536", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"1.5", 0, false},
		{"abc", 0, false},
		{"", 0, false},
		{"   ", 0, false},
	}
	for _, tc := range tests {
		got, err := parsePort(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("parsePort(%q) error = %v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if got != tc.want {
			t.Errorf("parsePort(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeScheme(t *testing.T) {
	for _, in := range []string{"http", "HTTP", " Http "} {
		if got, err := normalizeScheme(in); err != nil || got != "http" {
			t.Errorf("normalizeScheme(%q) = %q,%v; want http,nil", in, got, err)
		}
	}
	for _, in := range []string{"https", "HTTPS"} {
		if got, err := normalizeScheme(in); err != nil || got != "https" {
			t.Errorf("normalizeScheme(%q) = %q,%v; want https,nil", in, got, err)
		}
	}
	for _, in := range []string{"ssh", "SSH"} {
		if got, err := normalizeScheme(in); err != nil || got != "ssh" {
			t.Errorf("normalizeScheme(%q) = %q,%v; want ssh,nil", in, got, err)
		}
	}
	for _, in := range []string{"ftp", "file", ""} {
		if _, err := normalizeScheme(in); !errors.Is(err, errInvalidScheme) {
			t.Errorf("normalizeScheme(%q) error = %v, want errInvalidScheme", in, err)
		}
	}
}

func TestBuildClientCommand(t *testing.T) {
	tests := []struct {
		name   string
		action clientAction
		ip     string
		custom customPort
		want   command
	}{
		{"http", actionHTTP, "192.168.1.5", customPort{}, command{Kind: launchShell, Target: "http://192.168.1.5/"}},
		{"https", actionHTTPS, "192.168.1.5", customPort{}, command{Kind: launchShell, Target: "https://192.168.1.5/"}},
		{"ssh", actionSSH, "192.168.1.5", customPort{}, command{Kind: launchProcess, Target: "ssh.exe", Args: []string{"192.168.1.5"}, NewConsole: true}},
		{"telnet", actionTelnet, "192.168.1.5", customPort{}, command{Kind: launchProcess, Target: "telnet.exe", Args: []string{"192.168.1.5"}, NewConsole: true}},
		{"rdp", actionRDP, "192.168.1.5", customPort{}, command{Kind: launchProcess, Target: "mstsc.exe", Args: []string{"/v:192.168.1.5"}}},
		{"smb", actionSMB, "192.168.1.5", customPort{}, command{Kind: launchProcess, Target: "explorer.exe", Args: []string{`\\192.168.1.5`}}},
		{"custom http", actionCustomPort, "192.168.1.5", customPort{Scheme: "http", Port: 8080}, command{Kind: launchShell, Target: "http://192.168.1.5:8080/"}},
		{"custom https", actionCustomPort, "192.168.1.5", customPort{Scheme: "https", Port: 8443}, command{Kind: launchShell, Target: "https://192.168.1.5:8443/"}},
		{"custom ssh", actionCustomPort, "192.168.1.5", customPort{Scheme: "ssh", Port: 2222}, command{Kind: launchProcess, Target: "ssh.exe", Args: []string{"-p", "2222", "192.168.1.5"}, NewConsole: true}},
		{"continuous ping", actionContinuousPing, "192.168.1.5", customPort{}, command{Kind: launchProcess, Target: "ping.exe", Args: []string{"-t", "192.168.1.5"}, NewConsole: true}},
		{"copy", actionCopyIP, "192.168.1.5", customPort{}, command{Kind: launchClipboard, Target: "192.168.1.5"}},
	}
	for _, tc := range tests {
		got, err := buildClientCommand(tc.action, tc.ip, tc.custom)
		if err != nil {
			t.Errorf("%s: buildClientCommand error = %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestBuildCustomPortCommand(t *testing.T) {
	tests := []struct {
		scheme string
		port   uint16
		want   command
	}{
		{"http", 1, command{Kind: launchShell, Target: "http://10.0.0.1:1/"}},
		{"https", 65535, command{Kind: launchShell, Target: "https://10.0.0.1:65535/"}},
		{"ssh", 22, command{Kind: launchProcess, Target: "ssh.exe", Args: []string{"-p", "22", "10.0.0.1"}, NewConsole: true}},
	}
	for _, tc := range tests {
		got, err := buildCustomPortCommand("10.0.0.1", tc.scheme, tc.port)
		if err != nil {
			t.Errorf("buildCustomPortCommand(%q,%d) error = %v", tc.scheme, tc.port, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("buildCustomPortCommand(%q,%d) = %+v, want %+v", tc.scheme, tc.port, got, tc.want)
		}
	}
	if _, err := buildCustomPortCommand("10.0.0.1", "ftp", 21); !errors.Is(err, errInvalidScheme) {
		t.Errorf("ftp scheme error = %v, want errInvalidScheme", err)
	}
	if _, err := buildCustomPortCommand("10.0.0.1", "http", 0); !errors.Is(err, errInvalidPort) {
		t.Errorf("port 0 error = %v, want errInvalidPort", err)
	}
	if _, err := buildCustomPortCommand("bad", "http", 80); !errors.Is(err, errInvalidIP) {
		t.Errorf("bad address error = %v, want errInvalidIP", err)
	}
}

func TestBuildClientCommandRejectsInvalidAddress(t *testing.T) {
	for _, ip := range []string{"", "999.1.1.1", "192.168.1.1/24", "fd00::1", "0.0.0.0", "255.255.255.255"} {
		if _, err := buildClientCommand(actionHTTP, ip, customPort{}); !errors.Is(err, errInvalidIP) {
			t.Errorf("buildClientCommand(%q) error = %v, want errInvalidIP", ip, err)
		}
	}
	if _, err := buildClientCommand(clientAction(99), "192.168.1.1", customPort{}); !errors.Is(err, errUnknownAction) {
		t.Errorf("unknown action error = %v, want errUnknownAction", err)
	}
}

func TestCommandsHaveNoShellSyntax(t *testing.T) {
	for _, item := range contextMenuItems {
		custom := customPort{}
		if item.Action == actionCustomPort {
			custom = customPort{Scheme: "https", Port: 8443}
		}
		c, err := buildClientCommand(item.Action, "192.168.1.5", custom)
		if err != nil {
			t.Fatalf("%s: %v", item.Label, err)
		}
		parts := append([]string{c.Target}, c.Args...)
		for _, part := range parts {
			for _, bad := range []string{"cmd", "/c", "&", "|", ";", "`", "$", ">", "<", "\n", "\r"} {
				if strings.Contains(part, bad) {
					t.Errorf("%s: %q contains shell syntax %q", item.Label, part, bad)
				}
			}
		}
	}
}

func TestContextMenuContract(t *testing.T) {
	want := []struct {
		label  string
		action clientAction
	}{
		{"HTTP", actionHTTP},
		{"HTTPS", actionHTTPS},
		{"SSH", actionSSH},
		{"Telnet", actionTelnet},
		{"RDP", actionRDP},
		{"SMB", actionSMB},
		{"Custom Port", actionCustomPort},
		{"Continuous Ping", actionContinuousPing},
		{"Copy IP Address", actionCopyIP},
	}
	if len(contextMenuItems) != len(want) {
		t.Fatalf("context menu has %d items, want %d", len(contextMenuItems), len(want))
	}
	seen := map[int]bool{}
	for i, item := range contextMenuItems {
		if item.Label != want[i].label || item.Action != want[i].action {
			t.Errorf("item %d = %q/%v, want %q/%v", i, item.Label, item.Action, want[i].label, want[i].action)
		}
		if seen[item.ID] {
			t.Errorf("duplicate context menu ID %d", item.ID)
		}
		seen[item.ID] = true
	}
	for _, item := range contextMenuItems {
		lower := strings.ToLower(item.Label)
		for _, bad := range []string{"traceroute", "tracert", "scan", "donate", "payment", "qr"} {
			if strings.Contains(lower, bad) {
				t.Errorf("context menu label %q contains forbidden feature %q", item.Label, bad)
			}
		}
	}
}

func TestContextActionLookup(t *testing.T) {
	for _, item := range contextMenuItems {
		got, ok := contextAction(item.ID)
		if !ok || got != item.Action {
			t.Errorf("contextAction(%d) = %v,%v; want %v,true", item.ID, got, ok, item.Action)
		}
	}
	if _, ok := contextAction(-1); ok {
		t.Error("contextAction(-1) succeeded")
	}
}

func TestAboutIdentity(t *testing.T) {
	if projectURL != "https://github.com/tamzrod/lanmonitor" {
		t.Errorf("projectURL = %q", projectURL)
	}
	if aboutQuote != "Gwapong Pilipino Taas Noo Kahit Kanino!" {
		t.Errorf("aboutQuote = %q", aboutQuote)
	}
	if got := windowTitle(); got != "LAN Monitor — Version 3" {
		t.Errorf("windowTitle() = %q", got)
	}
}

func TestUnavailableMessages(t *testing.T) {
	actions := []clientAction{
		actionHTTP, actionHTTPS, actionSSH, actionTelnet, actionRDP,
		actionSMB, actionCustomPort, actionContinuousPing, actionCopyIP,
	}
	for _, action := range actions {
		msg := unavailableMessage(action)
		if msg == "" {
			t.Errorf("unavailableMessage(%v) is empty", action)
			continue
		}
		if !strings.Contains(strings.ToLower(msg), "available") && !strings.Contains(msg, "Could not") {
			t.Errorf("unavailableMessage(%v) = %q", action, msg)
		}
	}
}

func TestContextMenuHitTestingEveryCell(t *testing.T) {
	for host := hostFirst; host <= hostLast; host++ {
		x, y := cellSlot(host)
		points := []struct{ x, y int }{
			{x, y},
			{x + cellFillWidth - 1, y},
			{x, y + cellFillHeight - 1},
			{x + cellFillWidth - 1, y + cellFillHeight - 1},
			{x + cellFillWidth/2, y + cellFillHeight/2},
		}
		for _, p := range points {
			got, ok := hostAt(p.x, p.y)
			if !ok || got != host {
				t.Fatalf("host %d hit test at (%d,%d) = %d,%v", host, p.x, p.y, got, ok)
			}
		}
	}
}

func TestHostAtRejectsOutsideGrid(t *testing.T) {
	tests := []struct{ x, y int }{
		{gridX - 1, gridY},
		{gridX, gridY - 1},
		{gridX + gridCols*cellWidth, gridY},
		{gridX, gridY + gridRows*cellHeight},
		{gridX + 14*cellWidth, gridY + 15*cellHeight},
		{gridX + 15*cellWidth, gridY + 15*cellHeight},
	}
	for _, tc := range tests {
		if got, ok := hostAt(tc.x, tc.y); ok {
			t.Errorf("hostAt(%d,%d) = %d, want miss", tc.x, tc.y, got)
		}
	}
}
