package main

import (
	"testing"
	"unsafe"
)

// The structs below are passed to Win32 by pointer, so their size and field
// offsets must match the windows/amd64 ABI exactly. These checks run on the
// Windows CI runner without touching a window or the network.
func TestWin32StructLayout(t *testing.T) {
	sizes := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"rect", unsafe.Sizeof(rect{}), 16},
		{"point", unsafe.Sizeof(point{}), 8},
		{"msg", unsafe.Sizeof(msg{}), 48},
		{"wndClass", unsafe.Sizeof(wndClass{}), 72},
		{"paintStruct", unsafe.Sizeof(paintStruct{}), 72},
		{"icmpEchoReply", unsafe.Sizeof(icmpEchoReply{}), 40},
	}
	for _, s := range sizes {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}
}

func TestWin32StructOffsets(t *testing.T) {
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"msg.WParam", unsafe.Offsetof(msg{}.WParam), 16},
		{"msg.LParam", unsafe.Offsetof(msg{}.LParam), 24},
		{"msg.Pt", unsafe.Offsetof(msg{}.Pt), 36},
		{"wndClass.WndProc", unsafe.Offsetof(wndClass{}.WndProc), 8},
		{"wndClass.Instance", unsafe.Offsetof(wndClass{}.Instance), 24},
		{"wndClass.ClassName", unsafe.Offsetof(wndClass{}.ClassName), 64},
		{"paintStruct.Paint", unsafe.Offsetof(paintStruct{}.Paint), 12},
		{"paintStruct.Reserved", unsafe.Offsetof(paintStruct{}.Reserved), 36},
		{"icmpEchoReply.Data", unsafe.Offsetof(icmpEchoReply{}.Data), 16},
		{"icmpEchoReply.Options", unsafe.Offsetof(icmpEchoReply{}.Options), 24},
	}
	for _, o := range offsets {
		if o.got != o.want {
			t.Errorf("offsetof(%s) = %d, want %d", o.name, o.got, o.want)
		}
	}
}

func TestStateColorIsColorRef(t *testing.T) {
	// COLORREF is 0x00BBGGRR; the blue channel must stay clear.
	for _, st := range []hostState{stateUnscanned, stateResponding, stateLost, stateNoResponse, stateSuspect} {
		if c := stateColor(st); c&0xFF000000 != 0 {
			t.Errorf("stateColor(%v) = %#x is not a COLORREF", st, c)
		}
	}
}
