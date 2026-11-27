package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	iphlpapi = syscall.NewLazyDLL("iphlpapi.dll")
)

// Win32 structures, laid out for windows/amd64.
type (
	rect struct {
		Left, Top, Right, Bottom int32
	}
	point struct {
		X, Y int32
	}
	msg struct {
		Hwnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      point
		Private uint32
	}
	// wndClass mirrors WNDCLASS, used with RegisterClassW (not WNDCLASSEXW).
	wndClass struct {
		Style      uint32
		WndProc    uintptr
		ClsExtra   int32
		WndExtra   int32
		Instance   uintptr
		Icon       uintptr
		Cursor     uintptr
		Background uintptr
		MenuName   uintptr
		ClassName  uintptr
	}
	paintStruct struct {
		DC        uintptr
		Erase     int32
		Paint     rect
		Restore   int32
		IncUpdate int32
		Reserved  [32]byte
	}
	icmpEchoReply struct {
		Address   uint32
		Status    uint32
		RoundTrip uint32
		DataSize  uint16
		Reserved  uint16
		Data      uintptr
		Options   [16]byte
	}
)

const (
	wmDestroy   = 0x0002
	wmPaint     = 0x000F
	wmSetFont   = 0x0030
	wmCommand   = 0x0111
	wmTimer     = 0x0113
	wmMouseMove = 0x0200

	wmScanProgress = 0x8001
	wmScanDone     = 0x8002

	bmGetCheck = 0x00F0
	bmSetCheck = 0x00F1
	bstChecked = 1

	idScan     = 1
	idStop     = 2
	idPoll     = 3
	idSubnet   = 10
	idInterval = 11
	idHint     = 12
	idStatus   = 13

	pollTimerID = 1

	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipChildren = 0x02000000
	wsBorder       = 0x00800000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	esNumber       = 0x2000
	bsAutoCheckBox = 0x00000003
	swShow         = 5
	dtCell         = 0x00000001 | 0x00000004 | 0x00000020 // DT_CENTER|DT_VCENTER|DT_SINGLELINE

	iconArrow       = 32512
	colorWindowBk   = 6
	stockDefaultGUI = 17
	transparentBk   = 1

	logPixelsX       = 88
	fwNormal         = 400
	defaultCharset   = 1
	clearTypeQuality = 5
	uiFontPoints     = 13
)

var (
	procCache sync.Map

	mainWindow  uintptr
	subnetEdit  uintptr
	pollCheck   uintptr
	intervalBox uintptr
	statusLine  uintptr
	scanButton  uintptr
	instance    uintptr
	guiFont     uintptr

	// uiScale converts the 96-DPI layout units in logic.go to device pixels.
	uiScale = 1.0

	scanBusy       atomic.Bool
	scanCancel     atomic.Bool
	pingerFailures atomic.Int32
	scanGeneration uint64 // UI thread only

	polling       bool   // UI thread only
	currentStatus string // UI thread only

	mapMu        sync.Mutex
	targetSubnet string
	hostStates   [hostLast + 1]hostRecord
)

type hostRecord struct {
	state hostState
	rttMS uint32
}

func winProc(dll *syscall.LazyDLL, name string) *syscall.LazyProc {
	key := dll.Name + "!" + name
	if p, ok := procCache.Load(key); ok {
		return p.(*syscall.LazyProc)
	}
	p := dll.NewProc(name)
	actual, _ := procCache.LoadOrStore(key, p)
	return actual.(*syscall.LazyProc)
}

// call invokes a Win32 entry point. String arguments become NUL-terminated
// UTF-16 buffers that stay reachable until the call returns, so the garbage
// collector cannot free them while Windows is reading them.
func call(dll *syscall.LazyDLL, name string, args ...any) uintptr {
	pinned := make([]*uint16, 0, len(args))
	raw := make([]uintptr, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case string:
			p, err := syscall.UTF16PtrFromString(v)
			if err != nil {
				p, _ = syscall.UTF16PtrFromString("")
			}
			pinned = append(pinned, p)
			raw[i] = uintptr(unsafe.Pointer(p))
		case uintptr:
			raw[i] = v
		case int:
			raw[i] = uintptr(v)
		case int32:
			raw[i] = uintptr(int64(v))
		case uint32:
			raw[i] = uintptr(v)
		case bool:
			if v {
				raw[i] = 1
			}
		case unsafe.Pointer:
			raw[i] = uintptr(v)
		}
	}
	r, _, _ := winProc(dll, name).Call(raw...)
	runtime.KeepAlive(pinned)
	return r
}

// px converts a layout unit (96 DPI) to device pixels.
func px(v int) int { return int(math.Round(float64(v) * uiScale)) }

// unpx converts device pixels back to layout units.
func unpx(v int) int { return int(math.Round(float64(v) / uiScale)) }

// setStatus updates the status line only when the text actually changes, so
// mouse movement does not repaint the control on every pixel.
func setStatus(text string) {
	if text == currentStatus {
		return
	}
	currentStatus = text
	call(user32, "SetWindowTextW", statusLine, text)
}

func createUIFont() uintptr {
	if h := call(gdi32, "CreateFontW", -px(uiFontPoints), uintptr(0), uintptr(0), uintptr(0),
		uintptr(fwNormal), uintptr(0), uintptr(0), uintptr(0), uintptr(defaultCharset),
		uintptr(0), uintptr(0), uintptr(clearTypeQuality), uintptr(0), "Segoe UI"); h != 0 {
		return h
	}
	return call(gdi32, "GetStockObject", uintptr(stockDefaultGUI))
}

func controlText(h uintptr) string {
	var buf [128]uint16
	call(user32, "GetWindowTextW", h, unsafe.Pointer(&buf[0]), len(buf))
	return syscall.UTF16ToString(buf[:])
}

func createControl(class, title string, style, x, y, w, h int, id int) uintptr {
	hwnd := call(user32, "CreateWindowExW", uintptr(0), class, title,
		uintptr(wsChild|wsVisible|style), px(x), px(y), px(w), px(h),
		mainWindow, uintptr(id), instance, uintptr(0))
	if hwnd != 0 && guiFont != 0 {
		call(user32, "SendMessageW", hwnd, uintptr(wmSetFont), guiFont, uintptr(1))
	}
	return hwnd
}

// icmpPinger owns one ICMP handle for the lifetime of a worker.
type icmpPinger struct {
	handle uintptr
}

func newPinger() (*icmpPinger, error) {
	h := call(iphlpapi, "IcmpCreateFile")
	if h == 0 || h == ^uintptr(0) {
		return nil, fmt.Errorf("IcmpCreateFile failed")
	}
	return &icmpPinger{handle: h}, nil
}

func (p *icmpPinger) close() {
	call(iphlpapi, "IcmpCloseHandle", p.handle)
}

// ping sends one echo request. The address is passed as a DWORD whose memory
// layout equals the four octets, which is what IPAddr expects. The reply buffer
// must hold an ICMP_ECHO_REPLY plus 8 bytes of ICMP header plus the payload.
func (p *icmpPinger) ping(addr [4]byte, timeoutMS uint32) (bool, uint32) {
	payload := [pingPayloadSize]byte{'l', 'a', 'n', 'm', 'o', 'n'}
	var reply [128]byte
	n := call(iphlpapi, "IcmpSendEcho", p.handle,
		uintptr(binary.LittleEndian.Uint32(addr[:])),
		unsafe.Pointer(&payload[0]), len(payload), uintptr(0),
		unsafe.Pointer(&reply[0]), len(reply), uintptr(timeoutMS))
	if n == 0 {
		return false, 0
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	return r.Status == 0, r.RoundTrip
}

func startScan() {
	if scanBusy.Load() {
		setStatus("Scan already running - wait or press Stop")
		return
	}
	base, canonical, err := parseSubnet(controlText(subnetEdit))
	if err != nil {
		setStatus(err.Error())
		return
	}

	mapMu.Lock()
	if targetSubnet != canonical {
		hostStates = [hostLast + 1]hostRecord{}
		targetSubnet = canonical
	}
	mapMu.Unlock()

	scanCancel.Store(false)
	scanBusy.Store(true)
	scanGeneration++
	gen := scanGeneration

	call(user32, "EnableWindow", subnetEdit, uintptr(0))
	call(user32, "EnableWindow", scanButton, uintptr(0))
	setStatus("Scanning " + canonical + " ...")

	go runScan(base, gen)
}

func runScan(base [4]byte, gen uint64) {
	pingerFailures.Store(0)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < scanWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := newPinger()
			if err != nil {
				pingerFailures.Add(1)
				// Drain so the producer never blocks on a dead worker.
				for range jobs {
				}
				return
			}
			defer p.close()
			for i := range jobs {
				if scanCancel.Load() {
					continue
				}
				ok, rtt := p.ping(hostAddr(base, i), pingTimeoutMS)
				mapMu.Lock()
				hostStates[i] = hostRecord{state: nextState(hostStates[i].state, ok), rttMS: rtt}
				mapMu.Unlock()
				call(user32, "PostMessageW", mainWindow, uintptr(wmScanProgress), uintptr(0), uintptr(0))
			}
		}()
	}
	for i := hostFirst; i <= hostLast && !scanCancel.Load(); i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	call(user32, "PostMessageW", mainWindow, uintptr(wmScanDone), uintptr(gen), uintptr(0))
}

func stateColor(st hostState) uintptr {
	switch st {
	case stateResponding:
		return 0x00459520 // COLORREF 0x00BBGGRR: green
	case stateLost:
		return 0x004040D0 // red
	case stateNoResponse:
		return 0x00606060 // dark grey
	default:
		return 0x00AAAAAA // grey
	}
}

// stateTextColor keeps the label readable on every cell colour.
func stateTextColor(st hostState) uintptr {
	if st == stateUnscanned {
		return 0x00000000 // black on the light grey unscanned cell
	}
	return 0x00FFFFFF
}

func paintWindow() {
	var ps paintStruct
	hdc := call(user32, "BeginPaint", mainWindow, unsafe.Pointer(&ps))
	if hdc == 0 {
		return
	}
	defer call(user32, "EndPaint", mainWindow, unsafe.Pointer(&ps))

	oldFont := uintptr(0)
	if guiFont != 0 {
		oldFont = call(gdi32, "SelectObject", hdc, guiFont)
	}
	call(gdi32, "SetBkMode", hdc, uintptr(transparentBk))

	var brushes [4]uintptr
	for st := range brushes {
		brushes[st] = call(gdi32, "CreateSolidBrush", stateColor(hostState(st)))
		defer call(gdi32, "DeleteObject", brushes[st])
	}
	brushFor := func(st hostState) uintptr {
		if st >= 0 && int(st) < len(brushes) {
			return brushes[st]
		}
		return brushes[0]
	}

	mapMu.Lock()
	snapshot := hostStates
	mapMu.Unlock()

	for i := hostFirst; i <= hostLast; i++ {
		st := snapshot[i].state
		x, y := cellSlot(i)
		r := rect{int32(px(x)), int32(px(y)), int32(px(x + cellFillWidth)), int32(px(y + cellFillHeight))}
		call(user32, "FillRect", hdc, unsafe.Pointer(&r), brushFor(st))
		call(gdi32, "SetTextColor", hdc, stateTextColor(st))
		label := strconv.Itoa(i)
		call(user32, "DrawTextW", hdc, label, len(label), unsafe.Pointer(&r), uintptr(dtCell))
	}
	if oldFont != 0 {
		call(gdi32, "SelectObject", hdc, oldFont)
	}
}

func stopScan(hwnd uintptr) {
	scanCancel.Store(true)
	polling = false
	call(user32, "SendMessageW", pollCheck, uintptr(bmSetCheck), uintptr(0), uintptr(0))
	call(user32, "KillTimer", hwnd, uintptr(pollTimerID))
	if scanBusy.Load() {
		setStatus("Stopping scan - waiting for in-flight probes")
	} else {
		setStatus("Scan stopped")
	}
}

func setPolling(hwnd uintptr) {
	polling = call(user32, "SendMessageW", pollCheck, uintptr(bmGetCheck), uintptr(0), uintptr(0)) == bstChecked
	call(user32, "KillTimer", hwnd, uintptr(pollTimerID))
	if !polling {
		setStatus("Continuous polling off")
		return
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(controlText(intervalBox)))
	if err != nil {
		seconds = minIntervalSeconds
	}
	seconds = clampInterval(seconds)
	call(user32, "SetWindowTextW", intervalBox, strconv.Itoa(seconds))
	call(user32, "SetTimer", hwnd, uintptr(pollTimerID), uintptr(seconds*1000), uintptr(0))
	startScan()
}

func handleCommand(hwnd, w uintptr) {
	switch int(w & 0xffff) {
	case idScan:
		startScan()
	case idStop:
		stopScan(hwnd)
	case idPoll:
		setPolling(hwnd)
	}
}

func handleMouseMove(l uintptr) {
	x := int(int16(l & 0xffff))
	y := int(int16((l >> 16) & 0xffff))
	i, ok := hostAt(unpx(x), unpx(y))
	if !ok {
		return
	}
	mapMu.Lock()
	rec, subnet := hostStates[i], targetSubnet
	mapMu.Unlock()
	setStatus(formatStatus(subnet, i, rec.state, rec.rttMS))
}

func windowProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmDestroy:
		scanCancel.Store(true)
		call(user32, "PostQuitMessage", uintptr(0))
		return 0
	case wmPaint:
		paintWindow()
		return 0
	case wmCommand:
		handleCommand(hwnd, w)
		return 0
	case wmTimer:
		if polling {
			startScan()
		}
		return 0
	case wmMouseMove:
		handleMouseMove(l)
		return 0
	case wmScanProgress:
		call(user32, "InvalidateRect", hwnd, unsafe.Pointer(nil), uintptr(0))
		return 0
	case wmScanDone:
		if uint64(w) == scanGeneration {
			scanBusy.Store(false)
			call(user32, "EnableWindow", subnetEdit, uintptr(1))
			call(user32, "EnableWindow", scanButton, uintptr(1))
			switch {
			case int(pingerFailures.Load()) >= scanWorkers:
				setStatus("Scan failed - could not open an ICMP handle")
			case scanCancel.Load():
				setStatus("Scan stopped")
			default:
				setStatus("Scan complete")
			}
		}
		return 0
	}
	return call(user32, "DefWindowProcW", hwnd, m, w, l)
}

// defaultSubnet returns the canonical form of the first active non-loopback
// IPv4 /24, or "" when the host has none.
func defaultSubnet() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, f := range ifaces {
		if f.Flags&net.FlagUp == 0 || f.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := f.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if _, canonical, err := parseSubnet(a.String()); err == nil {
				return canonical
			}
		}
	}
	return ""
}

func main() {
	runtime.LockOSThread()
	call(user32, "SetProcessDPIAware")
	if dc := call(user32, "GetDC", uintptr(0)); dc != 0 {
		if dpi := int(call(gdi32, "GetDeviceCaps", dc, uintptr(logPixelsX))); dpi > 0 {
			uiScale = float64(dpi) / 96
		}
		call(user32, "ReleaseDC", uintptr(0), dc)
	}
	instance = call(kernel32, "GetModuleHandleW", uintptr(0))
	guiFont = createUIFont()

	const className = "LANMonitorWindow"
	cls, err := syscall.UTF16FromString(className)
	if err != nil {
		return
	}
	wc := wndClass{
		WndProc:    syscall.NewCallback(windowProc),
		Instance:   instance,
		Cursor:     call(user32, "LoadCursorW", uintptr(0), uintptr(iconArrow)),
		Background: uintptr(colorWindowBk),
		ClassName:  uintptr(unsafe.Pointer(&cls[0])),
	}
	if call(user32, "RegisterClassW", unsafe.Pointer(&wc)) == 0 {
		return
	}
	runtime.KeepAlive(cls)

	style := uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	frame := rect{0, 0, int32(px(clientWidth)), int32(px(clientHeight))}
	call(user32, "AdjustWindowRect", unsafe.Pointer(&frame), style, uintptr(0))
	mainWindow = call(user32, "CreateWindowExW", uintptr(0), className, "LAN Monitor",
		style, px(100), px(100),
		int(frame.Right-frame.Left), int(frame.Bottom-frame.Top),
		uintptr(0), uintptr(0), instance, uintptr(0))
	if mainWindow == 0 {
		return
	}

	subnetEdit = createControl("EDIT", defaultSubnet(), wsBorder, 12, 12, 220, 26, idSubnet)
	scanButton = createControl("BUTTON", "Scan", 0, 242, 12, 80, 26, idScan)
	createControl("BUTTON", "Stop", 0, 330, 12, 80, 26, idStop)
	pollCheck = createControl("BUTTON", "Continuous polling", bsAutoCheckBox, 12, 46, 170, 26, idPoll)
	intervalBox = createControl("EDIT", strconv.Itoa(minIntervalSeconds), wsBorder|esNumber, 190, 46, 55, 26, idInterval)
	createControl("STATIC", "seconds (5-3600)", 0, 252, 48, 160, 24, idHint)
	statusLine = createControl("STATIC", "Ready - no automatic scan", 0, 12, 78, clientWidth-30, 20, idStatus)

	call(user32, "ShowWindow", mainWindow, uintptr(swShow))
	call(user32, "UpdateWindow", mainWindow)

	var m msg
	for {
		if int32(call(user32, "GetMessageW", unsafe.Pointer(&m), uintptr(0), uintptr(0), uintptr(0))) <= 0 {
			break
		}
		call(user32, "TranslateMessage", unsafe.Pointer(&m))
		call(user32, "DispatchMessageW", unsafe.Pointer(&m))
	}
}
