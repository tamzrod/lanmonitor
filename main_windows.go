package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"runtime"
	"strconv"
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

	wmRoundProgress = 0x8001
	wmRoundDone     = 0x8002

	roundDiscovery = 1
	roundActive    = 2
	// roundCancelled is or-ed into the wmRoundDone code when the round was cut
	// short, so the handler can leave the "Stopped" status line alone.
	roundCancelled = 0x100

	bmGetCheck = 0x00F0
	bmSetCheck = 0x00F1
	bstChecked = 1

	idScan    = 1
	idStop    = 2
	idMonitor = 3
	idSubnet  = 10
	idHint    = 12
	idStatus  = 13

	discoveryTimerID = 1
	activeTimerID    = 2

	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipChildren = 0x02000000
	wsBorder       = 0x00800000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
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

	mainWindow   uintptr
	subnetEdit   uintptr
	monitorCheck uintptr
	statusLine   uintptr
	instance     uintptr
	guiFont      uintptr

	// uiScale converts the 96-DPI layout units in logic.go to device pixels.
	uiScale = 1.0

	// Round bookkeeping. Every field below is touched only by the UI thread,
	// which is why plain values are enough.
	monitoring      bool
	discoveryBusy   bool
	activeBusy      bool
	discoveryQueued bool
	discoverySeq    uint64
	activeSeq       uint64
	currentStatus   string

	discoveryProbes   atomic.Int32
	discoveryFailures atomic.Int32

	tokenMu     sync.Mutex
	cancelToken chan struct{}

	mapMu        sync.Mutex
	targetSubnet string
	scanSession  uint64
	hostStates   [hostLast + 1]hostRecord
	hostLocks    [hostLast + 1]sync.Mutex
)

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

// icmpPinger owns one ICMP handle for the lifetime of a probe.
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

// currentToken returns the cancellation token rounds started now must watch.
func currentToken() chan struct{} {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if cancelToken == nil {
		cancelToken = make(chan struct{})
	}
	return cancelToken
}

// cancelRounds stops every round that is currently draining and gives later
// rounds a fresh token.
func cancelRounds() {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if cancelToken != nil {
		close(cancelToken)
		cancelToken = nil
	}
}

func isCancelled(token chan struct{}) bool {
	select {
	case <-token:
		return true
	default:
		return false
	}
}

// startDiscovery runs one full sweep of the /24. It is used by the Scan button,
// by the 15 s timer and by a queued request.
func startDiscovery() {
	if discoveryBusy {
		if !discoveryQueued {
			discoveryQueued = true
			setStatus("Discovery running - one more sweep queued")
		}
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
		scanSession++
	}
	ids := targets(true, hostStates[:])
	session := scanSession
	mapMu.Unlock()

	discoveryBusy = true
	discoverySeq++
	discoveryProbes.Store(0)
	discoveryFailures.Store(0)
	call(user32, "EnableWindow", subnetEdit, uintptr(0))
	setStatus("Discovery scanning " + canonical + " ...")
	go runRound(base, ids, true, discoverySeq, session, currentToken())
}

// startActive probes the promoted hosts only. It is driven by the 1 s timer and
// does nothing while monitoring is off or a previous active round still runs.
func startActive() {
	if !monitoring || activeBusy {
		return
	}
	base, canonical, err := parseSubnet(controlText(subnetEdit))
	if err != nil {
		return
	}

	mapMu.Lock()
	if canonical != targetSubnet {
		mapMu.Unlock()
		return
	}
	ids := targets(false, hostStates[:])
	session := scanSession
	mapMu.Unlock()

	if len(ids) == 0 {
		return
	}
	activeBusy = true
	activeSeq++
	go runRound(base, ids, false, activeSeq, session, currentToken())
}

func runRound(base [4]byte, ids []int, discovery bool, seq, session uint64, token chan struct{}) {
	var wg sync.WaitGroup
	for _, i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			probe(base, i, discovery, session, token)
		}(i)
	}
	wg.Wait()

	code := uintptr(roundDiscovery)
	if !discovery {
		code = roundActive
	}
	if isCancelled(token) {
		code |= roundCancelled
	}
	call(user32, "PostMessageW", mainWindow, uintptr(wmRoundDone), code, uintptr(seq))
}

// probe runs one echo request and publishes the result. The per-address lock
// keeps a sweep and an active round from probing the same address at the same
// time, and keeps a late result from overwriting a newer one.
func probe(base [4]byte, i int, discovery bool, session uint64, token chan struct{}) {
	if isCancelled(token) {
		return
	}
	hostLocks[i].Lock()
	defer hostLocks[i].Unlock()
	if isCancelled(token) {
		return
	}

	if discovery {
		discoveryProbes.Add(1)
	}
	p, err := newPinger()
	if err != nil {
		if discovery {
			discoveryFailures.Add(1)
		}
		return
	}
	responded, rttMS := p.ping(hostAddr(base, i), pingTimeoutMS)
	p.close()

	mapMu.Lock()
	if session == scanSession && (discovery || hostStates[i].promoted) {
		hostStates[i].observe(discovery, responded, rttMS)
	}
	mapMu.Unlock()

	call(user32, "PostMessageW", mainWindow, uintptr(wmRoundProgress), uintptr(0), uintptr(0))
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

	var brushes [5]uintptr
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
	setStatus(formatStatus(subnet, i, rec))
}

// setMonitoring applies the Monitor mode check box. Enabling it starts a sweep
// immediately and arms the 15 s and 1 s timers; disabling it cancels rounds.
func setMonitoring(hwnd uintptr) {
	monitoring = call(user32, "SendMessageW", monitorCheck, uintptr(bmGetCheck), uintptr(0), uintptr(0)) == bstChecked
	call(user32, "KillTimer", hwnd, uintptr(discoveryTimerID))
	call(user32, "KillTimer", hwnd, uintptr(activeTimerID))

	if !monitoring {
		discoveryQueued = false
		cancelRounds()
		call(user32, "EnableWindow", subnetEdit, uintptr(1))
		setStatus("Monitoring off")
		return
	}

	call(user32, "SetTimer", hwnd, uintptr(discoveryTimerID), uintptr(discoveryIntervalMS), uintptr(0))
	call(user32, "SetTimer", hwnd, uintptr(activeTimerID), uintptr(activeIntervalMS), uintptr(0))
	call(user32, "EnableWindow", subnetEdit, uintptr(0))
	if discoveryBusy {
		setStatus("Monitoring on - discovery already running")
		return
	}
	startDiscovery()
}

// stopAll is the Stop button: it ends monitoring, cancels rounds in flight and
// discards any queued sweep.
func stopAll(hwnd uintptr) {
	monitoring = false
	discoveryQueued = false
	cancelRounds()
	call(user32, "SendMessageW", monitorCheck, uintptr(bmSetCheck), uintptr(0), uintptr(0))
	call(user32, "KillTimer", hwnd, uintptr(discoveryTimerID))
	call(user32, "KillTimer", hwnd, uintptr(activeTimerID))
	call(user32, "EnableWindow", subnetEdit, uintptr(1))
	setStatus("Stopped")
}

func handleRoundDone(hwnd, code, seq uintptr) {
	kind := int(code & 0xFF)
	cancelled := code&roundCancelled != 0

	switch kind {
	case roundDiscovery:
		if uint64(seq) != discoverySeq {
			return
		}
		discoveryBusy = false
	case roundActive:
		if uint64(seq) != activeSeq {
			return
		}
		activeBusy = false
	default:
		return
	}

	call(user32, "InvalidateRect", hwnd, unsafe.Pointer(nil), uintptr(0))
	if cancelled || kind != roundDiscovery {
		return
	}

	if probes, failures := discoveryProbes.Load(), discoveryFailures.Load(); probes > 0 && failures >= probes {
		setStatus("Discovery failed - could not open an ICMP handle")
		return
	}
	if discoveryQueued {
		discoveryQueued = false
		startDiscovery()
		return
	}
	if monitoring {
		setStatus("Monitoring - discovery complete, next sweep in 15 s")
		return
	}
	setStatus("Scan complete")
}

func windowProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmDestroy:
		cancelRounds()
		call(user32, "KillTimer", hwnd, uintptr(discoveryTimerID))
		call(user32, "KillTimer", hwnd, uintptr(activeTimerID))
		call(user32, "PostQuitMessage", uintptr(0))
		return 0
	case wmPaint:
		paintWindow()
		return 0
	case wmCommand:
		switch int(w & 0xffff) {
		case idScan:
			startDiscovery()
		case idStop:
			stopAll(hwnd)
		case idMonitor:
			setMonitoring(hwnd)
		}
		return 0
	case wmTimer:
		if !monitoring {
			return 0
		}
		switch w {
		case uintptr(discoveryTimerID):
			startDiscovery()
		case uintptr(activeTimerID):
			startActive()
		}
		return 0
	case wmMouseMove:
		handleMouseMove(l)
		return 0
	case wmRoundProgress:
		call(user32, "InvalidateRect", hwnd, unsafe.Pointer(nil), uintptr(0))
		return 0
	case wmRoundDone:
		handleRoundDone(hwnd, w, l)
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

	title := fmt.Sprintf("%s — Version %d", appName, appVersion)
	style := uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	frame := rect{0, 0, int32(px(clientWidth)), int32(px(clientHeight))}
	call(user32, "AdjustWindowRect", unsafe.Pointer(&frame), style, uintptr(0))
	mainWindow = call(user32, "CreateWindowExW", uintptr(0), className, title,
		style, px(100), px(100),
		int(frame.Right-frame.Left), int(frame.Bottom-frame.Top),
		uintptr(0), uintptr(0), instance, uintptr(0))
	if mainWindow == 0 {
		return
	}

	subnetEdit = createControl("EDIT", defaultSubnet(), wsBorder, 12, 12, 220, 26, idSubnet)
	createControl("BUTTON", "Scan", 0, 242, 12, 80, 26, idScan)
	createControl("BUTTON", "Stop", 0, 330, 12, 80, 26, idStop)
	monitorCheck = createControl("BUTTON", "Monitor mode", bsAutoCheckBox, 12, 46, 170, 26, idMonitor)
	createControl("STATIC", "Discovery 15 s | Active 1 s", 0, 190, 48, 260, 24, idHint)
	statusLine = createControl("STATIC", "Ready - monitoring off", 0, 12, 78, clientWidth-30, 20, idStatus)

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
