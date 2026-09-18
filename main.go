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

//go:embed VERSION
var rawVersion string

var version = strings.TrimSpace(rawVersion)

//go:embed web/index.html
var rawIndexHTML []byte

// The version is stamped once at startup; the origin is stamped per request
// because OG tags need absolute URLs and the host is only known then.
var indexHTML = bytes.ReplaceAll(rawIndexHTML, []byte("{{VERSION}}"), []byte(version))

//go:embed web/404.html
var notFoundHTML []byte

// all: is needed so the .well-known dotdir is embedded.
//
//go:embed all:web/static
var staticFS embed.FS

// nickRE bounds what one stranger can make another stranger's screen show.
var nickRE = regexp.MustCompile(`^[a-z]{1,20}_[a-z]{1,20}_[a-z0-9]{4}$`)

var adjectives = strings.Fields(`sleepy brave curious dizzy eager fluffy gentle happy jolly
	kind lazy merry nimble proud quiet rusty shy tiny witty zesty clever bouncy calm
	dapper fuzzy grumpy hungry icy lucky mellow`)

var animals = strings.Fields(`beaver otter badger fox owl heron moose lynx mole newt panda
	quail raven seal toad vole walrus yak zebra crab dingo eagle ferret gecko hare
	ibis koala lemur marmot narwhal`)

const (
	suffixAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	suffixLen      = 4
)

// Everything the page needs is same-origin and nothing is inline.
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; " +
	"img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func newNick() string {
	var nick strings.Builder
	nick.WriteString(adjectives[rand.IntN(len(adjectives))])
	nick.WriteByte('_')
	nick.WriteString(animals[rand.IntN(len(animals))])
	nick.WriteByte('_')
	for range suffixLen {
		nick.WriteByte(suffixAlphabet[rand.IntN(len(suffixAlphabet))])
	}
	return nick.String()
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func writeHTML(w http.ResponseWriter, status int, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(page)
}

// origin honours a TLS-terminating proxy's X-Forwarded-Proto.
func origin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", csp)
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func newMux(matchmaker *hub) http.Handler {
	mux := http.NewServeMux()
	gauge := &connGauge{}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page := bytes.ReplaceAll(indexHTML, []byte("{{ORIGIN}}"), []byte(origin(r)))
		writeHTML(w, http.StatusOK, page)
	})
	registerStaticFiles(mux)
	// Catch-all 404 for every other GET; http.FS would force a 200.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeHTML(w, http.StatusNotFound, notFoundHTML)
	})

	register := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"nick": newNick()})
	}
	mux.HandleFunc("POST /api/register", rateLimit(newLimiter(registerLimit, registerWindow), register))
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]int32{"waiting": matchmaker.nWaiting.Load(), "paired": matchmaker.nPaired.Load()})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, version)
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(matchmaker, gauge, w, r)
	})

	// A method-less "/mcp" would conflict with the "GET /" catch-all under the
	// mux's precedence rules, so each method is registered explicitly.
	mcpHandler := newMCPHandler(matchmaker, gauge)
	for _, method := range []string{"GET", "POST", "DELETE"} {
		mux.Handle(method+" /mcp", mcpHandler)
	}
	return securityHeaders(mux)
}

// registerStaticFiles adds one route per embedded file so the catch-all 404
// still owns every other path.
func registerStaticFiles(mux *http.ServeMux) {
	static, _ := fs.Sub(staticFS, "web/static")
	fs.WalkDir(static, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		mux.HandleFunc("GET /"+name, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, static, name)
		})
		return nil
	})
}

func serveWS(matchmaker *hub, gauge *connGauge, w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	nick := query.Get("nick")
	if !nickRE.MatchString(nick) {
		http.Error(w, "bad nick", http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	if !gauge.acquire(ip) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer gauge.release(ip)

	// Default AcceptOptions enforce same-origin, which is right because this
	// binary serves the page. Add OriginPatterns only if a proxy rewrites Host.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("%s: accept: %v", nick, err)
		return
	}
	defer conn.CloseNow()

	// The library advises against r.Context() after the hijack.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peer := &client{
		conn:      conn,
		nick:      nick,
		bot:       query.Get("bot") == "1",
		humanOnly: query.Get("human") == "1",
		send:      make(chan []byte, sendBuf),
		ctx:       ctx,
		cancel:    cancel,
	}
	matchmaker.register <- peer
	defer func() { matchmaker.unregister <- peer }()
	go peer.writePump()
	peer.readPump(matchmaker)
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

	matchmaker := newHub()
	go matchmaker.run()
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, newMux(matchmaker)))
}
