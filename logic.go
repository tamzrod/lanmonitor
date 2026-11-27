package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Application identity. Versioning is integer-only: v2.
const (
	appName    = "LAN Monitor"
	appVersion = 2
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

var (
	errSubnetSyntax = errors.New("enter an IPv4 /24 subnet, for example 192.168.1.0/24")
	errSubnetIPv4   = errors.New("only IPv4 subnets are supported")
	errSubnetSize   = errors.New("only IPv4 /24 subnets are supported")
)

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

// stateTextColor keeps the address label readable on every cell colour.
func stateTextColor(s hostState) uintptr {
	if s == stateUnscanned {
		return 0x00000000
	}
	return 0x00FFFFFF
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
