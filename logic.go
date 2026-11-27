package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Application identity. Versioning is integer-only: v3.
const (
	appName    = "LAN Monitor"
	appVersion = 3
	projectURL = "https://github.com/tamzrod/lanmonitor"
	aboutQuote = "Gwapong Pilipino Taas Noo Kahit Kanino!"
)

// Address map and grid geometry. The map is fixed: the usable hosts of a /24.
const (
	hostFirst = 1
	hostLast  = 254

	gridCols = 16
	gridRows = 16
	gridX    = 12
	gridY    = 100

	cellWidth      = 43
	cellHeight     = 28
	cellFillWidth  = 40
	cellFillHeight = 25

	clientWidth  = 720
	clientHeight = 560
)

// Monitoring cadence and probe settings.
const (
	pingTimeoutMS   = 500
	pingPayloadSize = 32

	// discoveryIntervalMS is the period of the full /24 sweep.
	discoveryIntervalMS = 15000
	// activeIntervalMS is the period of the probe sent to promoted hosts.
	activeIntervalMS = 1000
	// demotionMisses is the number of consecutive discovery sweeps a promoted
	// host may miss before it stops being polled actively.
	demotionMisses = 2
)

// Context menu command IDs. They stay clear of the main window control IDs.
const (
	ctxHTTP = 100 + iota
	ctxHTTPS
	ctxSSH
	ctxTelnet
	ctxRDP
	ctxSMB
	ctxCustomPort
	ctxContinuousPing
	ctxCopyIP
)

var (
	errSubnetSyntax  = errors.New("enter an IPv4 /24 subnet, for example 192.168.1.0/24")
	errSubnetIPv4    = errors.New("only IPv4 subnets are supported")
	errSubnetSize    = errors.New("only IPv4 /24 subnets are supported")
	errInvalidIP     = errors.New("enter a valid IPv4 address")
	errInvalidPort   = errors.New("enter a port between 1 and 65535")
	errInvalidScheme = errors.New("choose HTTP, HTTPS or SSH")
	errUnknownAction = errors.New("unknown client action")
)

// clientAction identifies one entry of the IP cell context menu.
type clientAction int

const (
	actionHTTP clientAction = iota
	actionHTTPS
	actionSSH
	actionTelnet
	actionRDP
	actionSMB
	actionCustomPort
	actionContinuousPing
	actionCopyIP
)

func (a clientAction) String() string {
	switch a {
	case actionHTTP:
		return "HTTP"
	case actionHTTPS:
		return "HTTPS"
	case actionSSH:
		return "SSH"
	case actionTelnet:
		return "Telnet"
	case actionRDP:
		return "RDP"
	case actionSMB:
		return "SMB"
	case actionCustomPort:
		return "Custom Port"
	case actionContinuousPing:
		return "Continuous Ping"
	case actionCopyIP:
		return "Copy IP Address"
	default:
		return "Client"
	}
}

type contextMenuItem struct {
	ID     int
	Label  string
	Action clientAction
}

// contextMenuItems is the complete right-click menu for a valid IP cell. The
// order is the order shown to the user.
var contextMenuItems = []contextMenuItem{
	{ctxHTTP, "HTTP", actionHTTP},
	{ctxHTTPS, "HTTPS", actionHTTPS},
	{ctxSSH, "SSH", actionSSH},
	{ctxTelnet, "Telnet", actionTelnet},
	{ctxRDP, "RDP", actionRDP},
	{ctxSMB, "SMB", actionSMB},
	{ctxCustomPort, "Custom Port", actionCustomPort},
	{ctxContinuousPing, "Continuous Ping", actionContinuousPing},
	{ctxCopyIP, "Copy IP Address", actionCopyIP},
}

func contextAction(id int) (clientAction, bool) {
	for _, item := range contextMenuItems {
		if item.ID == id {
			return item.Action, true
		}
	}
	return 0, false
}

// launchKind tells the Windows layer how to start a prepared command.
type launchKind int

const (
	launchShell launchKind = iota
	launchProcess
	launchClipboard
)

// command is a validated client invocation. Args are passed to the executable
// one by one; no shell is involved.
type command struct {
	Kind       launchKind
	Target     string
	Args       []string
	NewConsole bool
}

// customPort is the user's choice in the Custom Port dialog.
type customPort struct {
	Scheme string
	Port   uint16
}

// hostState is the per-address state shown in the IP map.
type hostState int

const (
	stateUnscanned hostState = iota
	stateResponding
	stateLost
	stateNoResponse
	stateSuspect
)

func (s hostState) String() string {
	switch s {
	case stateResponding:
		return "Responding"
	case stateLost:
		return "Lost response"
	case stateNoResponse:
		return "No response"
	case stateSuspect:
		return "Suspect"
	default:
		return "Not scanned"
	}
}

// stateColor returns the COLORREF (0x00BBGGRR) of a cell.
func stateColor(s hostState) uintptr {
	switch s {
	case stateResponding:
		return 0x00459520 // green
	case stateLost:
		return 0x004040D0 // red
	case stateNoResponse:
		return 0x00606060 // dark grey
	case stateSuspect:
		return 0x000099DD // orange
	default:
		return 0x00AAAAAA // light grey
	}
}

// hostRecord is the monitoring state of one address.
type hostRecord struct {
	state    hostState
	rttMS    uint32
	seen     bool
	promoted bool
	misses   int
}

// observe applies one probe result.
//
// discovery distinguishes a result from the 15 s full sweep from a 1 s active
// probe of an already promoted host. A discovery reply promotes the host and
// clears its miss counter. A promoted host that misses demotionMisses
// consecutive discovery sweeps is demoted, so it is no longer polled actively
// until a sweep finds it again. Active misses only change the displayed state: a
// single lost echo on a busy link is not evidence that a host has gone away.
func (h *hostRecord) observe(discovery, responded bool, rttMS uint32) {
	if responded {
		h.seen = true
		h.rttMS = rttMS
		h.state = stateResponding
		if discovery {
			h.promoted = true
			h.misses = 0
		}
		return
	}
	if discovery && h.promoted {
		h.misses++
		if h.misses >= demotionMisses {
			h.promoted = false
			h.misses = 0
			h.state = stateLost
			return
		}
	}
	switch {
	case h.promoted:
		h.state = stateSuspect
	case h.seen:
		h.state = stateLost
	default:
		h.state = stateNoResponse
	}
}

// targets returns the addresses a round must probe: every usable host for a
// discovery sweep, only promoted hosts for an active round.
func targets(discovery bool, states []hostRecord) []int {
	ids := make([]int, 0, hostLast)
	for i := hostFirst; i <= hostLast; i++ {
		if discovery || states[i].promoted {
			ids = append(ids, i)
		}
	}
	return ids
}

// nextScanSession returns the session for a discovery round. A changed subnet
// increments the session so results from a round on the old subnet are dropped.
func nextScanSession(target, canonical string, session uint64) (uint64, bool) {
	if target == canonical {
		return session, false
	}
	return session + 1, true
}

// parseSubnet validates a CIDR string and returns the network address plus its
// canonical form, which is used to detect subnet changes between scans.
func parseSubnet(s string) (base [4]byte, canonical string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return base, "", errSubnetSyntax
	}
	_, network, perr := net.ParseCIDR(s)
	if perr != nil {
		return base, "", errSubnetSyntax
	}
	ip4 := network.IP.To4()
	if ip4 == nil {
		return base, "", errSubnetIPv4
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || ones != 24 {
		return base, "", errSubnetSize
	}
	copy(base[:], ip4)
	return base, network.String(), nil
}

// hostAddr returns the address of the i-th usable host in base/24.
func hostAddr(base [4]byte, i int) [4]byte {
	return [4]byte{base[0], base[1], base[2], byte(i)}
}

// validateIPv4 returns the canonical dotted-quad form of a unicast IPv4
// address. It rejects IPv6, CIDR notation, the unspecified address, broadcast
// and multicast, so a value that passes can be passed to a client as one
// argument without quoting or shell interpolation.
func validateIPv4(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "/:") {
		return "", errInvalidIP
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", errInvalidIP
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", errInvalidIP
	}
	if ip4[0] == 0 || ip4[0] >= 224 {
		return "", errInvalidIP
	}
	return ip4.String(), nil
}

// hostAddress returns the validated address of host i inside a /24 subnet.
func hostAddress(subnet string, i int) (string, error) {
	base, _, err := parseSubnet(subnet)
	if err != nil {
		return "", err
	}
	if i < hostFirst || i > hostLast {
		return "", errInvalidIP
	}
	return validateIPv4(net.IPv4(base[0], base[1], base[2], byte(i)).String())
}

// parsePort accepts a decimal port in the range 1-65535.
func parsePort(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 5 {
		return 0, errInvalidPort
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errInvalidPort
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, errInvalidPort
	}
	return uint16(n), nil
}

// normalizeScheme accepts the three protocols supported by Custom Port.
func normalizeScheme(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "http":
		return "http", nil
	case "https":
		return "https", nil
	case "ssh":
		return "ssh", nil
	}
	return "", errInvalidScheme
}

// buildClientCommand validates the address and turns one menu action into a
// concrete command. The IP address is always validated before it is placed in a
// URL or an argument list.
func buildClientCommand(action clientAction, ip string, custom customPort) (command, error) {
	addr, err := validateIPv4(ip)
	if err != nil {
		return command{}, err
	}
	switch action {
	case actionHTTP:
		return command{Kind: launchShell, Target: "http://" + addr + "/"}, nil
	case actionHTTPS:
		return command{Kind: launchShell, Target: "https://" + addr + "/"}, nil
	case actionSSH:
		return command{Kind: launchProcess, Target: "ssh.exe", Args: []string{addr}, NewConsole: true}, nil
	case actionTelnet:
		return command{Kind: launchProcess, Target: "telnet.exe", Args: []string{addr}, NewConsole: true}, nil
	case actionRDP:
		return command{Kind: launchProcess, Target: "mstsc.exe", Args: []string{"/v:" + addr}}, nil
	case actionSMB:
		return command{Kind: launchProcess, Target: "explorer.exe", Args: []string{`\\` + addr}}, nil
	case actionCustomPort:
		return buildCustomPortCommand(addr, custom.Scheme, custom.Port)
	case actionContinuousPing:
		return command{Kind: launchProcess, Target: "ping.exe", Args: []string{"-t", addr}, NewConsole: true}, nil
	case actionCopyIP:
		return command{Kind: launchClipboard, Target: addr}, nil
	}
	return command{}, errUnknownAction
}

// buildCustomPortCommand builds an HTTP, HTTPS or SSH command on a chosen port.
func buildCustomPortCommand(ip, scheme string, port uint16) (command, error) {
	addr, err := validateIPv4(ip)
	if err != nil {
		return command{}, err
	}
	if port < 1 {
		return command{}, errInvalidPort
	}
	scheme, err = normalizeScheme(scheme)
	if err != nil {
		return command{}, err
	}
	p := strconv.FormatUint(uint64(port), 10)
	switch scheme {
	case "http":
		return command{Kind: launchShell, Target: "http://" + addr + ":" + p + "/"}, nil
	case "https":
		return command{Kind: launchShell, Target: "https://" + addr + ":" + p + "/"}, nil
	case "ssh":
		return command{Kind: launchProcess, Target: "ssh.exe", Args: []string{"-p", p, addr}, NewConsole: true}, nil
	}
	return command{}, errInvalidScheme
}

// unavailableMessage is shown when the requested client cannot be started.
func unavailableMessage(action clientAction) string {
	switch action {
	case actionHTTP, actionHTTPS:
		return "No default web browser is available for this URL."
	case actionSSH:
		return "SSH client (ssh.exe) is not available on this system."
	case actionTelnet:
		return "Telnet client (telnet.exe) is not available on this system."
	case actionRDP:
		return "Remote Desktop Connection (mstsc.exe) is not available on this system."
	case actionSMB:
		return "Windows Explorer (explorer.exe) is not available on this system."
	case actionContinuousPing:
		return "ping.exe is not available on this system."
	case actionCustomPort:
		return "The selected client is not available on this system."
	case actionCopyIP:
		return "Could not copy the IP address to the clipboard."
	default:
		return action.String() + " is not available on this system."
	}
}

// windowTitle is the exact title bar text for this release.
func windowTitle() string {
	return fmt.Sprintf("%s — Version %d", appName, appVersion)
}

// cellSlot returns the top-left corner of the grid slot holding host i.
func cellSlot(i int) (x, y int) {
	k := i - hostFirst
	return gridX + (k%gridCols)*cellWidth, gridY + (k/gridCols)*cellHeight
}

// hostAt maps a client-area point to a host number. Gaps between cells and
// unused slots count as misses.
func hostAt(x, y int) (int, bool) {
	dx, dy := x-gridX, y-gridY
	if dx < 0 || dy < 0 {
		return 0, false
	}
	col, row := dx/cellWidth, dy/cellHeight
	if col >= gridCols || row >= gridRows {
		return 0, false
	}
	i := hostFirst + row*gridCols + col
	if i > hostLast {
		return 0, false
	}
	return i, true
}

// formatStatus builds the status line for one host.
func formatStatus(subnet string, i int, rec hostRecord) string {
	if subnet == "" {
		subnet = "(no subnet)"
	}
	s := fmt.Sprintf("%s | Host .%d | %s", subnet, i, rec.state)
	if rec.state == stateResponding {
		s += fmt.Sprintf(" | %d ms", rec.rttMS)
	}
	if rec.promoted {
		s += " | Promoted"
	}
	return s
}
