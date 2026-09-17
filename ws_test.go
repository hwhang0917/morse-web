package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Two real browsers' worth of sockets through the whole stack: pairing
// frames arrive, a key press is relayed, and leaving re-queues the survivor.
func TestWebSocketEndToEnd(t *testing.T) {
	h := newHub()
	go h.run()
	srv := httptest.NewServer(newMux(h))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func(nick string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, strings.Replace(srv.URL, "http", "ws", 1)+"/ws?nick="+nick, nil)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	// read skips online-count broadcasts; the hub test covers those.
	read := func(c *websocket.Conn, want []byte) {
		t.Helper()
		for {
			typ, b, err := c.Read(ctx)
			if err == nil && typ == websocket.MessageBinary && len(b) > 0 && b[0] == opOnline {
				continue
			}
			if err != nil || typ != websocket.MessageBinary || !bytes.Equal(b, want) {
				t.Fatalf("got %q (%v, err=%v), want %q", b, typ, err, want)
			}
			return
		}
	}

	a := dial("sleepy_beaver_aaaa")
	defer a.CloseNow()
	read(a, []byte{opWaiting})

	b := dial("brave_otter_bbbb")
	read(a, append([]byte{opPaired}, "brave_otter_bbbb"...))
	read(b, append([]byte{opPaired}, "sleepy_beaver_aaaa"...))

	if err := a.Write(ctx, websocket.MessageBinary, keyDown); err != nil {
		t.Fatal(err)
	}
	read(b, keyDown)

	b.Close(websocket.StatusNormalClosure, "")
	read(a, []byte{opWaiting})
}

func TestWebSocketPerIPCap(t *testing.T) {
	h := newHub()
	go h.run()
	srv := httptest.NewServer(newMux(h))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.Replace(srv.URL, "http", "ws", 1) + "/ws?nick=sleepy_beaver_aaaa"

	var conns []*websocket.Conn
	for i := 0; i < maxConnsPerIP; i++ {
		c, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	if _, resp, err := websocket.Dial(ctx, url, nil); err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("conn %d should be refused with 429, got err=%v resp=%v", maxConnsPerIP, err, resp)
	}

	conns[0].Close(websocket.StatusNormalClosure, "")
	// The slot is released after the server notices the close; retry briefly.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, _, err := websocket.Dial(ctx, url, nil)
		if err == nil {
			c.CloseNow()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not released after close: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, c := range conns[1:] {
		c.CloseNow()
	}
}
