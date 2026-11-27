package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// Address map and grid geometry. The map is fixed: usable hosts of a /24.
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

	minIntervalSeconds = 5
	maxIntervalSeconds = 3600

	clientWidth  = 720
	clientHeight = 560

	pingTimeoutMS   = 500
	pingPayloadSize = 32
	scanWorkers     = 16
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
)

func (s hostState) String() string {
	switch s {
	case stateResponding:
		return "Responding"
	case stateLost:
		return "Lost response"
	case stateNoResponse:
		return "No response"
	default:
		return "Not scanned"
	}
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

// nextState applies one probe result to the previous state. A host that has
// answered at least once turns red ("lost") when it stops answering, while a
// host that never answered stays dark grey.
func nextState(prev hostState, responded bool) hostState {
	if responded {
		return stateResponding
	}
	if prev == stateResponding || prev == stateLost {
		return stateLost
	}
	return stateNoResponse
}

// clampInterval forces a polling interval into the supported range.
func clampInterval(seconds int) int {
	if seconds < minIntervalSeconds {
		return minIntervalSeconds
	}
	if seconds > maxIntervalSeconds {
		return maxIntervalSeconds
	}
	return seconds
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
func formatStatus(subnet string, i int, st hostState, rttMS uint32) string {
	if subnet == "" {
		subnet = "(no subnet)"
	}
	s := fmt.Sprintf("%s | Host .%d | %s", subnet, i, st)
	if st == stateResponding {
		s += fmt.Sprintf(" | %d ms", rttMS)
	}
	return s
}
