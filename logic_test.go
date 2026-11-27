package main

import (
	"strings"
	"testing"
)

func TestParseSubnet(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBase  [4]byte
		wantCanon string
		wantErr   error
	}{
		{"plain", "192.168.1.0/24", [4]byte{192, 168, 1, 0}, "192.168.1.0/24", nil},
		{"host bits set", "192.168.1.77/24", [4]byte{192, 168, 1, 0}, "192.168.1.0/24", nil},
		{"padded", "  10.0.0.0/24 ", [4]byte{10, 0, 0, 0}, "10.0.0.0/24", nil},
		{"private high", "172.16.5.9/24", [4]byte{172, 16, 5, 0}, "172.16.5.0/24", nil},
		{"empty", "", [4]byte{}, "", errSubnetSyntax},
		{"blank", "   ", [4]byte{}, "", errSubnetSyntax},
		{"no prefix", "192.168.1.0", [4]byte{}, "", errSubnetSyntax},
		{"no address", "/24", [4]byte{}, "", errSubnetSyntax},
		{"garbage", "not-a-subnet", [4]byte{}, "", errSubnetSyntax},
		{"missing octet", "192.168.1/24", [4]byte{}, "", errSubnetSyntax},
		{"octet out of range", "192.168.1.256/24", [4]byte{}, "", errSubnetSyntax},
		{"slash 16", "192.168.0.0/16", [4]byte{}, "", errSubnetSize},
		{"slash 25", "192.168.1.0/25", [4]byte{}, "", errSubnetSize},
		{"slash 32", "192.168.1.1/32", [4]byte{}, "", errSubnetSize},
		{"ipv6", "2001:db8::/24", [4]byte{}, "", errSubnetIPv4},
		{"ipv6 loopback", "::1/128", [4]byte{}, "", errSubnetIPv4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, canonical, err := parseSubnet(tc.input)
			if err != tc.wantErr {
				t.Fatalf("parseSubnet(%q) error = %v, want %v", tc.input, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if base != tc.wantBase {
				t.Errorf("parseSubnet(%q) base = %v, want %v", tc.input, base, tc.wantBase)
			}
			if canonical != tc.wantCanon {
				t.Errorf("parseSubnet(%q) canonical = %q, want %q", tc.input, canonical, tc.wantCanon)
			}
		})
	}
}

func TestParseSubnetErrorMessages(t *testing.T) {
	for _, err := range []error{errSubnetSyntax, errSubnetIPv4, errSubnetSize} {
		if err.Error() == "" {
			t.Errorf("error %v has an empty message", err)
		}
	}
	if !strings.Contains(errSubnetSyntax.Error(), "/24") {
		t.Errorf("syntax error should hint at the /24 form: %q", errSubnetSyntax)
	}
}

func TestHostAddr(t *testing.T) {
	base := [4]byte{192, 168, 1, 0}
	for _, i := range []int{1, 2, 100, 254} {
		got := hostAddr(base, i)
		if got[0] != 192 || got[1] != 168 || got[2] != 1 || int(got[3]) != i {
			t.Errorf("hostAddr(base, %d) = %v, want 192.168.1.%d", i, got, i)
		}
	}
}

func TestNextState(t *testing.T) {
	tests := []struct {
		prev      hostState
		responded bool
		want      hostState
	}{
		{stateUnscanned, true, stateResponding},
		{stateUnscanned, false, stateNoResponse},
		{stateResponding, true, stateResponding},
		{stateResponding, false, stateLost},
		{stateLost, true, stateResponding},
		{stateLost, false, stateLost},
		{stateNoResponse, true, stateResponding},
		{stateNoResponse, false, stateNoResponse},
	}
	for _, tc := range tests {
		if got := nextState(tc.prev, tc.responded); got != tc.want {
			t.Errorf("nextState(%v, %v) = %v, want %v", tc.prev, tc.responded, got, tc.want)
		}
	}
}

func TestClampInterval(t *testing.T) {
	tests := []struct{ in, want int }{
		{-10, minIntervalSeconds},
		{0, minIntervalSeconds},
		{4, minIntervalSeconds},
		{5, 5},
		{60, 60},
		{3600, maxIntervalSeconds},
		{3601, maxIntervalSeconds},
	}
	for _, tc := range tests {
		if got := clampInterval(tc.in); got != tc.want {
			t.Errorf("clampInterval(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestGridHoldsEveryHost(t *testing.T) {
	if got := gridCols * gridRows; got < hostLast-hostFirst+1 {
		t.Fatalf("grid has %d slots, need at least %d", got, hostLast-hostFirst+1)
	}
}

func TestCellSlotOrder(t *testing.T) {
	firstX, firstY := cellSlot(hostFirst)
	if firstX != gridX || firstY != gridY {
		t.Errorf("cellSlot(%d) = (%d,%d), want (%d,%d)", hostFirst, firstX, firstY, gridX, gridY)
	}
	// Host 17 starts a new row.
	x17, y17 := cellSlot(17)
	if x17 != gridX || y17 != gridY+cellHeight {
		t.Errorf("cellSlot(17) = (%d,%d), want (%d,%d)", x17, y17, gridX, gridY+cellHeight)
	}
	x254, y254 := cellSlot(254)
	if x254 <= gridX || y254 != gridY+15*cellHeight {
		t.Errorf("cellSlot(254) = (%d,%d)", x254, y254)
	}
	// The last cell must stay inside the client area.
	if x254+cellFillWidth > clientWidth || y254+cellFillHeight > clientHeight {
		t.Errorf("cell 254 at (%d,%d) overflows the %dx%d client area", x254, y254, clientWidth, clientHeight)
	}
}

func TestHostAtRoundTrip(t *testing.T) {
	for i := hostFirst; i <= hostLast; i++ {
		x, y := cellSlot(i)
		got, ok := hostAt(x+cellFillWidth/2, y+cellFillHeight/2)
		if !ok || got != i {
			t.Fatalf("hostAt(centre of cell %d) = (%d,%v), want (%d,true)", i, got, ok, i)
		}
	}
}

func TestHostAtMisses(t *testing.T) {
	tests := []struct {
		name string
		x, y int
	}{
		{"negative", -1, -1},
		{"above grid", gridX, gridY - 1},
		{"left of grid", gridX - 1, gridY},
		{"right of last column", gridX + gridCols*cellWidth, gridY},
		{"below last row", gridX, gridY + gridRows*cellHeight},
		{"unused slot 255", gridX + 14*cellWidth, gridY + 15*cellHeight},
		{"unused slot 256", gridX + 15*cellWidth, gridY + 15*cellHeight},
	}
	for _, tc := range tests {
		if i, ok := hostAt(tc.x, tc.y); ok {
			t.Errorf("%s: hostAt(%d,%d) = %d, want a miss", tc.name, tc.x, tc.y, i)
		}
	}
}

func TestHostAtIgnoresColumnOverflow(t *testing.T) {
	// Regression: a point just past column 16 used to be reported as host 17.
	if i, ok := hostAt(gridX+gridCols*cellWidth-1, gridY+1); ok {
		if i > 16 {
			t.Errorf("hostAt at the last column returned host %d", i)
		}
	}
	if _, ok := hostAt(gridX+gridCols*cellWidth, gridY+1); ok {
		t.Error("hostAt past the last column reported a host")
	}
}

func TestFormatStatus(t *testing.T) {
	got := formatStatus("192.168.1.0/24", 5, stateResponding, 12)
	want := "192.168.1.0/24 | Host .5 | Responding | 12 ms"
	if got != want {
		t.Errorf("formatStatus() = %q, want %q", got, want)
	}

	got = formatStatus("10.0.0.0/24", 254, stateLost, 0)
	want = "10.0.0.0/24 | Host .254 | Lost response"
	if got != want {
		t.Errorf("formatStatus() = %q, want %q", got, want)
	}

	got = formatStatus("", 1, stateUnscanned, 0)
	want = "(no subnet) | Host .1 | Not scanned"
	if got != want {
		t.Errorf("formatStatus() = %q, want %q", got, want)
	}
}

func TestHostStateString(t *testing.T) {
	tests := []struct {
		state hostState
		want  string
	}{
		{stateUnscanned, "Not scanned"},
		{stateResponding, "Responding"},
		{stateLost, "Lost response"},
		{stateNoResponse, "No response"},
		{hostState(99), "Not scanned"},
	}
	for _, tc := range tests {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("hostState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}
