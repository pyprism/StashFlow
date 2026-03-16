package web

import (
	"encoding/json"
	"testing"
	"time"
)

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

func TestHubMultipleSubscribers(t *testing.T) {
	hub := NewHub()
	ch1 := hub.Subscribe()
	ch2 := hub.Subscribe()
	ch3 := hub.Subscribe()
	defer hub.Unsubscribe(ch1)
	defer hub.Unsubscribe(ch2)
	defer hub.Unsubscribe(ch3)

	hub.Broadcast(map[string]any{"multi": true})

	for i, ch := range []chan []byte{ch1, ch2, ch3} {
		select {
		case msg := <-ch:
			if len(msg) == 0 {
				t.Errorf("subscriber %d got empty message", i)
			}
		default:
			t.Errorf("subscriber %d did not receive message", i)
		}
	}
}

func TestHubBroadcastNoSubscribers(t *testing.T) {
	hub := NewHub()
	// Should not panic with no subscribers.
	hub.Broadcast(map[string]any{"lonely": true})
}

func TestHubBroadcastDropsOnFullChannel(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// Fill the channel buffer (capacity is 5).
	for i := 0; i < 5; i++ {
		hub.Broadcast(map[string]any{"i": i})
	}

	// This sixth broadcast should be dropped, not block.
	done := make(chan struct{})
	go func() {
		hub.Broadcast(map[string]any{"overflow": true})
		close(done)
	}()

	select {
	case <-done:
		// Good — broadcast returned without blocking.
	case <-time.After(1 * time.Second):
		t.Fatal("Broadcast blocked on full channel")
	}
}

func TestHubUnsubscribeRemovesChannel(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()

	hub.mu.Lock()
	count := len(hub.subs)
	hub.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}

	hub.Unsubscribe(ch)

	hub.mu.Lock()
	count = len(hub.subs)
	hub.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", count)
	}
}

func TestHubBroadcastJSONContent(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	payload := map[string]any{"status": "ok", "count": 42}
	hub.Broadcast(payload)

	msg := <-ch
	var got map[string]any
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("broadcast message is not valid JSON: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", got["status"])
	}
}

func TestHubSubscribeReturnsBufferedChannel(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	if cap(ch) != 5 {
		t.Errorf("expected channel capacity 5, got %d", cap(ch))
	}
}

func TestHubBroadcastAfterUnsubscribe(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	hub.Unsubscribe(ch)

	// Broadcasting after all subscribers are gone should not panic.
	hub.Broadcast(map[string]any{"after": "unsub"})
}

