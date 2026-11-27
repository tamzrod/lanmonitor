package main

import (
	"net"
	"testing"
)

func testAddr(t *testing.T, cidr string) net.Addr {
	t.Helper()
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", cidr, err)
	}
	return &net.IPNet{IP: ip, Mask: n.Mask}
}

func TestClassifyAdapter(t *testing.T) {
	cases := []struct {
		name    string
		want    adapterKind
		include bool
	}{
		{"Ethernet", adapterEthernet, true},
		{"Ethernet 2", adapterEthernet, true},
		{"Local Area Connection", adapterEthernet, true},
		{"eth0", adapterEthernet, true},
		{"enp3s0", adapterEthernet, true},
		{"Wi-Fi", adapterWiFi, true},
		{"Wi-Fi 2", adapterWiFi, true},
		{"Wireless LAN adapter Wi-Fi", adapterWiFi, true},
		{"wlan0", adapterWiFi, true},
		{"wlp2s0", adapterWiFi, true},
		{"WireGuard Tunnel", adapterVPN, true},
		{"wg0", adapterVPN, true},
		{"OpenVPN TAP-Windows6", adapterVPN, true},
		{"OpenVPN Data Channel Offload", adapterVPN, true},
		{"tap0", adapterVPN, true},
		{"tun0", adapterVPN, true},
		{"Tailscale", adapterVPN, true},
		{"Tailscale Tunnel", adapterVPN, true},
		{"ZeroTier One", adapterVPN, true},
		{"Cloudflare WARP", adapterVPN, true},
		{"Docker Desktop", adapterOther, false},
		{"docker0", adapterOther, false},
		{"veth1234", adapterOther, false},
		{"br-1234", adapterOther, false},
		{"vEthernet (WSL (Hyper-V firewall))", adapterOther, false},
		{"vEthernet (Default Switch)", adapterOther, false},
		{"Hyper-V Virtual Ethernet Adapter", adapterOther, false},
		{"VMware Network Adapter VMnet1", adapterOther, false},
		{"VirtualBox Host-Only Network", adapterOther, false},
		{"Loopback Pseudo-Interface 1", adapterOther, false},
		{"Bluetooth Network Connection", adapterOther, false},
		{"Microsoft Wi-Fi Direct Virtual Adapter", adapterOther, false},
		{"Microsoft Hosted Network Virtual Adapter", adapterOther, false},
		{"Unknown Adapter", adapterOther, true},
	}
	for _, tc := range cases {
		got, include := classifyAdapter(tc.name)
		if got != tc.want || include != tc.include {
			t.Errorf("classifyAdapter(%q) = (%v, %v), want (%v, %v)",
				tc.name, got, include, tc.want, tc.include)
		}
	}
}

func TestCandidateSubnet(t *testing.T) {
	cases := []struct {
		addr string
		want string
		ok   bool
	}{
		{"192.168.1.5/24", "192.168.1.0/24", true},
		{"100.64.1.2/32", "100.64.1.0/24", true},
		{"10.0.5.23/16", "10.0.5.0/24", true},
		{"172.16.9.7/22", "172.16.9.0/24", true},
		{"0.0.0.1/24", "", false},
		{"127.0.0.1/8", "", false},
		{"224.0.0.1/24", "", false},
		{"::1/128", "", false},
		{"2001:db8::1/64", "", false},
	}
	for _, tc := range cases {
		got, ok := candidateSubnet(testAddr(t, tc.addr))
		if got != tc.want || ok != tc.ok {
			t.Errorf("candidateSubnet(%q) = (%q, %v), want (%q, %v)",
				tc.addr, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := candidateSubnet(nil); ok || got != "" {
		t.Errorf("candidateSubnet(nil) = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestNetworkCandidatesFilterAndOrder(t *testing.T) {
	addrs := func(cidr string) []net.Addr { return []net.Addr{testAddr(t, cidr)} }
	adapters := []adapterInfo{
		{Name: "Docker Desktop", Flags: net.FlagUp, Addrs: addrs("172.17.0.1/24")},
		{Name: "vEthernet (WSL)", Flags: net.FlagUp, Addrs: addrs("172.20.0.1/24")},
		{Name: "vEthernet (Default Switch)", Flags: net.FlagUp, Addrs: addrs("172.21.0.1/24")},
		{Name: "VMware Network Adapter VMnet1", Flags: net.FlagUp, Addrs: addrs("192.168.56.1/24")},
		{Name: "VirtualBox Host-Only Network", Flags: net.FlagUp, Addrs: addrs("192.168.57.1/24")},
		{Name: "Loopback Pseudo-Interface 1", Flags: net.FlagUp | net.FlagLoopback, Addrs: addrs("127.0.0.1/8")},
		{Name: "Down Ethernet", Flags: 0, Addrs: addrs("192.168.99.10/24")},
		{Name: "Ethernet", Flags: net.FlagUp, Addrs: addrs("192.168.1.10/24")},
		{Name: "Ethernet", Flags: net.FlagUp, Addrs: addrs("192.168.1.20/24")},
		{Name: "Wi-Fi", Flags: net.FlagUp, Addrs: addrs("192.168.50.10/24")},
		{Name: "Tailscale", Flags: net.FlagUp, Addrs: addrs("100.64.1.2/32")},
		{Name: "WireGuard Tunnel", Flags: net.FlagUp, Addrs: addrs("10.8.0.2/24")},
		{Name: "OpenVPN TAP-Windows6", Flags: net.FlagUp, Addrs: addrs("10.9.0.6/24")},
	}

	got := networkCandidates(adapters)
	wantCIDRs := []string{
		"192.168.1.0/24",
		"192.168.50.0/24",
		"10.9.0.0/24",
		"100.64.1.0/24",
		"10.8.0.0/24",
	}
	if len(got) != len(wantCIDRs) {
		t.Fatalf("networkCandidates returned %d candidates, want %d: %+v", len(got), len(wantCIDRs), got)
	}
	for i, want := range wantCIDRs {
		if got[i].CIDR != want {
			t.Errorf("candidate %d CIDR = %q, want %q", i, got[i].CIDR, want)
		}
	}
	for _, c := range got {
		switch c.CIDR {
		case "172.17.0.0/24", "172.20.0.0/24", "172.21.0.0/24",
			"192.168.56.0/24", "192.168.57.0/24", "192.168.99.0/24":
			t.Errorf("excluded or down adapter leaked into candidates: %+v", c)
		}
	}
}

func TestNetworkCandidatesFlagsAndDuplicatePreference(t *testing.T) {
	addrs := func(cidr string) []net.Addr { return []net.Addr{testAddr(t, cidr)} }
	adapters := []adapterInfo{
		{Name: "Ethernet", Flags: net.FlagUp, Addrs: addrs("192.168.1.10/24")},
		{Name: "Tailscale", Flags: net.FlagUp, Addrs: addrs("192.168.1.20/32")},
		{Name: "Ethernet", Flags: net.FlagUp | net.FlagLoopback, Addrs: addrs("192.168.77.10/24")},
		{Name: "Tailscale", Flags: 0, Addrs: addrs("100.64.9.2/32")},
	}
	got := networkCandidates(adapters)
	if len(got) != 1 {
		t.Fatalf("networkCandidates returned %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Kind != adapterEthernet || got[0].Name != "Ethernet" || got[0].CIDR != "192.168.1.0/24" {
		t.Errorf("duplicate preference = %+v, want the physical Ethernet candidate", got[0])
	}
}

func TestNetworkCandidateLabel(t *testing.T) {
	cases := []struct {
		c    networkCandidate
		want string
	}{
		{networkCandidate{Name: "Ethernet", Kind: adapterEthernet, CIDR: "192.168.1.0/24"}, "Ethernet — 192.168.1.0/24"},
		{networkCandidate{Name: "Wi-Fi", Kind: adapterWiFi, CIDR: "192.168.50.0/24"}, "Wi-Fi — 192.168.50.0/24"},
		{networkCandidate{Name: "Tailscale", Kind: adapterVPN, CIDR: "100.64.1.0/24"}, "VPN — 100.64.1.0/24 (Tailscale)"},
		{networkCandidate{Name: "OpenVPN TAP-Windows6", Kind: adapterVPN, CIDR: "10.9.0.0/24"}, "VPN — 10.9.0.0/24 (OpenVPN TAP-Windows6)"},
	}
	for _, tc := range cases {
		if got := tc.c.Label(); got != tc.want {
			t.Errorf("Label() = %q, want %q", got, tc.want)
		}
	}
}

func TestDetectedNetworksAreValid24s(t *testing.T) {
	for _, c := range detectedNetworks() {
		_, canonical, err := parseSubnet(c.CIDR)
		if err != nil {
			t.Errorf("detected network %q is not a /24: %v", c.CIDR, err)
			continue
		}
		if canonical != c.CIDR {
			t.Errorf("detected network %q canonicalizes to %q", c.CIDR, canonical)
		}
		if c.Kind.String() == "" {
			t.Errorf("detected network %q has no adapter kind", c.CIDR)
		}
	}
}
