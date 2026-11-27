package main

import (
	"net"
	"sort"
	"strings"
)

// adapterKind is the user-facing category of a detected adapter.
type adapterKind int

const (
	adapterEthernet adapterKind = iota
	adapterWiFi
	adapterVPN
	adapterOther
)

func (k adapterKind) String() string {
	switch k {
	case adapterEthernet:
		return "Ethernet"
	case adapterWiFi:
		return "Wi-Fi"
	case adapterVPN:
		return "VPN"
	default:
		return "Other"
	}
}

// adapterInfo is the subset of net.Interface needed by detection. Tests build
// these directly so classification does not depend on the host's real adapters.
type adapterInfo struct {
	Name  string
	Flags net.Flags
	Addrs []net.Addr
}

// networkCandidate is one selectable /24. The CIDR is always the /24 that
// contains an adapter address, including /32 VPN addresses.
type networkCandidate struct {
	Name string
	Kind adapterKind
	CIDR string
}

// Label is the exact text shown in the network ComboBox.
func (c networkCandidate) Label() string {
	label := c.Kind.String() + " — " + c.CIDR
	if c.Name != "" && !strings.EqualFold(c.Name, c.Kind.String()) {
		label += " (" + c.Name + ")"
	}
	return label
}

// detectedNetworks reads the host adapters and returns the selectable /24s.
// It performs no ICMP or other network traffic.
func detectedNetworks() []networkCandidate {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	adapters := make([]adapterInfo, 0, len(ifaces))
	for _, f := range ifaces {
		addrs, err := f.Addrs()
		if err != nil {
			continue
		}
		adapters = append(adapters, adapterInfo{Name: f.Name, Flags: f.Flags, Addrs: addrs})
	}
	return networkCandidates(adapters)
}

// networkCandidates filters, classifies, sorts and de-duplicates adapters.
// Physical Ethernet is preferred, then Wi-Fi, then VPN, then anything else.
// The first candidate is the automatic default.
func networkCandidates(adapters []adapterInfo) []networkCandidate {
	var all []networkCandidate
	for _, a := range adapters {
		if a.Flags&net.FlagUp == 0 || a.Flags&net.FlagLoopback != 0 {
			continue
		}
		kind, ok := classifyAdapter(a.Name)
		if !ok {
			continue
		}
		for _, addr := range a.Addrs {
			cidr, ok := candidateSubnet(addr)
			if !ok {
				continue
			}
			all = append(all, networkCandidate{Name: a.Name, Kind: kind, CIDR: cidr})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Kind != all[j].Kind {
			return all[i].Kind < all[j].Kind
		}
		ni, nj := strings.ToLower(all[i].Name), strings.ToLower(all[j].Name)
		if ni != nj {
			return ni < nj
		}
		return all[i].CIDR < all[j].CIDR
	})

	out := make([]networkCandidate, 0, len(all))
	seen := make(map[string]bool, len(all))
	for _, c := range all {
		if seen[c.CIDR] {
			continue
		}
		seen[c.CIDR] = true
		out = append(out, c)
	}
	return out
}

// classifyAdapter returns the category of an adapter and whether it belongs in
// the default list. Virtual switches, container networks and loopback are
// excluded by name; manual CIDR entry can still target them.
func classifyAdapter(name string) (adapterKind, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, token := range excludedAdapterTokens {
		if strings.Contains(n, token) {
			return adapterOther, false
		}
	}
	switch {
	case isVPNAdapter(n):
		return adapterVPN, true
	case isWiFiAdapter(n):
		return adapterWiFi, true
	case isEthernetAdapter(n):
		return adapterEthernet, true
	default:
		return adapterOther, true
	}
}

var excludedAdapterTokens = []string{
	"docker",
	"wsl",
	"hyper-v",
	"hyperv",
	"vethernet",
	"default switch",
	"vmware",
	"vmnet",
	"virtualbox",
	"vbox",
	"loopback",
	"pseudo",
	"teredo",
	"isatap",
	"npcap",
	"winpcap",
	"bluetooth",
	"wi-fi direct",
	"wifi direct",
	"virtual wi-fi",
	"virtual wifi",
	"hosted network",
	"veth",
	"virbr",
	"podman",
	"container",
	"cni",
	"flannel",
	"cali",
	"br-",
}

func isVPNAdapter(n string) bool {
	for _, token := range []string{
		"vpn", "wireguard", "openvpn", "tailscale", "zerotier", "nordlynx",
		"proton", "mullvad", "surfshark", "anyconnect", "globalprotect",
		"pangp", "fortissl", "fortinet", "juniper", "pulse", "netextender",
		"sonicwall", "checkpoint", "sangfor", "hillstone", "aventail",
		"cloudflare", "warp", "sstp", "pptp", "l2tp", "ikev2", "ipsec",
		"softether", "pritunl", "hamachi", "radmin", "neorouter", "vpnc",
		"strongswan", "tunnelblick", "tunnel",
	} {
		if strings.Contains(n, token) {
			return true
		}
	}
	for _, prefix := range []string{"wg", "tun", "tap", "ppp", "utun"} {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

func isWiFiAdapter(n string) bool {
	for _, token := range []string{"wi-fi", "wifi", "wlan", "wireless", "802.11", "airport"} {
		if strings.Contains(n, token) {
			return true
		}
	}
	return strings.HasPrefix(n, "wl")
}

func isEthernetAdapter(n string) bool {
	for _, token := range []string{"ethernet", "local area connection", "usb lan", "usb-to-lan", "thunderbolt"} {
		if strings.Contains(n, token) {
			return true
		}
	}
	return strings.HasPrefix(n, "eth") || strings.HasPrefix(n, "en")
}

// candidateSubnet derives the /24 that contains an adapter address. A /32 VPN
// address still yields a usable /24, which is what the scanner supports.
func candidateSubnet(addr net.Addr) (string, bool) {
	if addr == nil {
		return "", false
	}
	var ip net.IP
	switch a := addr.(type) {
	case *net.IPNet:
		ip = a.IP
	case *net.IPAddr:
		ip = a.IP
	default:
		s := addr.String()
		if i := strings.IndexByte(s, '/'); i >= 0 {
			s = s[:i]
		}
		ip = net.ParseIP(s)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", false
	}
	if ip4[0] == 0 || ip4[0] == 127 || ip4[0] >= 224 {
		return "", false
	}
	return net.IPv4(ip4[0], ip4[1], ip4[2], 0).String() + "/24", true
}
