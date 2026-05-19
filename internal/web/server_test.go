package web

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stashflow/internal/config"
	"stashflow/internal/torrent"
)

// setupTestServer builds a Server backed by a real torrent Manager in temp dirs.
func setupTestServer(t *testing.T) *Server {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "stashflow-web-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	storagePath := filepath.Join(tmpDir, "storage")
	torrentDir := filepath.Join(tmpDir, "torrents")
	os.MkdirAll(storagePath, 0o755)
	os.MkdirAll(torrentDir, 0o755)
	statePath := filepath.Join(tmpDir, "state.json")
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := &config.Config{
		StoragePath:     storagePath,
		Port:            0,
		MaxUsagePercent: 0.90,
	}

	mgr, err := torrent.NewManager(storagePath, torrentDir, statePath, 0.90)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() {
		mgr.Close()
		os.RemoveAll(tmpDir)
	})

	hub := NewHub()
	return NewServer(cfg, cfgPath, mgr, hub)
}

// ---------------------------------------------------------------------------
// NewServer / Router
// ---------------------------------------------------------------------------

func TestNewServer(t *testing.T) {
	srv := setupTestServer(t)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.Router() == nil {
		t.Fatal("expected non-nil router")
	}
}

// ---------------------------------------------------------------------------
// GET /  (index)
// ---------------------------------------------------------------------------

func TestHandleIndex(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content-type, got %q", ct)
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty body")
	}
}

// ---------------------------------------------------------------------------
// GET /assets/*filepath
// ---------------------------------------------------------------------------

func TestHandleAssetsCSS(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/assets/assets.css", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("expected text/css, got %q", ct)
	}
}

func TestHandleAssetsJS(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/assets/assets.js", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript content-type, got %q", ct)
	}
}

func TestHandleAssetsPNG(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/assets/images/logo.png", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "image/png") {
		t.Errorf("expected image/png, got %q", ct)
	}
}

func TestHandleAssetsNotFound(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/assets/nonexistent.xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleAssetsEmptyPath(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/assets/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/state
// ---------------------------------------------------------------------------

func TestHandleState(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["state"]; !ok {
		t.Error("missing 'state' key")
	}
	if _, ok := body["storage"]; !ok {
		t.Error("missing 'storage' key")
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings
// ---------------------------------------------------------------------------

func TestHandleGetSettings(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg config.Config
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if cfg.MaxUsagePercent != 0.90 {
		t.Errorf("expected max_usage_percent=0.90, got %f", cfg.MaxUsagePercent)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/settings
// ---------------------------------------------------------------------------

func TestHandleUpdateSettings(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	payload := `{"max_usage_percent": 0.80}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["restartRequired"] == true {
		t.Error("changing only max_usage_percent should not require restart")
	}
}

func TestHandleUpdateSettingsPortChange(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	payload := `{"port": 9999}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["restartRequired"] != true {
		t.Error("changing port should require restart")
	}
}

func TestHandleUpdateSettingsStoragePathChange(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	payload := `{"storage_path": "/tmp/new-path"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["restartRequired"] != true {
		t.Error("changing storage_path should require restart")
	}
}

func TestHandleUpdateSettingsInvalidPayload(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleUpdateSettingsMaxUsageOutOfRange(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	tests := []struct {
		name    string
		payload string
	}{
		{"too low", `{"max_usage_percent": 0.001}`},
		{"too high", `{"max_usage_percent": 1.5}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// POST /api/torrents
// ---------------------------------------------------------------------------

func TestHandleAddTorrentMissingData(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodPost, "/api/torrents", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleAddTorrentMagnet(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	body := strings.NewReader("magnet=magnet%3A%3Fxt%3Durn%3Abtih%3A0123456789abcdef0123456789abcdef01234567%26dn%3Dtest")
	req := httptest.NewRequest(http.MethodPost, "/api/torrents", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var item map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if item["id"] == nil || item["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/torrents/:id
// ---------------------------------------------------------------------------

func TestHandleRemoveTorrentNotFound(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodDelete, "/api/torrents/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/torrents/:id  (success path — add then remove)
// ---------------------------------------------------------------------------

func TestHandleRemoveTorrentSuccess(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	// First add a magnet.
	addBody := strings.NewReader("magnet=magnet%3A%3Fxt%3Durn%3Abtih%3Aabcdefabcdefabcdefabcdefabcdefabcdefabcd%26dn%3Dremove-test")
	addReq := httptest.NewRequest(http.MethodPost, "/api/torrents", addBody)
	addReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addW := httptest.NewRecorder()
	r.ServeHTTP(addW, addReq)

	if addW.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d", addW.Code)
	}
	var item map[string]any
	json.Unmarshal(addW.Body.Bytes(), &item)
	id := item["id"].(string)

	// Now remove it.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/torrents/"+id, nil)
	delW := httptest.NewRecorder()
	r.ServeHTTP(delW, delReq)

	if delW.Code != http.StatusNoContent {
		t.Errorf("remove: expected 204, got %d; body: %s", delW.Code, delW.Body.String())
	}
}

// ---------------------------------------------------------------------------
// POST /api/queue/reorder
// ---------------------------------------------------------------------------

func TestHandleReorder(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	payload := `{"order":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/queue/reorder", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleReorderInvalidPayload(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodPost, "/api/queue/reorder", strings.NewReader("bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/storage/check
// ---------------------------------------------------------------------------

func TestHandleStorageCheck(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodPost, "/api/storage/check", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var stats map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if stats["total_bytes"] == nil {
		t.Error("expected total_bytes in response")
	}
}

// ---------------------------------------------------------------------------
// GET /api/events  (SSE)
// ---------------------------------------------------------------------------

func TestHandleEvents(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	// Use a cancellable context so we can stop the SSE handler.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Give the handler time to write the initial SSE message.
	time.Sleep(200 * time.Millisecond)

	// Cancel the request context to stop the SSE handler.
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE handler did not terminate after context cancel")
	}

	body := w.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Errorf("expected SSE data prefix, got: %q", body)
	}
	// The initial message should contain state and storage.
	if !strings.Contains(body, "state") {
		t.Errorf("expected 'state' in initial SSE message, got: %q", body)
	}
}

func TestHandleEventsBroadcast(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for subscription to be established.
	time.Sleep(200 * time.Millisecond)

	// Broadcast a message — it should appear in the SSE stream.
	srv.hub.Broadcast(map[string]any{"test": "broadcast"})
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "broadcast") {
		t.Errorf("expected broadcast content in SSE stream, got: %q", body)
	}
}

// ---------------------------------------------------------------------------
// POST /api/torrents  (file upload)
// ---------------------------------------------------------------------------

func TestHandleAddTorrentFileUpload(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	// Create a multipart form with an invalid torrent file.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.torrent")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	part.Write([]byte("not a valid torrent file"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/torrents", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail because the torrent data is invalid.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid torrent file, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// PUT /api/settings  (same value, no restart)
// ---------------------------------------------------------------------------

func TestHandleUpdateSettingsSameValues(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	// First get current settings.
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var cfg config.Config
	json.Unmarshal(w.Body.Bytes(), &cfg)

	// Send the same max_usage_percent back — should not require restart.
	payload, _ := json.Marshal(map[string]any{"max_usage_percent": cfg.MaxUsagePercent})
	req = httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["restartRequired"] == true {
		t.Error("same values should not require restart")
	}
}

// ---------------------------------------------------------------------------
// POST /api/torrents  (valid torrent file upload)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Method-not-allowed edge cases
// ---------------------------------------------------------------------------

func TestHandleStatePOSTNotAllowed(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodPost, "/api/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("POST /api/state should not return 200")
	}
}

func TestHandleAddTorrentEmptyMagnet(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	body := strings.NewReader("magnet=")
	req := httptest.NewRequest(http.MethodPost, "/api/torrents", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty magnet, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleReorderWithMultipleTorrents(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	// Add two torrents.
	add := func(hash, name string) string {
		magnet := "magnet%3A%3Fxt%3Durn%3Abtih%3A" + hash + "%26dn%3D" + name
		addBody := strings.NewReader("magnet=" + magnet)
		addReq := httptest.NewRequest(http.MethodPost, "/api/torrents", addBody)
		addReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		addW := httptest.NewRecorder()
		r.ServeHTTP(addW, addReq)
		if addW.Code != http.StatusOK {
			t.Fatalf("add torrent: expected 200, got %d", addW.Code)
		}
		var item map[string]any
		json.Unmarshal(addW.Body.Bytes(), &item)
		return item["id"].(string)
	}

	id1 := add("0123456789abcdef0123456789abcdef01234560", "torrent-one")
	id2 := add("0123456789abcdef0123456789abcdef01234561", "torrent-two")

	// Reorder: put second before first.
	payload, _ := json.Marshal(map[string]any{"order": []string{id2, id1}})
	req := httptest.NewRequest(http.MethodPost, "/api/queue/reorder", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify state reflects new order.
	req2 := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var body map[string]json.RawMessage
	json.Unmarshal(w2.Body.Bytes(), &body)
	var state map[string]json.RawMessage
	json.Unmarshal(body["state"], &state)
	var order []string
	json.Unmarshal(state["order"], &order)

	if len(order) < 2 {
		t.Fatalf("expected at least 2 items in order, got %d", len(order))
	}
	if order[0] != id2 || order[1] != id1 {
		t.Errorf("expected order [%s, %s], got %v", id2, id1, order)
	}
}

func TestHandleStorageCheckResponseFields(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodPost, "/api/storage/check", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var stats map[string]any
	json.Unmarshal(w.Body.Bytes(), &stats)

	for _, field := range []string{"total_bytes", "used_bytes", "free_bytes", "max_usage_bytes", "available_for_new"} {
		if stats[field] == nil {
			t.Errorf("missing field %q in storage response", field)
		}
	}
}

func TestHandleRemoveTorrentMissingID(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodDelete, "/api/torrents/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Either 404 or 405, definitely not 200/204.
	if w.Code == http.StatusNoContent || w.Code == http.StatusOK {
		t.Errorf("expected error status for empty ID, got %d", w.Code)
	}
}

func TestHandleAddMultipleMagnets(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	hashes := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccc",
	}

	ids := make([]string, 0, len(hashes))
	for _, h := range hashes {
		magnet := "magnet%3A%3Fxt%3Durn%3Abtih%3A" + h
		addBody := strings.NewReader("magnet=" + magnet)
		req := httptest.NewRequest(http.MethodPost, "/api/torrents", addBody)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("add: expected 200, got %d", w.Code)
		}
		var item map[string]any
		json.Unmarshal(w.Body.Bytes(), &item)
		ids = append(ids, item["id"].(string))
	}

	// State should show 3 items.
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]json.RawMessage
	json.Unmarshal(w.Body.Bytes(), &body)
	var state map[string]json.RawMessage
	json.Unmarshal(body["state"], &state)
	var items []json.RawMessage
	json.Unmarshal(state["items"], &items)

	if len(items) != 3 {
		t.Errorf("expected 3 items in state, got %d", len(items))
	}

	// Remove all.
	for _, id := range ids {
		req := httptest.NewRequest(http.MethodDelete, "/api/torrents/"+id, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Errorf("remove %s: expected 204, got %d", id, w.Code)
		}
	}

	// State should show 0 items.
	req = httptest.NewRequest(http.MethodGet, "/api/state", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	json.Unmarshal(w.Body.Bytes(), &body)
	json.Unmarshal(body["state"], &state)
	json.Unmarshal(state["items"], &items)

	if len(items) != 0 {
		t.Errorf("expected 0 items after removal, got %d", len(items))
	}
}

func TestHandleUpdateSettingsPortZero(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	payload := `{"port": 0}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Port 0 is same as current (default 0), no restart needed.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetSettingsResponseFields(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, field := range []string{"storage_path", "port", "max_usage_percent"} {
		if _, ok := m[field]; !ok {
			t.Errorf("missing field %q in settings response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// POST /api/torrents  (valid torrent file upload)
// ---------------------------------------------------------------------------

func TestHandleAddTorrentFileUploadValid(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	torrentData := []byte("d8:announce35:http://tracker.example.com/announce4:infod6:lengthi1024e4:name8:test.txt12:piece lengthi16384e6:pieces20:xxxxxxxxxxxxxxxxxxxx7:privatei0eee")

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "valid.torrent")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	part.Write(torrentData)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/torrents", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid torrent file, got %d; body: %s", w.Code, w.Body.String())
	}

	var item map[string]any
	json.Unmarshal(w.Body.Bytes(), &item)
	if item["id"] == nil || item["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

func TestHandlePauseAndResumeTorrent(t *testing.T) {
	srv := setupTestServer(t)
	r := srv.Router()

	addBody := strings.NewReader("magnet=magnet%3A%3Fxt%3Durn%3Abtih%3Aabcdefabcdefabcdefabcdefabcdefabcdefabcd%26dn%3Dpause-me")
	addReq := httptest.NewRequest(http.MethodPost, "/api/torrents", addBody)
	addReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addW := httptest.NewRecorder()
	r.ServeHTTP(addW, addReq)

	if addW.Code != http.StatusOK {
		t.Fatalf("add: expected 200, got %d", addW.Code)
	}
	var item map[string]any
	json.Unmarshal(addW.Body.Bytes(), &item)
	id := item["id"].(string)

	pauseReq := httptest.NewRequest(http.MethodPost, "/api/torrents/"+id+"/pause", nil)
	pauseW := httptest.NewRecorder()
	r.ServeHTTP(pauseW, pauseReq)
	if pauseW.Code != http.StatusNoContent {
		t.Fatalf("pause: expected 204, got %d; body: %s", pauseW.Code, pauseW.Body.String())
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	stateW := httptest.NewRecorder()
	r.ServeHTTP(stateW, stateReq)
	var body map[string]json.RawMessage
	json.Unmarshal(stateW.Body.Bytes(), &body)
	var state struct {
		Items []torrent.Item `json:"items"`
	}
	json.Unmarshal(body["state"], &state)
	if len(state.Items) != 1 || state.Items[0].Status != torrent.StatusPaused {
		t.Fatalf("expected paused item in state, got %+v", state.Items)
	}

	resumeReq := httptest.NewRequest(http.MethodPost, "/api/torrents/"+id+"/resume", nil)
	resumeW := httptest.NewRecorder()
	r.ServeHTTP(resumeW, resumeReq)
	if resumeW.Code != http.StatusNoContent {
		t.Fatalf("resume: expected 204, got %d; body: %s", resumeW.Code, resumeW.Body.String())
	}
}
