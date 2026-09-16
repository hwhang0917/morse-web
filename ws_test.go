package main

import (
	"bytes"
	"context"
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
	read := func(c *websocket.Conn, want []byte) {
		t.Helper()
		typ, b, err := c.Read(ctx)
		if err != nil || typ != websocket.MessageBinary || !bytes.Equal(b, want) {
			t.Fatalf("got %q (%v, err=%v), want %q", b, typ, err, want)
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
