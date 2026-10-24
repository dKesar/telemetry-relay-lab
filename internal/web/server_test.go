package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dKesar/telemetry-relay-lab/internal/sim"
)

func TestEmbeddedEvidenceAndDataArchive(t *testing.T) {
	h := Handler()
	w := request(t, h, "GET", "/research.html", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), sim.Version) || !strings.Contains(w.Header().Get("Content-Security-Policy"), "style-src 'unsafe-inline'") {
		t.Fatal("missing or blocked evidence page")
	}
	w = request(t, h, "GET", "/evidence-data.zip", "", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal("missing data archive")
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 4 {
		t.Fatal("incomplete research archive")
	}
	f, err := z.Open("summary.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var report struct {
		Model  string `json:"model"`
		Checks int    `json:"validated_runs"`
	}
	if err := json.NewDecoder(f).Decode(&report); err != nil || report.Model != sim.Version || report.Checks < 1 {
		t.Fatal("stale evidence archive", err)
	}
}

func request(t *testing.T, h http.Handler, method, path, body, origin string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAssetsAndPresets(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/app.js", "/style.css", "/healthz", "/api/presets"} {
		w := request(t, h, "GET", path, "", "")
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
	if w := request(t, h, "GET", "/missing", "", ""); w.Code != 404 {
		t.Fatal("unknown asset exposed")
	}
	r := httptest.NewRequest("GET", "http://attacker.example/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("rebind Host accepted")
	}
}

func TestAPIProducesGoReport(t *testing.T) {
	h := Handler()
	c, _ := sim.Preset("shared_failure")
	body, _ := json.Marshal(map[string]any{"config": c})
	w := request(t, h, "POST", "/api/run", string(body), "http://127.0.0.1:8080")
	if w.Code != 200 {
		t.Fatalf("API: %d %s", w.Code, w.Body.String())
	}
	var report sim.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != len(sim.Policies()) || report.Version != sim.Version {
		t.Fatal("incorrect report")
	}
	direct, err := sim.RunAll(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(report)
	b, _ := json.Marshal(direct)
	if !bytes.Equal(a, b) {
		t.Fatal("HTTP result differs from CLI engine")
	}
}

func TestAPIRejectsBadRequests(t *testing.T) {
	h := Handler()
	for _, tc := range []struct {
		method, body, origin string
		code                 int
	}{
		{"GET", "", "", 405},
		{"POST", "{}", "http://attacker.example", 403},
		{"POST", "{}", "", 400},
		{"POST", "{bad", "", 400},
		{"POST", "{\"unknown\":1}", "", 400},
		{"POST", strings.Repeat("x", 70<<10), "", 400},
	} {
		w := request(t, h, tc.method, "/api/run", tc.body, tc.origin)
		if w.Code != tc.code {
			t.Fatalf("got %d want %d", w.Code, tc.code)
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/run", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("accepted missing JSON media type")
	}
}

func TestServeRejectsPublicBindingAndStops(t *testing.T) {
	if err := Serve(context.Background(), "0.0.0.0:0", io.Discard); err == nil {
		t.Fatal("public interface accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "127.0.0.1:0", io.Discard) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}
