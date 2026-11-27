package main

import (
 "encoding/binary"
 "fmt"
 "net"
 "runtime"
 "sync"
 "sync/atomic"
 "syscall"
 "unsafe"
)

var u=syscall.NewLazyDLL("user32.dll")
var g=syscall.NewLazyDLL("gdi32.dll")
var k=syscall.NewLazyDLL("kernel32.dll")
var ip=syscall.NewLazyDLL("iphlpapi.dll")
func call(d *syscall.LazyDLL,n string,a ...uintptr) uintptr { v,_,_:=d.NewProc(n).Call(a...);return v }
func str(s string) uintptr { p,_:=syscall.UTF16PtrFromString(s);return uintptr(unsafe.Pointer(p)) }
type rect struct{ L,T,R,B int32 }
type point struct{ X,Y int32 }
type msg struct{ H uintptr; M uint32; W,L uintptr; Time uint32; P point; Private uint32 }
type wc struct{ Style uint32; Proc uintptr; ClsExtra,WndExtra int32; Instance,Icon,Cursor,Background,Menu,Class uintptr }
type paint struct{ DC uintptr; Erase int32; R rect; Restore,Update int32; Reserved [32]byte }
type echoReply struct{ Address,Status,RTT uint32; Size,Reserved uint16; Data uintptr; Options [16]byte }
type host struct{ state int; rtt uint32; seen bool }
var hosts [255]host
var mu sync.Mutex
var busy,cancel atomic.Bool
var window,edit,auto,interval,status,scanButton uintptr
var target string
var active bool
var generation uint64
func label(s string){call(u,"SetWindowTextW",status,str(s))}
func control(class,title string,style uintptr,x,y,w,h,id uintptr)uintptr{return call(u,"CreateWindowExW",0,str(class),str(title),0x50000000|style,x,y,w,h,window,id,0,0)}
func read(h uintptr)string{var b [256]uint16;call(u,"GetWindowTextW",h,uintptr(unsafe.Pointer(&b[0])),256);return syscall.UTF16ToString(b[:])}
func ping(h uintptr,a net.IP)(bool,uint32){
 var buf [256]byte
 v:=call(ip,"IcmpSendEcho",h,uintptr(binary.LittleEndian.Uint32(a.To4())),0,0,0,uintptr(unsafe.Pointer(&buf[0])),uintptr(len(buf)),500)
 if v==0{return false,0}
 p:=(*echoReply)(unsafe.Pointer(&buf[0]));return p.Status==0,p.RTT
}
func start(){
 if busy.Load(){return}
 _,n,e:=net.ParseCIDR(read(edit));if e!=nil||n.IP.To4()==nil {label("Enter an IPv4 /24 subnet");return}
 ones,bits:=n.Mask.Size();if ones!=24||bits!=32{label("Only IPv4 /24 is supported");return}
 base:=n.IP.To4();next:=n.String()
 mu.Lock();if target!=next{hosts=[255]host{};target=next};mu.Unlock()
 busy.Store(true);cancel.Store(false);generation++;gen:=generation
 call(u,"EnableWindow",edit,0);call(u,"EnableWindow",scanButton,0)
 label("Scanning "+next+" ...")
 go func(){
  jobs:=make(chan int);var wg sync.WaitGroup
  for j:=0;j<16;j++{wg.Add(1);go func(){defer wg.Done();h:=call(ip,"IcmpCreateFile");if h==^uintptr(0)||h==0{for range jobs{};return};defer call(ip,"IcmpCloseHandle",h)
   for i:=range jobs{if cancel.Load(){continue};a:=net.IPv4(base[0],base[1],base[2],byte(i));ok,rtt:=ping(h,a)
    mu.Lock();p:=&hosts[i];if ok{p.state=1;p.seen=true;p.rtt=rtt}else if p.seen{p.state=2}else{p.state=3};mu.Unlock()
    call(u,"PostMessageW",window,0x8001,0,0)
   }
  }()}
  for i:=1;i<=254&&!cancel.Load();i++{jobs<-i};close(jobs);wg.Wait()
  call(u,"PostMessageW",window,0x8002,uintptr(gen),0)
 }()
}
func cell(i int)rect{x:=int32((i-1)%16)*43+12;y:=int32((i-1)/16)*28+100;return rect{x,y,x+40,y+25}}
func draw(){
 var p paint;dc:=call(u,"BeginPaint",window,uintptr(unsafe.Pointer(&p)));defer call(u,"EndPaint",window,uintptr(unsafe.Pointer(&p)))
 mu.Lock();defer mu.Unlock()
 colors:=[]uintptr{0xaaaaaa,0x459520,0x4040d0,0x606060}
 call(g,"SetBkMode",dc,1);call(g,"SetTextColor",dc,0xffffff)
 for i:=1;i<=254;i++{r:=cell(i);b:=call(g,"CreateSolidBrush",colors[hosts[i].state]);call(u,"FillRect",dc,uintptr(unsafe.Pointer(&r)),b);call(g,"DeleteObject",b);s:=fmt.Sprint(i);call(u,"DrawTextW",dc,str(s),uintptr(len(s)),uintptr(unsafe.Pointer(&r)),0x25)}
}
func proc(h uintptr,m uint32,w,l uintptr)uintptr{
 switch m{
 case 2:cancel.Store(true);call(u,"PostQuitMessage",0);return 0
 case 15:if window!=0{draw();return 0}
 case 0x111:
 switch w&0xffff{
 case 1:start()
 case 2:cancel.Store(true);active=false;call(u,"SendMessageW",auto,0xf1,0,0);call(u,"KillTimer",h,1)
 case 3:active=call(u,"SendMessageW",auto,0xf0,0,0)==1;call(u,"KillTimer",h,1);if active{var sec int;if _,e:=fmt.Sscan(read(interval),&sec);e!=nil||sec<5||sec>3600{sec=5};call(u,"SetTimer",h,1,uintptr(sec*1000),0);start()}
 };return 0
 case 0x113:if active{start()};return 0
 case 0x8001:call(u,"InvalidateRect",h,0,0);return 0
 case 0x8002:if uint64(w)==generation{busy.Store(false);call(u,"EnableWindow",edit,1);call(u,"EnableWindow",scanButton,1);if cancel.Load(){label("Scan stopped")}else{label("Scan complete")}};return 0
 case 0x200:
 x,y:=int32(int16(l&0xffff)),int32(int16((l>>16)&0xffff))
 if x>=12&&y>=100{col,row:=(x-12)/43,(y-100)/28;i:=int(row*16+col+1);if col<16&&i<=254{mu.Lock();p:=hosts[i];t:=target;mu.Unlock();states:=[]string{"Not scanned","Responding","Lost response","No response"};s:=fmt.Sprintf("%s | Host .%d | %s",t,i,states[p.state]);if p.state==1{s+=fmt.Sprintf(" | %d ms",p.rtt)};label(s)}};return 0
 }
 return call(u,"DefWindowProcW",h,uintptr(m),w,l)
}
func defaultSubnet()string{
 ifaces,_:=net.Interfaces();for _,f:=range ifaces{if f.Flags&net.FlagUp==0||f.Flags&net.FlagLoopback!=0{continue};as,_:=f.Addrs();for _,a:=range as{v,n,e:=net.ParseCIDR(a.String());if e==nil&&v.To4()!=nil{ones,_:=n.Mask.Size();if ones==24{return n.String()}}}};return ""
}
func main(){
 runtime.LockOSThread()
 instance:=call(k,"GetModuleHandleW",0);class:=str("LANMonitorWindow")
 c:=wc{Proc:syscall.NewCallback(proc),Instance:instance,Cursor:call(u,"LoadCursorW",0,32512),Background:6,Class:class}
 if call(u,"RegisterClassW",uintptr(unsafe.Pointer(&c)))==0{return}
 window=call(u,"CreateWindowExW",0,class,str("LAN Monitor"),0x00ca0000,100,100,720,610,0,0,instance,0)
 if window==0{return}
 edit=control("EDIT",defaultSubnet(),0x800000,12,12,220,26,10)
 scanButton=control("BUTTON","Scan",0,242,12,80,26,1)
 control("BUTTON","Stop",0,330,12,80,26,2)
 auto=control("BUTTON","Continuous polling",3,12,46,170,26,3)
 interval=control("EDIT","5",0x800000|0x2000,190,46,55,26,11)
 control("STATIC","seconds (5-3600)",0,252,48,160,24,12)
 status=control("STATIC","Ready - no automatic scan",0,12,78,680,20,13)
 call(u,"ShowWindow",window,5);call(u,"UpdateWindow",window)
 var m msg;for{v:=int32(call(u,"GetMessageW",uintptr(unsafe.Pointer(&m)),0,0,0));if v<=0{break};call(u,"TranslateMessage",uintptr(unsafe.Pointer(&m)));call(u,"DispatchMessageW",uintptr(unsafe.Pointer(&m)))}
}
