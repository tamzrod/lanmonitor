package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
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
	shell32  = syscall.NewLazyDLL("shell32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
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
	// nmhdr mirrors NMHDR, the header of every WM_NOTIFY payload.
	nmhdr struct {
		HwndFrom uintptr
		IDFrom   uintptr
		Code     uint32
	}
	initCommonControlsEx struct {
		Size uint32
		ICC  uint32
	}
)

const (
	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmSetFont     = 0x0030
	wmNotify      = 0x004E
	wmCommand     = 0x0111
	wmTimer       = 0x0113
	wmMouseMove   = 0x0200
	wmContextMenu = 0x007B
	wmNull        = 0x0000

	wmRoundProgress = 0x8001
	wmRoundDone     = 0x8002
	wmComboCommit   = 0x8003
	wmComboRestore  = 0x8004

	nmClick  = 0xFFFFFFFE
	nmReturn = 0xFFFFFFFF

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
	idRefresh = 4
	idSubnet  = 10
	idHint    = 12
	idStatus  = 13

	idAbout       = 200
	idAboutOK     = 1
	idAboutCancel = 2
	idAboutLink   = 201

	idPortScheme = 20
	idPortEdit   = 21
	idPortOpen   = 1
	idPortCancel = 2

	discoveryTimerID = 1
	activeTimerID    = 2

	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipChildren = 0x02000000
	wsBorder       = 0x00800000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsPopup        = 0x80000000
	wsTabStop      = 0x00010000
	wsVScroll      = 0x00200000

	bsAutoCheckBox  = 0x00000003
	bsDefPushButton = 0x00000001
	cbsAutoHScroll  = 0x0040
	cbsDropDown     = 0x0002
	cbsDropDownList = 0x0003
	cbAddString     = 0x0143
	cbGetCurSel     = 0x0147
	cbResetContent  = 0x014B
	cbSetCurSel     = 0x014E
	cbnDropdown     = 7
	cbnSelEndOk     = 9
	cbnSelEndCancel = 10
	esNumber        = 0x2000

	mfString    = 0x00000000
	mfPopup     = 0x00000010
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNonotify    = 0x0080

	mbOK          = 0x00000000
	mbIconWarning = 0x00000030

	swShow      = 5
	swpNoZOrder = 0x0004
	dtCell      = 0x00000001 | 0x00000004 | 0x00000020 // DT_CENTER|DT_VCENTER|DT_SINGLELINE

	iconArrow       = 32512
	colorWindowBk   = 6
	colorDialogBk   = 15
	stockDefaultGUI = 17
	transparentBk   = 1
	iccLinkClass    = 0x00008000

	createNewConsole = 0x00000010

	logPixelsX       = 88
	fwNormal         = 400
	defaultCharset   = 1
	clearTypeQuality = 5
	uiFontPoints     = 13
)

var (
	procCache sync.Map

	mainWindow   uintptr
	subnetCombo  uintptr
	monitorCheck uintptr
	statusLine   uintptr
	instance     uintptr
	guiFont      uintptr

	// networkList mirrors the items currently shown in subnetCombo.
	networkList []networkCandidate

	// comboUpdating suppresses ComboBox notifications caused by our own edits.
	comboUpdating bool

	// selectedNetwork is the CIDR captured when a drop-down entry is chosen.
	// The commit handler uses it instead of the ComboBox text, which the native
	// control may still be rewriting.
	selectedNetwork string

	// subnet is the authoritative subnet state. The ComboBox text is only
	// pending input; every scan, monitor round, hover status and context-menu
	// action reads the committed value.
	subnet = newSubnetController()

	aboutWindow uintptr
	aboutLink   uintptr

	portWindow uintptr
	portScheme uintptr
	portEdit   uintptr

	portDialogResult customPort
	portDialogOK     bool

	// contextHost is the cell that opened the current context menu.
	contextHost int

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

	// hostLocks serialises probes of the same address. The host map itself is
	// owned by subnet.Model().
	hostLocks [hostLast + 1]sync.Mutex
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

// ptrFromUintptr recovers a pointer that Windows passed as an LPARAM. The
// intermediate variable keeps go vet's unsafeptr check quiet without changing
// the value.
func ptrFromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

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

func createControl(class, title string, style, x, y, w, h, id int) uintptr {
	return createChildControl(mainWindow, class, title, style, x, y, w, h, id)
}

func createChildControl(parent uintptr, class, title string, style, x, y, w, h, id int) uintptr {
	hwnd := call(user32, "CreateWindowExW", uintptr(0), class, title,
		uintptr(wsChild|wsVisible|style), px(x), px(y), px(w), px(h),
		parent, uintptr(id), instance, uintptr(0))
	if hwnd != 0 && guiFont != 0 {
		call(user32, "SendMessageW", hwnd, uintptr(wmSetFont), guiFont, uintptr(1))
	}
	return hwnd
}

func registerWindowClass(name string, proc uintptr, background uintptr) bool {
	cls, err := syscall.UTF16FromString(name)
	if err != nil {
		return false
	}
	wc := wndClass{
		WndProc:    proc,
		Instance:   instance,
		Cursor:     call(user32, "LoadCursorW", uintptr(0), uintptr(iconArrow)),
		Background: background,
		ClassName:  uintptr(unsafe.Pointer(&cls[0])),
	}
	ok := call(user32, "RegisterClassW", unsafe.Pointer(&wc)) != 0
	runtime.KeepAlive(cls)
	return ok
}

func createMainMenu() uintptr {
	menu := call(user32, "CreateMenu")
	if menu == 0 {
		return 0
	}
	help := call(user32, "CreatePopupMenu")
	if help == 0 {
		call(user32, "DestroyMenu", menu)
		return 0
	}
	call(user32, "AppendMenuW", help, uintptr(mfString), uintptr(idAbout), "&About")
	call(user32, "AppendMenuW", menu, uintptr(mfPopup), help, "&Help")
	return menu
}

func centerWindow(hwnd uintptr, w, h int) {
	var r rect
	if call(user32, "GetWindowRect", mainWindow, unsafe.Pointer(&r)) == 0 {
		return
	}
	x := (r.Left + r.Right - int32(px(w))) / 2
	y := (r.Top + r.Bottom - int32(px(h))) / 2
	call(user32, "SetWindowPos", hwnd, uintptr(0), uintptr(x), uintptr(y),
		uintptr(px(w)), uintptr(px(h)), uintptr(swpNoZOrder))
}

func showErrorAt(parent uintptr, text string) {
	call(user32, "MessageBoxW", parent, text, appName, uintptr(mbOK|mbIconWarning))
}

func showError(text string) {
	showErrorAt(mainWindow, text)
}

// openURL hands a URL to the registered Windows handler. ShellExecuteW does not
// start a shell and does not parse the URL as a command line.
func openURL(url string) error {
	r := call(shell32, "ShellExecuteW", uintptr(0), "open", url, uintptr(0), uintptr(0), uintptr(swShow))
	if r <= 32 {
		return fmt.Errorf("ShellExecuteW failed (%d)", r)
	}
	return nil
}

func copyToClipboard(text string) error {
	if call(user32, "OpenClipboard", mainWindow) == 0 {
		return fmt.Errorf("OpenClipboard failed")
	}
	defer call(user32, "CloseClipboard")
	if call(user32, "EmptyClipboard") == 0 {
		return fmt.Errorf("EmptyClipboard failed")
	}
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	h := call(kernel32, "GlobalAlloc", uintptr(0x0002), uintptr(len(utf16)*2)) // GMEM_MOVEABLE
	if h == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}
	p := call(kernel32, "GlobalLock", h)
	if p == 0 {
		call(kernel32, "GlobalFree", h)
		return fmt.Errorf("GlobalLock failed")
	}
	call(kernel32, "RtlMoveMemory", p, unsafe.Pointer(&utf16[0]), uintptr(len(utf16)*2))
	runtime.KeepAlive(utf16)
	call(kernel32, "GlobalUnlock", h)
	if call(user32, "SetClipboardData", uintptr(13), h) == 0 { // CF_UNICODETEXT
		call(kernel32, "GlobalFree", h)
		return fmt.Errorf("SetClipboardData failed")
	}
	return nil
}

// launch starts a prepared command. Executables receive their arguments one by
// one; no cmd.exe, shell or string interpolation is involved.
func launch(c command) error {
	switch c.Kind {
	case launchShell:
		return openURL(c.Target)
	case launchProcess:
		path, err := exec.LookPath(c.Target)
		if err != nil {
			return err
		}
		cmd := exec.Command(path, c.Args...)
		if c.NewConsole {
			cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: uint32(createNewConsole)}
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	case launchClipboard:
		return copyToClipboard(c.Target)
	}
	return errUnknownAction
}

func showContextMenu() {
	var pt point
	if call(user32, "GetCursorPos", unsafe.Pointer(&pt)) == 0 {
		return
	}
	client := pt
	if call(user32, "ScreenToClient", mainWindow, unsafe.Pointer(&client)) == 0 {
		return
	}
	i, ok := hostAt(unpx(int(client.X)), unpx(int(client.Y)))
	if !ok {
		return
	}
	contextHost = i

	menu := call(user32, "CreatePopupMenu")
	if menu == 0 {
		return
	}
	for _, item := range contextMenuItems {
		if item.ID == ctxContinuousPing {
			call(user32, "AppendMenuW", menu, uintptr(mfSeparator), uintptr(0), uintptr(0))
		}
		call(user32, "AppendMenuW", menu, uintptr(mfString), uintptr(item.ID), item.Label)
	}
	call(user32, "SetForegroundWindow", mainWindow)
	cmd := call(user32, "TrackPopupMenu", menu,
		uintptr(tpmRightButton|tpmReturnCmd|tpmNonotify),
		uintptr(int(pt.X)), uintptr(int(pt.Y)), uintptr(0), mainWindow, uintptr(0))
	call(user32, "DestroyMenu", menu)
	call(user32, "PostMessageW", mainWindow, uintptr(wmNull), uintptr(0), uintptr(0))
	if cmd != 0 {
		handleContextCommand(int(cmd))
	}
}

func handleContextCommand(cmd int) {
	action, ok := contextAction(cmd)
	if !ok {
		return
	}
	r := commitPendingSubnet()
	if r.Err != nil {
		showError("Enter a valid IPv4 /24 subnet before opening a host.")
		return
	}
	if r.Changed && monitoring {
		startDiscovery()
	}
	ip, err := hostAddress(r.Canonical, contextHost)
	if err != nil {
		showError("Enter a valid IPv4 /24 subnet before opening a host.")
		return
	}

	var custom customPort
	if action == actionCustomPort {
		custom, ok = showCustomPortDialog(ip)
		if !ok {
			return
		}
	}

	c, err := buildClientCommand(action, ip, custom)
	if err != nil {
		showError(err.Error())
		return
	}
	if err := launch(c); err != nil {
		showError(unavailableMessage(action))
		return
	}

	switch action {
	case actionCopyIP:
		setStatus("Copied " + ip)
	case actionCustomPort:
		setStatus("Opened " + custom.Scheme + " on " + ip + " port " + strconv.Itoa(int(custom.Port)))
	default:
		setStatus("Opened " + action.String() + " for " + ip)
	}
}

func showCustomPortDialog(ip string) (customPort, bool) {
	if portWindow != 0 {
		call(user32, "SetForegroundWindow", portWindow)
		return portDialogResult, portDialogOK
	}
	portDialogResult = customPort{Scheme: "http", Port: 8080}
	portDialogOK = false

	call(user32, "EnableWindow", mainWindow, 0)
	portWindow = call(user32, "CreateWindowExW", uintptr(0), "LANMonitorPortDialog",
		"Custom Port — "+ip, uintptr(wsPopup|wsCaption|wsSysMenu),
		px(200), px(200), px(360), px(230), mainWindow, uintptr(0), instance, uintptr(0))
	if portWindow == 0 {
		call(user32, "EnableWindow", mainWindow, 1)
		showError("Could not open the Custom Port dialog.")
		return customPort{}, false
	}

	createChildControl(portWindow, "STATIC", "Host: "+ip, 0, 16, 16, 320, 20, 0)
	createChildControl(portWindow, "STATIC", "Protocol", 0, 16, 50, 90, 20, 0)
	portScheme = createChildControl(portWindow, "COMBOBOX", "", cbsDropDownList|wsTabStop|wsVScroll,
		110, 48, 220, 120, idPortScheme)
	call(user32, "SendMessageW", portScheme, uintptr(cbAddString), uintptr(0), "HTTP")
	call(user32, "SendMessageW", portScheme, uintptr(cbAddString), uintptr(0), "HTTPS")
	call(user32, "SendMessageW", portScheme, uintptr(cbAddString), uintptr(0), "SSH")
	call(user32, "SendMessageW", portScheme, uintptr(cbSetCurSel), uintptr(0), uintptr(0))
	createChildControl(portWindow, "STATIC", "Port (1-65535)", 0, 16, 86, 90, 20, 0)
	portEdit = createChildControl(portWindow, "EDIT", "8080", wsBorder|esNumber|wsTabStop,
		110, 84, 100, 24, idPortEdit)
	createChildControl(portWindow, "BUTTON", "Open", bsDefPushButton|wsTabStop, 110, 124, 90, 28, idPortOpen)
	createChildControl(portWindow, "BUTTON", "Cancel", wsTabStop, 210, 124, 90, 28, idPortCancel)
	createChildControl(portWindow, "STATIC", "HTTP and HTTPS open a URL; SSH uses -p.", 0, 16, 160, 320, 20, 0)

	centerWindow(portWindow, 360, 230)
	call(user32, "ShowWindow", portWindow, uintptr(swShow))
	call(user32, "SetFocus", portEdit)

	var m msg
	for portWindow != 0 {
		r := int32(call(user32, "GetMessageW", unsafe.Pointer(&m), uintptr(0), uintptr(0), uintptr(0)))
		if r <= 0 {
			break
		}
		if call(user32, "IsDialogMessageW", portWindow, unsafe.Pointer(&m)) != 0 {
			continue
		}
		call(user32, "TranslateMessage", unsafe.Pointer(&m))
		call(user32, "DispatchMessageW", unsafe.Pointer(&m))
	}
	call(user32, "EnableWindow", mainWindow, 1)
	call(user32, "SetForegroundWindow", mainWindow)
	return portDialogResult, portDialogOK
}

func portDialogProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmCommand:
		switch int(w & 0xffff) {
		case idPortOpen:
			idx := int(call(user32, "SendMessageW", portScheme, uintptr(cbGetCurSel), uintptr(0), uintptr(0)))
			scheme := "http"
			switch idx {
			case 1:
				scheme = "https"
			case 2:
				scheme = "ssh"
			}
			port, err := parsePort(controlText(portEdit))
			if err != nil {
				showErrorAt(hwnd, err.Error())
				call(user32, "SetFocus", portEdit)
				return 0
			}
			portDialogResult = customPort{Scheme: scheme, Port: port}
			portDialogOK = true
			call(user32, "DestroyWindow", hwnd)
		case idPortCancel:
			call(user32, "DestroyWindow", hwnd)
		}
		return 0
	case wmClose:
		call(user32, "DestroyWindow", hwnd)
		return 0
	case wmDestroy:
		portWindow = 0
		portScheme = 0
		portEdit = 0
		return 0
	}
	return call(user32, "DefWindowProcW", hwnd, m, w, l)
}

func showAbout() {
	if aboutWindow != 0 {
		call(user32, "SetForegroundWindow", aboutWindow)
		return
	}
	call(user32, "EnableWindow", mainWindow, 0)
	aboutWindow = call(user32, "CreateWindowExW", uintptr(0), "LANMonitorAbout", "About LAN Monitor",
		uintptr(wsPopup|wsCaption|wsSysMenu), px(200), px(200), px(520), px(280),
		mainWindow, uintptr(0), instance, uintptr(0))
	if aboutWindow == 0 {
		call(user32, "EnableWindow", mainWindow, 1)
		showError("Could not open the About dialog.")
		return
	}

	createChildControl(aboutWindow, "STATIC", windowTitle(), 0, 20, 20, 480, 24, 0)
	createChildControl(aboutWindow, "STATIC", "Portable Windows AMD64 IPv4 /24 monitor", 0, 20, 50, 480, 20, 0)
	createChildControl(aboutWindow, "STATIC", "MIT License", 0, 20, 74, 480, 20, 0)
	createChildControl(aboutWindow, "STATIC", "Project:", 0, 20, 106, 60, 20, 0)
	linkText := fmt.Sprintf(`<a href="%s">%s</a>`, projectURL, projectURL)
	aboutLink = createChildControl(aboutWindow, "SysLink", linkText, wsTabStop, 82, 104, 420, 22, idAboutLink)
	if aboutLink == 0 {
		aboutLink = createChildControl(aboutWindow, "BUTTON", projectURL, wsTabStop, 82, 102, 420, 26, idAboutLink)
	}
	createChildControl(aboutWindow, "STATIC", aboutQuote, 0, 20, 144, 480, 24, 0)
	createChildControl(aboutWindow, "BUTTON", "OK", bsDefPushButton|wsTabStop, 210, 200, 100, 30, idAboutOK)

	centerWindow(aboutWindow, 520, 280)
	call(user32, "ShowWindow", aboutWindow, uintptr(swShow))
	call(user32, "SetFocus", aboutLink)

	var m msg
	for aboutWindow != 0 {
		r := int32(call(user32, "GetMessageW", unsafe.Pointer(&m), uintptr(0), uintptr(0), uintptr(0)))
		if r <= 0 {
			break
		}
		if call(user32, "IsDialogMessageW", aboutWindow, unsafe.Pointer(&m)) != 0 {
			continue
		}
		call(user32, "TranslateMessage", unsafe.Pointer(&m))
		call(user32, "DispatchMessageW", unsafe.Pointer(&m))
	}
	call(user32, "EnableWindow", mainWindow, 1)
	call(user32, "SetForegroundWindow", mainWindow)
}

func aboutProc(hwnd, m, w, l uintptr) uintptr {
	switch uint32(m) {
	case wmCommand:
		switch int(w & 0xffff) {
		case idAboutOK, idAboutCancel:
			call(user32, "DestroyWindow", hwnd)
		case idAboutLink:
			if err := openURL(projectURL); err != nil {
				showErrorAt(hwnd, unavailableMessage(actionHTTP))
			}
		}
		return 0
	case wmNotify:
		if l != 0 {
			hdr := (*nmhdr)(ptrFromUintptr(l))
			if hdr.HwndFrom == aboutLink && (hdr.Code == nmClick || hdr.Code == nmReturn) {
				if err := openURL(projectURL); err != nil {
					showErrorAt(hwnd, unavailableMessage(actionHTTP))
				}
			}
		}
		return 0
	case wmClose:
		call(user32, "DestroyWindow", hwnd)
		return 0
	case wmDestroy:
		aboutWindow = 0
		aboutLink = 0
		return 0
	}
	return call(user32, "DefWindowProcW", hwnd, m, w, l)
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

// startDiscovery runs one full sweep of the committed /24. It is used by the
// Scan button, by the 15 s timer and by a queued request. The subnet is never
// read from the ComboBox here.
func startDiscovery() {
	if discoveryBusy {
		if !discoveryQueued {
			discoveryQueued = true
			setStatus("Discovery running - one more sweep queued")
		}
		return
	}
	base, canonical, session, ok := subnet.Model().Current()
	if !ok {
		setStatus(errSubnetSyntax.Error())
		return
	}
	ids := subnet.Model().Targets(true)

	discoveryBusy = true
	discoverySeq++
	discoveryProbes.Store(0)
	discoveryFailures.Store(0)
	setStatus("Discovery scanning " + canonical + " ...")
	go runRound(base, ids, true, discoverySeq, session, currentToken())
}

// startActive probes the promoted hosts of the committed subnet only. It is
// driven by the 1 s timer and does nothing while monitoring is off or a
// previous active round still runs.
func startActive() {
	if !monitoring || activeBusy {
		return
	}
	base, _, session, ok := subnet.Model().Current()
	if !ok {
		return
	}
	ids := subnet.Model().Targets(false)
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

	subnet.Model().Observe(session, i, discovery, responded, rttMS)

	call(user32, "PostMessageW", mainWindow, uintptr(wmRoundProgress), uintptr(0), uintptr(0))
}

// stateTextColor keeps the address label readable on every cell colour.
func stateTextColor(s hostState) uintptr {
	if s == stateUnscanned {
		return 0x00000000
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

	snapshot := subnet.Model().Snapshot()

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
	_, canonical, _, _ := subnet.Model().Current()
	setStatus(formatStatus(canonical, i, subnet.Model().Host(i)))
}

// setMonitoring applies the Monitor mode check box. Enabling it commits the
// pending subnet, starts a sweep immediately and arms the 15 s and 1 s timers;
// disabling it cancels rounds.
func setMonitoring(hwnd uintptr) {
	monitoring = call(user32, "SendMessageW", monitorCheck, uintptr(bmGetCheck), uintptr(0), uintptr(0)) == bstChecked
	call(user32, "KillTimer", hwnd, uintptr(discoveryTimerID))
	call(user32, "KillTimer", hwnd, uintptr(activeTimerID))

	if !monitoring {
		discoveryQueued = false
		cancelRounds()
		setStatus("Monitoring off")
		return
	}

	if r := commitPendingSubnet(); r.Err != nil {
		monitoring = false
		call(user32, "SendMessageW", monitorCheck, uintptr(bmSetCheck), uintptr(0), uintptr(0))
		setStatus(r.Err.Error())
		return
	}

	call(user32, "SetTimer", hwnd, uintptr(discoveryTimerID), uintptr(discoveryIntervalMS), uintptr(0))
	call(user32, "SetTimer", hwnd, uintptr(activeTimerID), uintptr(activeIntervalMS), uintptr(0))
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
	if kind != roundDiscovery {
		return
	}

	// A sweep queued after Stop or after a cancelled round is an explicit Scan
	// request, so it still runs. Every cancellation path clears the queue first,
	// so a sweep queued before Stop is never started by this check.
	if discoveryQueued {
		discoveryQueued = false
		startDiscovery()
		return
	}
	if cancelled {
		return
	}

	if probes, failures := discoveryProbes.Load(), discoveryFailures.Load(); probes > 0 && failures >= probes {
		setStatus("Discovery failed - could not open an ICMP handle")
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
			if r := commitPendingSubnet(); r.Err != nil {
				setStatus(r.Err.Error())
			} else {
				startDiscovery()
			}
		case idStop:
			stopAll(hwnd)
		case idMonitor:
			setMonitoring(hwnd)
		case idSubnet:
			if !comboUpdating {
				// The selected CIDR is captured here, while the control still
				// knows its list index. The posted commit then runs after the
				// native control has finished, and never reads the edit text.
				switch uint32(w) >> 16 {
				case cbnDropdown:
					selectedNetwork = ""
					subnet.BeginDrop(controlText(subnetCombo))
				case cbnSelEndOk:
					selectedNetwork = selectedNetworkCIDR()
					call(user32, "PostMessageW", hwnd, uintptr(wmComboCommit), uintptr(0), uintptr(0))
				case cbnSelEndCancel:
					call(user32, "PostMessageW", hwnd, uintptr(wmComboRestore), uintptr(0), uintptr(0))
				}
			}
		case idRefresh:
			refreshNetworks()
		case idAbout:
			showAbout()
		}
		return 0
	case wmTimer:
		if !monitoring {
			return 0
		}
		switch w {
		case uintptr(discoveryTimerID):
			syncPendingSubnet()
			startDiscovery()
		case uintptr(activeTimerID):
			startActive()
		}
		return 0
	case wmMouseMove:
		handleMouseMove(l)
		return 0
	case wmContextMenu:
		showContextMenu()
		return 0
	case wmRoundProgress:
		call(user32, "InvalidateRect", hwnd, unsafe.Pointer(nil), uintptr(0))
		return 0
	case wmRoundDone:
		handleRoundDone(hwnd, w, l)
		return 0
	case wmComboCommit:
		commitSelectedNetwork()
		return 0
	case wmComboRestore:
		restoreAfterDrop()
		return 0
	}
	return call(user32, "DefWindowProcW", hwnd, m, w, l)
}

// refreshNetworkList rebuilds the ComboBox items. With preserve, the pending
// edit text (including a manually typed CIDR) is kept and the committed subnet
// is untouched. Without preserve, the first detected network becomes the
// committed default. Detection never sends ICMP.
func refreshNetworkList(preserve bool) {
	subnet.SetText(resolveSubnetInput(controlText(subnetCombo), networkList))
	networkList = detectedNetworks()
	selectedNetwork = ""

	comboUpdating = true
	call(user32, "SendMessageW", subnetCombo, uintptr(cbResetContent), uintptr(0), uintptr(0))
	for _, c := range networkList {
		call(user32, "SendMessageW", subnetCombo, uintptr(cbAddString), uintptr(0), c.Label())
	}
	comboUpdating = false

	if !preserve {
		if len(networkList) > 0 {
			applyCommit(subnet.Select(networkList[0].CIDR))
		} else {
			subnet.SetText("")
			setComboText("")
		}
		return
	}

	text := subnet.Refresh()
	if strings.TrimSpace(text) == "" && len(networkList) > 0 {
		text = networkList[0].CIDR
		subnet.SetText(text)
	}
	setComboText(text)
}

// setComboText replaces the edit text without changing the list selection.
// Notifications caused by this programmatic edit are suppressed.
func setComboText(text string) {
	comboUpdating = true
	call(user32, "SetWindowTextW", subnetCombo, text)
	comboUpdating = false
}

func refreshNetworks() {
	refreshNetworkList(true)
	if len(networkList) == 0 {
		setStatus("No IPv4 networks detected - type a /24 manually")
		return
	}
	setStatus(fmt.Sprintf("Network list refreshed - %d network(s)", len(networkList)))
}

// selectedNetworkCIDR reads the list entry the ComboBox settled on. It runs
// from the CBN_SELENDOK notification, before the posted commit, so the commit
// never depends on the transient edit text.
func selectedNetworkCIDR() string {
	idx := int(call(user32, "SendMessageW", subnetCombo, uintptr(cbGetCurSel), uintptr(0), uintptr(0)))
	if idx < 0 || idx >= len(networkList) {
		return ""
	}
	return networkList[idx].CIDR
}

// commitSelectedNetwork applies a drop-down choice captured at CBN_SELENDOK.
func commitSelectedNetwork() {
	cidr := selectedNetwork
	selectedNetwork = ""
	if cidr == "" {
		restoreAfterDrop()
		return
	}

	r := subnet.Select(cidr)
	applyCommit(r)
	if r.Err != nil {
		restoreAfterDrop()
		setStatus(r.Err.Error())
		return
	}
	if monitoring {
		startDiscovery()
		return
	}
	setStatus("Selected " + r.Canonical + " - press Scan")
}

// restoreAfterDrop handles CBN_SELENDCANCEL. If a selection was committed while
// the list was open, the committed CIDR wins; otherwise the pre-drop text
// returns. The committed subnet itself is never changed here.
func restoreAfterDrop() {
	setComboText(subnet.CancelDrop())
}

// commitPendingSubnet makes the current edit text authoritative before Scan,
// Monitor mode or a context-menu action. A subnet change discards the old map
// and every round that belongs to it.
func commitPendingSubnet() commitResult {
	subnet.SetText(resolveSubnetInput(controlText(subnetCombo), networkList))
	r := subnet.CommitPending()
	applyCommit(r)
	return r
}

// syncPendingSubnet lets a valid manually typed CIDR take effect on the next
// 15 s sweep without stopping monitoring. Invalid or partial text is ignored
// and the committed subnet keeps being monitored.
func syncPendingSubnet() {
	subnet.SetText(resolveSubnetInput(controlText(subnetCombo), networkList))
	r, ok := subnet.CommitPendingIfValid()
	if !ok {
		return
	}
	applyCommit(r)
}

// applyCommit makes a successful commit visible and, when the subnet changed,
// discards the map's rounds. A failed commit leaves the edit text alone so the
// user can correct it.
func applyCommit(r commitResult) {
	if r.Err != nil {
		return
	}
	setComboText(subnet.Text())
	if r.Changed {
		applySubnetChange()
	}
}

// applySubnetChange cancels the rounds of the previous subnet and invalidates
// their completions. The host map was already cleared by the model.
func applySubnetChange() {
	cancelRounds()
	discoverySeq++
	activeSeq++
	discoveryBusy = false
	activeBusy = false
	discoveryQueued = false
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

	icc := initCommonControlsEx{Size: uint32(unsafe.Sizeof(initCommonControlsEx{})), ICC: iccLinkClass}
	call(comctl32, "InitCommonControlsEx", unsafe.Pointer(&icc))

	registerWindowClass("LANMonitorWindow", syscall.NewCallback(windowProc), uintptr(colorWindowBk))
	registerWindowClass("LANMonitorAbout", syscall.NewCallback(aboutProc), uintptr(colorDialogBk))
	registerWindowClass("LANMonitorPortDialog", syscall.NewCallback(portDialogProc), uintptr(colorDialogBk))

	style := uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	frame := rect{0, 0, int32(px(clientWidth)), int32(px(clientHeight))}
	call(user32, "AdjustWindowRectEx", unsafe.Pointer(&frame), style, uintptr(1), uintptr(0))
	mainWindow = call(user32, "CreateWindowExW", uintptr(0), "LANMonitorWindow", windowTitle(),
		style, px(100), px(100),
		int(frame.Right-frame.Left), int(frame.Bottom-frame.Top),
		uintptr(0), uintptr(0), instance, uintptr(0))
	if mainWindow == 0 {
		return
	}
	if menu := createMainMenu(); menu != 0 {
		call(user32, "SetMenu", mainWindow, menu)
		call(user32, "DrawMenuBar", mainWindow)
	}

	subnetCombo = createControl("COMBOBOX", "", cbsDropDown|cbsAutoHScroll|wsVScroll|wsTabStop, 12, 12, 380, 200, idSubnet)
	createControl("BUTTON", "Refresh", 0, 400, 12, 70, 26, idRefresh)
	createControl("BUTTON", "Scan", 0, 478, 12, 80, 26, idScan)
	createControl("BUTTON", "Stop", 0, 566, 12, 80, 26, idStop)
	monitorCheck = createControl("BUTTON", "Monitor mode", bsAutoCheckBox, 12, 46, 170, 26, idMonitor)
	createControl("STATIC", "Select or type a /24 | 15 s / 1 s", 0, 190, 48, 320, 24, idHint)
	statusLine = createControl("STATIC", "Ready - monitoring off", 0, 12, 78, clientWidth-30, 20, idStatus)
	currentStatus = "Ready - monitoring off"
	refreshNetworkList(false)
	if len(networkList) == 0 {
		setStatus("No IPv4 networks detected - type a /24 manually")
	}

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
