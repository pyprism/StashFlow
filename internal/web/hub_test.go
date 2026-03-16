package web

import "testing"

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	
	ch := hub.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe returned nil")
	}
	
	hub.Unsubscribe(ch)
}
