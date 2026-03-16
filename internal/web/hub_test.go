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

func TestHubBroadcast(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	hub.Broadcast(map[string]any{"ok": true})

	select {
	case msg := <-ch:
		if len(msg) == 0 {
			t.Fatal("expected broadcast payload")
		}
	default:
		t.Fatal("expected broadcast to deliver message")
	}
}
