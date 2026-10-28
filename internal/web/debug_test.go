package web

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDebugRoutesOptInAndLoopbackOnly(t *testing.T) {
	s := setupTestServer(t)
	for _, enabled := range []string{"", "1"} {
		t.Setenv("STASHFLOW_DEBUG", enabled)
		r := s.Router()
		for _, path := range []string{"/debug/diagnostics", "/debug/pprof/heap", "/debug/pprof/goroutine"} {
			for _, remote := range []string{"127.0.0.1:1234", "[::1]:1234", "192.0.2.1:1234"} {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.RemoteAddr = remote
				req.Header.Set("X-Forwarded-For", "127.0.0.1")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				want := http.StatusOK
				if enabled == "" {
					want = http.StatusNotFound
				} else if remote == "192.0.2.1:1234" {
					want = http.StatusForbidden
				}
				if w.Code != want {
					t.Fatalf("%s enabled=%q remote=%s: %d, want %d", path, enabled, remote, w.Code, want)
				}
				if want != http.StatusOK {
					continue
				}
				if path == "/debug/diagnostics" {
					var body map[string]json.RawMessage
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if body["resources"] == nil || body["storage"] == nil {
						t.Fatal("missing diagnostic data")
					}
				} else {
					reader, err := gzip.NewReader(w.Body)
					if err != nil {
						t.Fatalf("invalid profile: %v", err)
					}
					_ = reader.Close()
				}
			}
		}
	}
}
