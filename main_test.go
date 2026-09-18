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
		if nick := newNick(); !nickRE.MatchString(nick) {
			t.Fatalf("nick %q does not match %s", nick, nickRE)
		}
	}
}

func TestVersionIsTrimmed(t *testing.T) {
	if version == "" || strings.TrimSpace(version) != version {
		t.Fatalf("version %q should be non-empty with no surrounding whitespace", version)
	}
}

func TestRoutes(t *testing.T) {
	server := httptest.NewServer(newMux(newHub()))
	defer server.Close()

	get := func(path string) *http.Response {
		t.Helper()
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := get("/")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "v"+version) {
		t.Fatalf("GET /: status=%d type=%q version stamped=%v", resp.StatusCode, resp.Header.Get("Content-Type"), strings.Contains(string(body), version))
	}

	if !strings.Contains(string(body), `property="og:image" content="`+server.URL+`/img/og.png"`) {
		t.Fatalf("GET /: og:image not stamped with request origin")
	}
	if !strings.Contains(string(body), `<script type="application/ld+json">`) || !strings.Contains(string(body), `"url": "`+server.URL+`/"`) {
		t.Fatalf("GET /: JSON-LD missing or not stamped with request origin")
	}

	for path, wantType := range map[string]string{
		"/robots.txt":               "text/plain",
		"/img/favicon.svg":          "image/svg+xml",
		"/img/favicon.png":          "image/png",
		"/img/og.png":               "image/png",
		"/js/settings.js":           "text/javascript",
		"/js/audio.js":              "text/javascript",
		"/js/socket.js":             "text/javascript",
		"/js/app.js":                "text/javascript",
		"/js/morse.js":              "text/javascript",
		"/css/base.css":             "text/css",
		"/css/app.css":              "text/css",
		"/css/404.css":              "text/css",
		"/css/mcp.css":              "text/css",
		"/.well-known/security.txt": "text/plain",
		"/llms.txt":                 "text/plain",
		"/mcp.html":                 "text/html",
		"/js/mcp-guide.js":          "text/javascript",
	} {
		if resp := get(path); resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), wantType) {
			t.Fatalf("GET %s: status=%d type=%q, want %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), wantType)
		}
	}

	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "X-Frame-Options"} {
		if resp.Header.Get(name) == "" {
			t.Fatalf("GET /: missing %s header", name)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || strings.Contains(string(body), "<script type=\"module\">") {
		t.Fatalf("CSP %q requires the page script to be external", csp)
	}

	resp = get("/version")
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != version+"\n" {
		t.Fatalf("GET /version: status=%d body=%q", resp.StatusCode, body)
	}

	resp = get("/nope")
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 404 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || !strings.Contains(string(body), `name="robots" content="noindex"`) {
		t.Fatalf("GET /nope: status=%d type=%q noindex=%v", resp.StatusCode, resp.Header.Get("Content-Type"), strings.Contains(string(body), "noindex"))
	}

	if resp := get("/ws?nick=Not_Valid"); resp.StatusCode != 400 {
		t.Fatalf("GET /ws bad nick: status=%d", resp.StatusCode)
	}

	resp, err := http.Post(server.URL+"/api/register", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var registered struct{ Nick string }
	if err := json.NewDecoder(resp.Body).Decode(&registered); err != nil || !nickRE.MatchString(registered.Nick) {
		t.Fatalf("POST /api/register: err=%v nick=%q", err, registered.Nick)
	}

	var status struct{ Waiting, Paired int }
	if err := json.NewDecoder(get("/api/status").Body).Decode(&status); err != nil || status.Waiting != 0 || status.Paired != 0 {
		t.Fatalf("GET /api/status: err=%v got %+v", err, status)
	}
}
