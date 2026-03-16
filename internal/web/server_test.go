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
