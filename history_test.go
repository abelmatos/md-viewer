package main

import "testing"

func TestHistory(t *testing.T) {
	var h History
	if _, ok := h.Current(); ok || h.CanBack() || h.CanForward() {
		t.Fatal("empty history has entries")
	}
	h.Visit(Entry{Path: "a"})
	h.Visit(Entry{Path: "b"})
	h.SetScroll(120)
	h.Visit(Entry{Path: "c"})
	if e, _ := h.Back(); e.Path != "b" || e.Scroll != 120 {
		t.Fatalf("Back = %+v", e)
	}
	if !h.CanForward() {
		t.Fatal("no forward after back")
	}
	h.Visit(Entry{Path: "d"}) // drops c
	if h.CanForward() {
		t.Fatal("forward kept after visit")
	}
	h.Back()
	if e, _ := h.Back(); e.Path != "a" || h.CanBack() {
		t.Fatalf("Back = %+v", e)
	}
	if _, ok := h.Back(); ok {
		t.Fatal("Back past start")
	}
	if e, _ := h.Forward(); e.Path != "b" {
		t.Fatalf("Forward = %+v", e)
	}
}
