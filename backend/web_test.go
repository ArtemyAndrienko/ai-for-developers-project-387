package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	srv := newMux(NewStore())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"status":"ok"}` {
		t.Errorf("health body: %s", body)
	}
}

func TestStaticIndexServed(t *testing.T) {
	srv := newMux(NewStore())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / : %d", rec.Code)
	}
	if !contains(rec.Body.String(), "<") && !contains(rec.Body.String(), "Календарь") {
		t.Errorf("index не похож на страницу: %.80s", rec.Body.String())
	}
}

func TestAPIRouteBeatsStatic(t *testing.T) {
	srv := newMux(NewStore())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/meetings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/meetings: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" || !contains(ct, "json") {
		t.Errorf("meeting list content-type: %q", ct)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
