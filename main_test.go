package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewNickMatchesPattern(t *testing.T) {
	for range 100 {
		if n := newNick(); !nickRE.MatchString(n) {
			t.Fatalf("nick %q does not match %s", n, nickRE)
		}
	}
}

func TestVersionIsTrimmed(t *testing.T) {
	if version == "" || strings.TrimSpace(version) != version {
		t.Fatalf("version %q should be non-empty with no surrounding whitespace", version)
	}
}

func TestRoutes(t *testing.T) {
	srv := httptest.NewServer(newMux(newHub()))
	defer srv.Close()

	get := func(path string) *http.Response {
		t.Helper()
		r, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	r := get("/")
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "v"+version) {
		t.Fatalf("GET /: status=%d type=%q version stamped=%v", r.StatusCode, r.Header.Get("Content-Type"), strings.Contains(string(body), version))
	}

	if !strings.Contains(string(body), `property="og:image" content="`+srv.URL+`/og.png"`) {
		t.Fatalf("GET /: og:image not stamped with request origin")
	}

	for path, typ := range map[string]string{
		"/robots.txt":  "text/plain",
		"/favicon.svg": "image/svg+xml",
		"/favicon.png": "image/png",
		"/og.png":      "image/png",
		"/settings.js": "text/javascript",
		"/audio.js":    "text/javascript",
		"/socket.js":   "text/javascript",
	} {
		if r := get(path); r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), typ) {
			t.Fatalf("GET %s: status=%d type=%q, want %s", path, r.StatusCode, r.Header.Get("Content-Type"), typ)
		}
	}

	r = get("/version")
	body, _ = io.ReadAll(r.Body)
	if r.StatusCode != 200 || string(body) != version+"\n" {
		t.Fatalf("GET /version: status=%d body=%q", r.StatusCode, body)
	}

	r = get("/nope")
	body, _ = io.ReadAll(r.Body)
	if r.StatusCode != 404 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(body), `name="robots" content="noindex"`) {
		t.Fatalf("GET /nope: status=%d type=%q noindex=%v", r.StatusCode, r.Header.Get("Content-Type"), strings.Contains(string(body), "noindex"))
	}

	if r := get("/ws?nick=Not_Valid"); r.StatusCode != 400 {
		t.Fatalf("GET /ws bad nick: status=%d", r.StatusCode)
	}

	r, err := http.Post(srv.URL+"/api/register", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var reg struct{ Nick string }
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil || !nickRE.MatchString(reg.Nick) {
		t.Fatalf("POST /api/register: err=%v nick=%q", err, reg.Nick)
	}

	var st struct{ Waiting, Paired int }
	if err := json.NewDecoder(get("/api/status").Body).Decode(&st); err != nil || st.Waiting != 0 || st.Paired != 0 {
		t.Fatalf("GET /api/status: err=%v got %+v", err, st)
	}
}
