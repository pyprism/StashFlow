package torrent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStatusConstants(t *testing.T) {
	if StatusQueued != "queued" {
		t.Error("wrong queued status")
	}
	if StatusDownloading != "downloading" {
		t.Error("wrong downloading status")
	}
	if StatusPaused != "paused" {
		t.Error("wrong paused status")
	}
	if StatusCompleted != "completed" {
		t.Error("wrong completed status")
	}
	if StatusError != "error" {
		t.Error("wrong error status")
	}
}

func TestItemJSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	item := &Item{
		ID:           "test-id-123",
		Name:         "ubuntu-24.04.iso",
		InfoHash:     "0123456789abcdef0123456789abcdef01234567",
		SizeBytes:    4_700_000_000,
		Status:       StatusDownloading,
		Progress:     0.42,
		Downloaded:   1_974_000_000,
		AddedAt:      now,
		Magnet:       "magnet:?xt=urn:btih:abc123",
		ErrorMessage: "",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Item
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if got.ID != item.ID {
		t.Errorf("ID mismatch: want %q, got %q", item.ID, got.ID)
	}
	if got.Name != item.Name {
		t.Errorf("Name mismatch: want %q, got %q", item.Name, got.Name)
	}
	if got.InfoHash != item.InfoHash {
		t.Errorf("InfoHash mismatch: want %q, got %q", item.InfoHash, got.InfoHash)
	}
	if got.SizeBytes != item.SizeBytes {
		t.Errorf("SizeBytes mismatch: want %d, got %d", item.SizeBytes, got.SizeBytes)
	}
	if got.Status != item.Status {
		t.Errorf("Status mismatch: want %q, got %q", item.Status, got.Status)
	}
	if got.Progress != item.Progress {
		t.Errorf("Progress mismatch: want %f, got %f", item.Progress, got.Progress)
	}
	if got.Downloaded != item.Downloaded {
		t.Errorf("Downloaded mismatch: want %d, got %d", item.Downloaded, got.Downloaded)
	}
	if !got.AddedAt.Equal(item.AddedAt) {
		t.Errorf("AddedAt mismatch: want %v, got %v", item.AddedAt, got.AddedAt)
	}
	if got.Magnet != item.Magnet {
		t.Errorf("Magnet mismatch: want %q, got %q", item.Magnet, got.Magnet)
	}
}

func TestItemTorrentFileSerialization(t *testing.T) {
	item := &Item{
		ID:          "tf-1",
		Status:      StatusQueued,
		TorrentFile: "/path/to/file.torrent",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got Item
	json.Unmarshal(data, &got)
	if got.TorrentFile != item.TorrentFile {
		t.Errorf("TorrentFile mismatch: want %q, got %q", item.TorrentFile, got.TorrentFile)
	}
}

func TestItemErrorMessageSerialization(t *testing.T) {
	item := &Item{
		ID:           "err-1",
		Status:       StatusError,
		ErrorMessage: "torrent size exceeds the max allowed storage",
	}

	data, _ := json.Marshal(item)
	var got Item
	json.Unmarshal(data, &got)

	if got.ErrorMessage != item.ErrorMessage {
		t.Errorf("ErrorMessage mismatch: want %q, got %q", item.ErrorMessage, got.ErrorMessage)
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	state := &State{
		Items: []*Item{
			{ID: "a", Name: "alpha", Status: StatusCompleted, Progress: 1.0},
			{ID: "b", Name: "beta", Status: StatusQueued, Progress: 0},
			{ID: "c", Name: "gamma", Status: StatusError, ErrorMessage: "fail"},
		},
		Order: []string{"a", "b", "c"},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got State
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(got.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got.Items))
	}
	if got.Items[0].ID != "a" || got.Items[1].ID != "b" || got.Items[2].ID != "c" {
		t.Error("item IDs mismatch after round trip")
	}
	if len(got.Order) != 3 {
		t.Fatalf("expected 3 order entries, got %d", len(got.Order))
	}
}

func TestStateEmptyItems(t *testing.T) {
	state := &State{Items: []*Item{}, Order: []string{}}
	data, _ := json.Marshal(state)
	var got State
	json.Unmarshal(data, &got)
	if len(got.Items) != 0 {
		t.Errorf("expected empty items, got %d", len(got.Items))
	}
}

func TestStateViewJSONFields(t *testing.T) {
	sv := &StateView{
		Items:    []*Item{{ID: "x", Status: StatusDownloading, Progress: 0.5}},
		Order:    []string{"x"},
		ActiveID: "x",
	}

	data, err := json.Marshal(sv)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]json.RawMessage
	json.Unmarshal(data, &m)

	if _, ok := m["items"]; !ok {
		t.Error("expected 'items' field in JSON")
	}
	if _, ok := m["order"]; !ok {
		t.Error("expected 'order' field in JSON")
	}
	if _, ok := m["active_id"]; !ok {
		t.Error("expected 'active_id' field in JSON")
	}

	var got StateView
	json.Unmarshal(data, &got)
	if got.ActiveID != "x" {
		t.Errorf("expected active_id=%q, got %q", "x", got.ActiveID)
	}
}

func TestStatusIsString(t *testing.T) {
	s := StatusQueued
	data, _ := json.Marshal(s)
	if string(data) != `"queued"` {
		t.Errorf("expected \"queued\", got %s", string(data))
	}
}
