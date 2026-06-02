package torrent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	atorrent "github.com/anacrolix/torrent"
)

const minimalTorrentData = "d8:announce35:http://tracker.example.com/announce4:infod6:lengthi1024e4:name8:test.txt12:piece lengthi16384e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei0eee"

// newBareManager builds a Manager without a real torrent client.
// Use only for tests that never touch m.client (State, Remove with nil
// torrent, Reorder, SetMaxUsagePercent, saveStateLocked, …).
func newBareManager(t *testing.T) *Manager {
	t.Helper()
	tmpDir := t.TempDir()
	torrentDir := filepath.Join(tmpDir, "torrents")
	os.MkdirAll(torrentDir, 0o755)
	return &Manager{
		items:           map[string]*Item{},
		torrents:        map[string]*atorrent.Torrent{},
		order:           []string{},
		statePath:       filepath.Join(tmpDir, "state.json"),
		storagePath:     tmpDir,
		maxUsagePercent: 0.90,
		torrentDir:      torrentDir,
	}
}

// newRealManager builds a Manager backed by a real anacrolix torrent client.
func newRealManager(t *testing.T) *Manager {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "stashflow-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	storagePath := filepath.Join(tmpDir, "storage")
	torrentDir := filepath.Join(tmpDir, "torrents")
	os.MkdirAll(storagePath, 0o755)
	os.MkdirAll(torrentDir, 0o755)
	statePath := filepath.Join(tmpDir, "state.json")

	m, err := NewManager(storagePath, torrentDir, statePath, 0.90)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() {
		m.Close()
		os.RemoveAll(tmpDir)
	})
	return m
}

// ---------------------------------------------------------------------------
// NewManager
// ---------------------------------------------------------------------------

func TestNewManager(t *testing.T) {
	m := newRealManager(t)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNewManagerInvalidDataDir(t *testing.T) {
	// A path that doesn't exist and can't be created should still succeed
	// for the client (it tries to create it), but let's verify no panic.
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "storage")
	torrentDir := filepath.Join(tmpDir, "torrents")
	os.MkdirAll(torrentDir, 0o755)
	statePath := filepath.Join(tmpDir, "state.json")

	m, err := NewManager(storagePath, torrentDir, statePath, 0.90)
	if err != nil {
		// Some systems may fail – that's fine, we just check no panic.
		return
	}
	t.Cleanup(func() { m.Close() })
}

func TestNewClientConfigEnablesPublicPeerDiscovery(t *testing.T) {
	storagePath := t.TempDir()
	cfg := newClientConfig(storagePath)

	if cfg.DataDir != storagePath {
		t.Errorf("expected DataDir %q, got %q", storagePath, cfg.DataDir)
	}
	if cfg.NoDHT {
		t.Error("expected DHT to be enabled for low-seeder peer discovery")
	}
	if cfg.DisablePEX {
		t.Error("expected PEX to be enabled for low-seeder peer discovery")
	}
	if cfg.DisableWebseeds {
		t.Error("expected webseeds to be enabled as a fallback source")
	}
	if !cfg.DisableWebtorrent {
		t.Error("expected WebTorrent support to remain disabled")
	}
	if !cfg.NoUpload {
		t.Error("expected upload suppression to remain enabled")
	}
	if !cfg.NoDefaultPortForwarding {
		t.Error("expected default port forwarding to remain disabled")
	}
	if cfg.ListenPort != 0 {
		t.Errorf("expected random listen port, got %d", cfg.ListenPort)
	}
	if cfg.EstablishedConnsPerTorrent < 40 {
		t.Errorf("expected at least 40 established connections per torrent, got %d", cfg.EstablishedConnsPerTorrent)
	}
	if cfg.HalfOpenConnsPerTorrent < 16 {
		t.Errorf("expected at least 16 half-open connections per torrent, got %d", cfg.HalfOpenConnsPerTorrent)
	}
	if cfg.TotalHalfOpenConns < 32 {
		t.Errorf("expected at least 32 total half-open connections, got %d", cfg.TotalHalfOpenConns)
	}
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

func TestManagerStateEmpty(t *testing.T) {
	m := newBareManager(t)
	sv := m.State()

	if sv == nil {
		t.Fatal("State() returned nil")
	}
	if len(sv.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(sv.Items))
	}
	if len(sv.Order) != 0 {
		t.Errorf("expected 0 order, got %d", len(sv.Order))
	}
	if sv.ActiveID != "" {
		t.Errorf("expected empty active ID, got %q", sv.ActiveID)
	}
}

func TestManagerStateWithItems(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a", Name: "file-a", Status: StatusCompleted}
	m.items["b"] = &Item{ID: "b", Name: "file-b", Status: StatusQueued}
	m.order = []string{"a", "b"}
	m.activeID = "b"

	sv := m.State()
	if len(sv.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(sv.Items))
	}
	if sv.ActiveID != "b" {
		t.Errorf("expected active ID %q, got %q", "b", sv.ActiveID)
	}
	// Verify order is preserved.
	if sv.Order[0] != "a" || sv.Order[1] != "b" {
		t.Errorf("order mismatch: %v", sv.Order)
	}
	// Returned order slice must be a copy.
	sv.Order[0] = "z"
	if m.order[0] == "z" {
		t.Error("State() should return a copy of order")
	}
}

// ---------------------------------------------------------------------------
// SetOnChange / onChange
// ---------------------------------------------------------------------------

func TestManagerSetOnChange(t *testing.T) {
	m := newBareManager(t)
	called := false
	m.SetOnChange(func() { called = true })
	if m.onChange == nil {
		t.Fatal("expected onChange to be set")
	}
	m.onChange()
	if !called {
		t.Fatal("onChange was not invoked")
	}
}

// ---------------------------------------------------------------------------
// SetMaxUsagePercent
// ---------------------------------------------------------------------------

func TestManagerSetMaxUsagePercent(t *testing.T) {
	m := newBareManager(t)
	m.SetMaxUsagePercent(0.50)
	if m.maxUsagePercent != 0.50 {
		t.Errorf("expected 0.50, got %f", m.maxUsagePercent)
	}
	m.SetMaxUsagePercent(1.0)
	if m.maxUsagePercent != 1.0 {
		t.Errorf("expected 1.0, got %f", m.maxUsagePercent)
	}
}

// ---------------------------------------------------------------------------
// Remove
// ---------------------------------------------------------------------------

func TestManagerRemoveNotFound(t *testing.T) {
	m := newBareManager(t)
	err := m.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing item")
	}
	if err.Error() != "not found" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestManagerRemoveExisting(t *testing.T) {
	m := newBareManager(t)
	m.items["x"] = &Item{ID: "x", Status: StatusCompleted}
	m.order = []string{"x"}

	err := m.Remove("x")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if _, ok := m.items["x"]; ok {
		t.Error("item should have been removed")
	}
	if len(m.order) != 0 {
		t.Errorf("order should be empty, got %v", m.order)
	}
}

func TestManagerRemoveActive(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a", Status: StatusDownloading}
	m.order = []string{"a"}
	m.activeID = "a"

	err := m.Remove("a")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if m.activeID != "" {
		t.Errorf("expected activeID to be cleared, got %q", m.activeID)
	}
}

func TestManagerRemoveCallsOnChange(t *testing.T) {
	m := newBareManager(t)
	m.items["c"] = &Item{ID: "c", Status: StatusCompleted}
	m.order = []string{"c"}

	called := false
	m.SetOnChange(func() { called = true })

	_ = m.Remove("c")
	if !called {
		t.Error("expected onChange to be called after Remove")
	}
}

func TestManagerRemovePreservesOtherItems(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a", Status: StatusCompleted}
	m.items["b"] = &Item{ID: "b", Status: StatusQueued}
	m.items["c"] = &Item{ID: "c", Status: StatusError}
	m.order = []string{"a", "b", "c"}

	_ = m.Remove("b")
	if len(m.order) != 2 {
		t.Fatalf("expected 2 items remaining, got %d", len(m.order))
	}
	if m.order[0] != "a" || m.order[1] != "c" {
		t.Errorf("order mismatch: %v", m.order)
	}
	if _, ok := m.items["a"]; !ok {
		t.Error("item a should still exist")
	}
	if _, ok := m.items["c"]; !ok {
		t.Error("item c should still exist")
	}
}

// ---------------------------------------------------------------------------
// Reorder
// ---------------------------------------------------------------------------

func TestManagerReorder(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a"}
	m.items["b"] = &Item{ID: "b"}
	m.items["c"] = &Item{ID: "c"}
	m.order = []string{"a", "b", "c"}

	m.Reorder([]string{"c", "b", "a"})
	if m.order[0] != "c" || m.order[1] != "b" || m.order[2] != "a" {
		t.Errorf("reorder failed: %v", m.order)
	}
}

func TestManagerReorderPartial(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a"}
	m.items["b"] = &Item{ID: "b"}
	m.items["c"] = &Item{ID: "c"}
	m.order = []string{"a", "b", "c"}

	// Only specify "c" — "a" and "b" should be appended.
	m.Reorder([]string{"c"})
	if len(m.order) != 3 {
		t.Fatalf("expected 3 items, got %d", len(m.order))
	}
	if m.order[0] != "c" {
		t.Errorf("expected c first, got %q", m.order[0])
	}
}

func TestManagerReorderMissingIDs(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a"}
	m.order = []string{"a"}

	// "z" doesn't exist — should be silently ignored.
	m.Reorder([]string{"z", "a"})
	if len(m.order) != 1 || m.order[0] != "a" {
		t.Errorf("expected [a], got %v", m.order)
	}
}

func TestManagerReorderCallsOnChange(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a"}
	m.order = []string{"a"}

	called := false
	m.SetOnChange(func() { called = true })
	m.Reorder([]string{"a"})
	if !called {
		t.Error("expected onChange after Reorder")
	}
}

// ---------------------------------------------------------------------------
// saveStateLocked / loadState
// ---------------------------------------------------------------------------

func TestManagerSaveAndLoadCompletedItems(t *testing.T) {
	m := newBareManager(t)
	now := time.Now().Truncate(time.Second)
	m.items["a"] = &Item{ID: "a", Name: "file-a", Status: StatusCompleted, Progress: 1.0, AddedAt: now}
	m.items["b"] = &Item{ID: "b", Name: "file-b", Status: StatusError, ErrorMessage: "bad hash", AddedAt: now}
	m.order = []string{"a", "b"}

	m.saveStateLocked()

	// Verify the file was written.
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		t.Fatalf("state file not written: %v", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("invalid state JSON: %v", err)
	}
	if len(state.Items) != 2 {
		t.Errorf("expected 2 items in state file, got %d", len(state.Items))
	}
	if len(state.Order) != 2 {
		t.Errorf("expected 2 items in order, got %d", len(state.Order))
	}
}

func TestManagerLoadStateMissing(t *testing.T) {
	m := newBareManager(t)
	// statePath doesn't exist yet — should be a no-op.
	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() with missing file should not error: %v", err)
	}
	if len(m.items) != 0 {
		t.Errorf("expected empty items, got %d", len(m.items))
	}
}

func TestManagerLoadStateCompleted(t *testing.T) {
	m := newBareManager(t)
	state := State{
		Items: []*Item{
			{ID: "a", Name: "done", Status: StatusCompleted, Progress: 1.0},
		},
		Order: []string{"a"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if len(m.items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(m.items))
	}
	if m.items["a"].Status != StatusCompleted {
		t.Errorf("expected status completed, got %q", m.items["a"].Status)
	}
}

func TestManagerLoadStateRecoversDownloading(t *testing.T) {
	// Items with status "downloading" should be reset to "queued".
	// We use a real manager because loadState tries to re-attach queued torrents
	// via the client.
	m := newRealManager(t)

	state := State{
		Items: []*Item{
			{ID: "d", Name: "was-downloading", Status: StatusDownloading, Progress: 0.5,
				Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
		},
		Order: []string{"d"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	// Reset internal state before loading.
	m.items = map[string]*Item{}
	m.order = nil

	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if m.items["d"].Status != StatusQueued {
		t.Errorf("expected downloading→queued, got %q", m.items["d"].Status)
	}
	if m.items["d"].Progress != 0 {
		t.Errorf("expected progress reset to 0, got %f", m.items["d"].Progress)
	}
}

func TestManagerLoadStateRecoversStorageError(t *testing.T) {
	m := newRealManager(t)

	state := State{
		Items: []*Item{
			{ID: "s", Name: "storage-err", Status: StatusError,
				ErrorMessage: "not enough storage available",
				Magnet:       "magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01"},
		},
		Order: []string{"s"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	m.items = map[string]*Item{}
	m.order = nil

	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if m.items["s"].Status != StatusQueued {
		t.Errorf("expected storage-error→queued, got %q", m.items["s"].Status)
	}
	if m.items["s"].ErrorMessage != "" {
		t.Errorf("expected error message cleared, got %q", m.items["s"].ErrorMessage)
	}
}

func TestManagerLoadStateKeepsPermanentError(t *testing.T) {
	m := newBareManager(t)

	state := State{
		Items: []*Item{
			{ID: "e", Name: "perm-err", Status: StatusError,
				ErrorMessage: "torrent size exceeds the max allowed storage"},
		},
		Order: []string{"e"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if m.items["e"].Status != StatusError {
		t.Errorf("expected permanent error to stay, got %q", m.items["e"].Status)
	}
}

func TestManagerLoadStateInvalidJSON(t *testing.T) {
	m := newBareManager(t)
	os.WriteFile(m.statePath, []byte("{bad json"), 0o644)

	err := m.loadState()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// CheckAndStartNext
// ---------------------------------------------------------------------------

func TestManagerCheckAndStartNextEmpty(t *testing.T) {
	m := newBareManager(t)
	// Should not panic with empty state.
	m.CheckAndStartNext()
}

func TestManagerCheckAndStartNextAlreadyActive(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a", Status: StatusDownloading}
	m.order = []string{"a"}
	m.activeID = "a"

	// Should return immediately because there's an active download.
	m.CheckAndStartNext()
	if m.activeID != "a" {
		t.Errorf("expected active ID to remain %q, got %q", "a", m.activeID)
	}
}

func TestManagerCheckAndStartNextNoQueuedItems(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a", Status: StatusCompleted}
	m.items["b"] = &Item{ID: "b", Status: StatusError, ErrorMessage: "bad"}
	m.order = []string{"a", "b"}

	m.CheckAndStartNext()
	if m.activeID != "" {
		t.Errorf("expected no active ID, got %q", m.activeID)
	}
}

func TestManagerCheckAndStartNextQueuedNoTorrent(t *testing.T) {
	m := newBareManager(t)
	m.items["q"] = &Item{ID: "q", Status: StatusQueued, SizeBytes: 1024}
	m.order = []string{"q"}
	// No torrent in m.torrents → should skip.

	m.CheckAndStartNext()
	if m.activeID != "" {
		t.Errorf("expected no active ID (no torrent), got %q", m.activeID)
	}
}

func TestManagerCheckAndStartNextQueuedZeroSize(t *testing.T) {
	m := newBareManager(t)
	m.items["q"] = &Item{ID: "q", Status: StatusQueued, SizeBytes: 0}
	m.order = []string{"q"}

	m.CheckAndStartNext()
	if m.activeID != "" {
		t.Errorf("expected no active ID (zero size), got %q", m.activeID)
	}
}

func TestManagerCheckAndStartNextStaleActive(t *testing.T) {
	m := newBareManager(t)
	// activeID points to an item that is no longer downloading.
	m.items["a"] = &Item{ID: "a", Status: StatusCompleted}
	m.order = []string{"a"}
	m.activeID = "a"

	m.CheckAndStartNext()
	if m.activeID != "" {
		t.Errorf("expected stale activeID cleared, got %q", m.activeID)
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestManagerClose(t *testing.T) {
	m := newRealManager(t)
	err := m.Close()
	if err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AddMagnet / AddTorrentFile (via real client)
// ---------------------------------------------------------------------------

func TestManagerAddMagnet(t *testing.T) {
	m := newRealManager(t)

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test"
	item, err := m.AddMagnet(magnet)
	if err != nil {
		t.Fatalf("AddMagnet() error: %v", err)
	}
	if item == nil {
		t.Fatal("expected non-nil item")
	}
	if item.Status != StatusQueued {
		t.Errorf("expected status queued, got %q", item.Status)
	}
	if item.Magnet != magnet {
		t.Errorf("expected magnet to match")
	}
	if item.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("expected parsed info hash, got %q", item.InfoHash)
	}
	if item.ID == "" {
		t.Error("expected non-empty ID")
	}

	// Item should appear in State.
	sv := m.State()
	if len(sv.Items) != 1 {
		t.Errorf("expected 1 item in state, got %d", len(sv.Items))
	}
	if m.torrents[item.ID] != nil {
		t.Fatal("queued magnet should not be attached immediately")
	}
}

func TestManagerAddMagnetCallsOnChange(t *testing.T) {
	m := newRealManager(t)
	var called atomic.Bool
	m.SetOnChange(func() { called.Store(true) })

	_, _ = m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test")
	if !called.Load() {
		t.Error("expected onChange to be called after AddMagnet")
	}
}

func TestManagerAddMagnetRejectsDuplicateInfoHash(t *testing.T) {
	m := newRealManager(t)

	_, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=first")
	if err != nil {
		t.Fatalf("first AddMagnet() error: %v", err)
	}

	_, err = m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=second&tr=http://tracker.example.com/announce")
	if err == nil {
		t.Fatal("expected duplicate magnet to be rejected")
	}
	if !strings.Contains(err.Error(), errDuplicateTorrent) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestManagerAddMagnetRejectsDuplicatePausedItem(t *testing.T) {
	m := newRealManager(t)

	item, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=first")
	if err != nil {
		t.Fatalf("first AddMagnet() error: %v", err)
	}
	m.mu.Lock()
	m.items[item.ID].Status = StatusPaused
	m.mu.Unlock()

	_, err = m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=second")
	if err == nil {
		t.Fatal("expected duplicate paused magnet to be rejected")
	}
	if !strings.Contains(err.Error(), errDuplicateTorrent) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestManagerAddMagnetAllowsDuplicateAfterCompletion(t *testing.T) {
	m := newRealManager(t)

	item, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=first")
	if err != nil {
		t.Fatalf("first AddMagnet() error: %v", err)
	}
	m.mu.Lock()
	m.items[item.ID].Status = StatusCompleted
	m.mu.Unlock()

	dup, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=second")
	if err != nil {
		t.Fatalf("expected completed duplicate to be allowed, got %v", err)
	}
	if dup == nil {
		t.Fatal("expected duplicate add result")
	}
}

func TestManagerAddMagnetAllowsDuplicateAfterError(t *testing.T) {
	m := newRealManager(t)

	item, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=first")
	if err != nil {
		t.Fatalf("first AddMagnet() error: %v", err)
	}
	m.mu.Lock()
	m.items[item.ID].Status = StatusError
	m.items[item.ID].ErrorMessage = "failed earlier"
	m.mu.Unlock()

	dup, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=second")
	if err != nil {
		t.Fatalf("expected errored duplicate to be allowed, got %v", err)
	}
	if dup == nil {
		t.Fatal("expected duplicate add result")
	}
}

func TestManagerAddTorrentFileInvalid(t *testing.T) {
	m := newRealManager(t)

	// Invalid torrent data should fail.
	_, err := m.AddTorrentFile("bad.torrent", []byte("not a torrent"))
	if err == nil {
		t.Fatal("expected error for invalid torrent file data")
	}
}

// ---------------------------------------------------------------------------
// StartBackground (smoke test)
// ---------------------------------------------------------------------------

func TestManagerStartBackground(t *testing.T) {
	m := newRealManager(t)
	// Should not panic; goroutines will be cleaned up when manager is closed.
	m.StartBackground()
	// Give the goroutines a moment to start.
	time.Sleep(50 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// AddTorrentFile success with minimal valid torrent
// ---------------------------------------------------------------------------

func TestManagerAddTorrentFileValid(t *testing.T) {
	m := newRealManager(t)

	torrentData := []byte(minimalTorrentData)

	item, err := m.AddTorrentFile("test.torrent", torrentData)
	if err != nil {
		t.Fatalf("AddTorrentFile() error: %v", err)
	}
	if item == nil {
		t.Fatal("expected non-nil item")
	}
	if item.Status != StatusQueued {
		t.Errorf("expected status queued, got %q", item.Status)
	}
	if item.TorrentFile == "" {
		t.Error("expected TorrentFile to be set")
	}
	if item.ID == "" {
		t.Error("expected non-empty ID")
	}
	if item.Name != "test.txt" {
		t.Errorf("expected parsed torrent name, got %q", item.Name)
	}
	if item.InfoHash == "" {
		t.Error("expected parsed torrent info hash")
	}
	if item.SizeBytes != 1024 {
		t.Errorf("expected parsed torrent size, got %d", item.SizeBytes)
	}
	if m.torrents[item.ID] != nil {
		t.Fatal("queued torrent file should not be attached immediately")
	}

	sv := m.State()
	if len(sv.Items) != 1 {
		t.Errorf("expected 1 item in state, got %d", len(sv.Items))
	}
}

func TestManagerAddTorrentFileCallsOnChange(t *testing.T) {
	m := newRealManager(t)
	var called atomic.Bool
	m.SetOnChange(func() { called.Store(true) })

	torrentData := []byte(minimalTorrentData)
	_, _ = m.AddTorrentFile("test.torrent", torrentData)
	if !called.Load() {
		t.Error("expected onChange to be called after AddTorrentFile")
	}
}

func TestManagerAddTorrentFileRejectsDuplicateInfoHash(t *testing.T) {
	m := newRealManager(t)

	torrentData := []byte(minimalTorrentData)
	if _, err := m.AddTorrentFile("first.torrent", torrentData); err != nil {
		t.Fatalf("first AddTorrentFile() error: %v", err)
	}

	_, err := m.AddTorrentFile("second.torrent", torrentData)
	if err == nil {
		t.Fatal("expected duplicate torrent file to be rejected")
	}
	if !strings.Contains(err.Error(), errDuplicateTorrent) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestManagerAddTorrentFileAllowsDuplicateAfterRemove(t *testing.T) {
	m := newRealManager(t)

	torrentData := []byte(minimalTorrentData)
	item, err := m.AddTorrentFile("first.torrent", torrentData)
	if err != nil {
		t.Fatalf("first AddTorrentFile() error: %v", err)
	}
	if err := m.Remove(item.ID); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	dup, err := m.AddTorrentFile("second.torrent", torrentData)
	if err != nil {
		t.Fatalf("expected re-add after remove to succeed, got %v", err)
	}
	if dup == nil {
		t.Fatal("expected duplicate add result")
	}
}

func TestManagerRejectsDuplicateAcrossMagnetAndTorrentFile(t *testing.T) {
	m := newRealManager(t)

	torrentData := []byte(minimalTorrentData)
	item, err := m.AddTorrentFile("first.torrent", torrentData)
	if err != nil {
		t.Fatalf("AddTorrentFile() error: %v", err)
	}

	_, err = m.AddMagnet("magnet:?xt=urn:btih:" + item.InfoHash + "&dn=same-content")
	if err == nil {
		t.Fatal("expected duplicate magnet to be rejected when torrent file is already queued")
	}
	if !strings.Contains(err.Error(), errDuplicateTorrent) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AddMagnet → Remove round-trip
// ---------------------------------------------------------------------------

func TestManagerAddMagnetThenRemove(t *testing.T) {
	m := newRealManager(t)

	item, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=round-trip")
	if err != nil {
		t.Fatalf("AddMagnet() error: %v", err)
	}

	// Remove the item.
	if err := m.Remove(item.ID); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	sv := m.State()
	if len(sv.Items) != 0 {
		t.Errorf("expected 0 items after remove, got %d", len(sv.Items))
	}
}

// ---------------------------------------------------------------------------
// CheckAndStartNext — queued item that doesn't fit storage
// ---------------------------------------------------------------------------

func TestManagerCheckAndStartNextCannotFit(t *testing.T) {
	m := newBareManager(t)
	// Set a very small maxUsagePercent so AvailableForNew is near zero.
	m.maxUsagePercent = 0.0001
	m.items["q"] = &Item{ID: "q", Status: StatusQueued, SizeBytes: 999999999999}
	m.order = []string{"q"}

	m.CheckAndStartNext()
	// Should not start because it doesn't fit.
	if m.activeID != "" {
		t.Errorf("expected no active ID (doesn't fit), got %q", m.activeID)
	}
	// Status should remain queued.
	if m.items["q"].Status != StatusQueued {
		t.Errorf("expected status to remain queued, got %q", m.items["q"].Status)
	}
}

// ---------------------------------------------------------------------------
// loadState — torrent file reattach
// ---------------------------------------------------------------------------

func TestManagerLoadStateReattachTorrentFile(t *testing.T) {
	m := newRealManager(t)

	// Create a valid torrent file on disk.
	torrentData := []byte(minimalTorrentData)
	torrentPath := filepath.Join(m.torrentDir, "test-id.torrent")
	os.WriteFile(torrentPath, torrentData, 0o644)

	state := State{
		Items: []*Item{
			{ID: "tf", Name: "test.txt", Status: StatusQueued, TorrentFile: torrentPath},
		},
		Order: []string{"tf"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	m.items = map[string]*Item{}
	m.order = nil

	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if _, ok := m.items["tf"]; !ok {
		t.Fatal("expected item 'tf' to be loaded")
	}
	if m.items["tf"].Status != StatusQueued {
		t.Errorf("expected queued status, got %q", m.items["tf"].Status)
	}
	if m.items["tf"].InfoHash == "" {
		t.Fatal("expected loadState to backfill torrent file info hash")
	}
	if m.torrents["tf"] != nil {
		t.Fatal("queued torrent file should remain detached after restart")
	}
}

// ---------------------------------------------------------------------------
// loadState — torrent file missing on disk
// ---------------------------------------------------------------------------

func TestManagerLoadStateTorrentFileMissing(t *testing.T) {
	m := newRealManager(t)

	state := State{
		Items: []*Item{
			{ID: "miss", Name: "missing", Status: StatusQueued, TorrentFile: "/nonexistent/missing.torrent"},
		},
		Order: []string{"miss"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	m.items = map[string]*Item{}
	m.order = nil

	err := m.loadState()
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	// Item should still be loaded in items map.
	if _, ok := m.items["miss"]; !ok {
		t.Fatal("expected item to be loaded even if torrent file is missing")
	}
	// But no torrent should be attached.
	if m.torrents["miss"] != nil {
		t.Error("expected no torrent attached for missing file")
	}
}

// ---------------------------------------------------------------------------
// saveStateLocked preserves all items
// ---------------------------------------------------------------------------

func TestManagerSaveStateMultipleItems(t *testing.T) {
	m := newBareManager(t)
	m.items["a"] = &Item{ID: "a", Status: StatusQueued, SizeBytes: 100}
	m.items["b"] = &Item{ID: "b", Status: StatusDownloading, SizeBytes: 200, Progress: 0.5}
	m.items["c"] = &Item{ID: "c", Status: StatusCompleted, SizeBytes: 300, Progress: 1.0}
	m.items["d"] = &Item{ID: "d", Status: StatusError, ErrorMessage: "fail"}
	m.order = []string{"a", "b", "c", "d"}

	m.saveStateLocked()

	data, err := os.ReadFile(m.statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state State
	json.Unmarshal(data, &state)

	if len(state.Items) != 4 {
		t.Errorf("expected 4 items, got %d", len(state.Items))
	}
	if len(state.Order) != 4 {
		t.Errorf("expected 4 order entries, got %d", len(state.Order))
	}
}

// ---------------------------------------------------------------------------
// progressLoop coverage — run for >1 tick
// ---------------------------------------------------------------------------

func TestManagerProgressLoopRuns(t *testing.T) {
	m := newRealManager(t)
	// Add an item in downloading status with nil torrent — the loop
	// should skip it without panic and still call saveStateLocked.
	m.items["dl"] = &Item{ID: "dl", Status: StatusDownloading}
	m.order = []string{"dl"}

	changed := make(chan struct{}, 5)
	m.SetOnChange(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	m.StartBackground()

	// Wait for at least one tick (1 second).
	select {
	case <-changed:
		// Good — the loop ran and called onChange.
	case <-time.After(3 * time.Second):
		t.Fatal("progressLoop did not fire within 3 seconds")
	}

	// State should have been saved.
	if _, err := os.Stat(m.statePath); os.IsNotExist(err) {
		t.Error("expected state file to be written by progressLoop")
	}
}

func TestManagerCheckAndStartNextAttachesQueuedMagnet(t *testing.T) {
	m := newRealManager(t)

	item, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=queued-start")
	if err != nil {
		t.Fatalf("AddMagnet() error: %v", err)
	}

	m.CheckAndStartNext()

	if m.torrents[item.ID] == nil {
		t.Fatal("front queued magnet should attach when queue processing starts")
	}
}

func TestManagerPauseAndResume(t *testing.T) {
	m := newRealManager(t)

	item, err := m.AddMagnet("magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01&dn=pause-test")
	if err != nil {
		t.Fatalf("AddMagnet() error: %v", err)
	}

	tor, err := m.client.AddMagnet(item.Magnet)
	if err != nil {
		t.Fatalf("client.AddMagnet() error: %v", err)
	}

	m.mu.Lock()
	m.items[item.ID].Status = StatusDownloading
	m.torrents[item.ID] = tor
	m.activeID = item.ID
	m.mu.Unlock()

	if err := m.Pause(item.ID); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}
	if m.items[item.ID].Status != StatusPaused {
		t.Fatalf("expected paused status, got %q", m.items[item.ID].Status)
	}
	if m.activeID != "" {
		t.Fatalf("expected activeID cleared, got %q", m.activeID)
	}
	if m.torrents[item.ID] != nil {
		t.Fatal("paused torrent should be detached")
	}

	if err := m.Resume(item.ID); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if m.items[item.ID].Status != StatusQueued {
		t.Fatalf("expected resumed item to return to queued, got %q", m.items[item.ID].Status)
	}
}

func TestManagerLoadStateKeepsPausedDetached(t *testing.T) {
	m := newRealManager(t)

	state := State{
		Items: []*Item{
			{ID: "p", Name: "paused", Status: StatusPaused, Magnet: "magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01"},
		},
		Order: []string{"p"},
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(m.statePath, data, 0o644)

	m.items = map[string]*Item{}
	m.order = nil

	if err := m.loadState(); err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if m.items["p"].Status != StatusPaused {
		t.Fatalf("expected paused status after restart, got %q", m.items["p"].Status)
	}
	if m.items["p"].InfoHash != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("expected paused magnet info hash to be backfilled, got %q", m.items["p"].InfoHash)
	}
	if m.torrents["p"] != nil {
		t.Fatal("paused torrent should not be reattached on restart")
	}
}
