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

	r = get("/version")
	body, _ = io.ReadAll(r.Body)
	if r.StatusCode != 200 || string(body) != version+"\n" {
		t.Fatalf("GET /version: status=%d body=%q", r.StatusCode, body)
	}

	r = get("/nope")
	if r.StatusCode != 404 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET /nope: status=%d type=%q", r.StatusCode, r.Header.Get("Content-Type"))
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
