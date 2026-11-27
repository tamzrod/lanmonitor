package main
import "testing"
func TestLifecycle(t *testing.T){
 var h host
 h.observe(true,true,1)
 if !h.promoted {t.Fatal("not promoted")}
 for i:=0;i<5;i++{h.observe(false,false,0)}
 if !h.promoted||h.misses!=0{t.Fatal("active misses must not demote")}
 h.observe(true,false,0)
 if !h.promoted||h.misses!=1{t.Fatal("first discovery miss")}
 h.observe(true,true,2)
 if h.misses!=0{t.Fatal("recovery did not reset misses")}
 h.observe(true,false,0);h.observe(true,false,0)
 if h.promoted||h.state!=2{t.Fatal("not demoted")}
 h.observe(true,true,3)
 if !h.promoted||h.misses!=0{t.Fatal("not repromoted")}
}
