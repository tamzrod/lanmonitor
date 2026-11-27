package main

type host struct {
 state int
 rtt uint32
 seen, promoted bool
 misses int
}

func (h *host) observe(discovery, ok bool, rtt uint32) {
 if ok {
  h.seen=true
  h.rtt=rtt
  h.state=1
  if discovery {h.promoted=true;h.misses=0}
  return
 }
 if discovery && h.promoted {
  h.misses++
  if h.misses>=2 {h.promoted=false;h.state=2;return}
 }
 if h.promoted {h.state=4} else if h.seen {h.state=2} else {h.state=3}
}
