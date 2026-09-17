package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/coder/websocket"
)

const defaultAddr = ":8080"

// Embedded at build time; no ldflags to remember across Docker and cross builds.
//
//go:embed VERSION
var rawVersion string
var version = strings.TrimSpace(rawVersion)

//go:embed web/index.html
var rawIndexHTML []byte

// The page is static, so the version is stamped in once at startup. The
// origin ({{ORIGIN}}) is stamped per request: OG tags need absolute URLs and
// the host is only known when a request arrives.
var indexHTML = bytes.ReplaceAll(rawIndexHTML, []byte("{{VERSION}}"), []byte(version))

//go:embed web/static
var staticFS embed.FS

//go:embed web/404.html
var notFoundHTML []byte

// nickRE bounds what one stranger can make another stranger's screen show.
var nickRE = regexp.MustCompile(`^[a-z]{1,20}_[a-z]{1,20}_[a-z0-9]{4}$`)

var adjectives = strings.Fields(`sleepy brave curious dizzy eager fluffy gentle happy jolly
	kind lazy merry nimble proud quiet rusty shy tiny witty zesty clever bouncy calm
	dapper fuzzy grumpy hungry icy lucky mellow`)

var animals = strings.Fields(`beaver otter badger fox owl heron moose lynx mole newt panda
	quail raven seal toad vole walrus yak zebra crab dingo eagle ferret gecko hare
	ibis koala lemur marmot narwhal`)

const suffixAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
const suffixLen = 4

func newNick() string {
	var s strings.Builder
	s.WriteString(adjectives[rand.IntN(len(adjectives))])
	s.WriteByte('_')
	s.WriteString(animals[rand.IntN(len(animals))])
	s.WriteByte('_')
	for range suffixLen {
		s.WriteByte(suffixAlphabet[rand.IntN(len(suffixAlphabet))])
	}
	return s.String()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// origin is the scheme and host a client used to reach us, honouring a
// TLS-terminating proxy's X-Forwarded-Proto.
func origin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func newMux(h *hub) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(bytes.ReplaceAll(indexHTML, []byte("{{ORIGIN}}"), []byte(origin(r))))
	})
	// One route per embedded static file (favicon, robots.txt, og image), so
	// the catch-all 404 below still owns every other path.
	static, _ := fs.Sub(staticFS, "web/static")
	entries, _ := fs.ReadDir(static, ".")
	for _, e := range entries {
		name := e.Name()
		mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, static, name)
		})
	}
	// Designated 404 page for every other GET. Not served via http.FS because
	// that would force a 200.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write(notFoundHTML)
	})
	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"nick": newNick()})
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]int32{"waiting": h.nWaiting.Load(), "paired": h.nPaired.Load()})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, version)
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) { serveWS(h, w, r) })
	return mux
}

func serveWS(h *hub, w http.ResponseWriter, r *http.Request) {
	nick := r.URL.Query().Get("nick")
	if !nickRE.MatchString(nick) {
		http.Error(w, "bad nick", http.StatusBadRequest)
		return
	}
	// Default AcceptOptions enforce same-origin, which is right: this binary
	// serves the page. Add OriginPatterns only if a proxy rewrites Host.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("%s: accept: %v", nick, err)
		return
	}
	defer conn.CloseNow()

	// Library docs advise against r.Context() after the hijack.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &client{conn: conn, nick: nick, send: make(chan []byte, sendBuf), ctx: ctx, cancel: cancel}
	h.register <- c
	defer func() { h.unregister <- c }()
	go c.writePump()
	c.readPump(h)
}

func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.BoolVar(showVersion, "v", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	h := newHub()
	go h.run()
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newMux(h)))
}
