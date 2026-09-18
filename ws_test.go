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

func wsURL(server *httptest.Server, query string) string {
	return strings.Replace(server.URL, "http", "ws", 1) + "/ws?" + query
}

func TestWebSocketEndToEnd(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()
	server := httptest.NewServer(newMux(matchmaker))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func(query string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, wsURL(server, query), nil)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	// read skips online-count broadcasts; the hub test covers those.
	read := func(conn *websocket.Conn, want []byte) {
		t.Helper()
		for {
			kind, frame, err := conn.Read(ctx)
			if err == nil && kind == websocket.MessageBinary && len(frame) > 0 && frame[0] == opOnline {
				continue
			}
			if err != nil || kind != websocket.MessageBinary || !bytes.Equal(frame, want) {
				t.Fatalf("got %q (%v, err=%v), want %q", frame, kind, err, want)
			}
			return
		}
	}

	alice := dial("nick=sleepy_beaver_aaaa")
	defer alice.CloseNow()
	read(alice, []byte{opWaiting})

	bob := dial("nick=brave_otter_bbbb&bot=1")
	read(alice, append([]byte{opPaired, flagBot}, "brave_otter_bbbb"...))
	read(bob, append([]byte{opPaired, 0}, "sleepy_beaver_aaaa"...))

	if err := alice.Write(ctx, websocket.MessageBinary, keyDown); err != nil {
		t.Fatal(err)
	}
	read(bob, keyDown)

	bob.Close(websocket.StatusNormalClosure, "")
	read(alice, []byte{opWaiting})
}

func TestWebSocketPerIPCap(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()
	server := httptest.NewServer(newMux(matchmaker))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := wsURL(server, "nick=sleepy_beaver_aaaa")

	var conns []*websocket.Conn
	for index := 0; index < maxConnsPerIP; index++ {
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("conn %d: %v", index, err)
		}
		conns = append(conns, conn)
	}
	if _, resp, err := websocket.Dial(ctx, url, nil); err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("conn %d should be refused with 429, got err=%v resp=%v", maxConnsPerIP, err, resp)
	}

	conns[0].Close(websocket.StatusNormalClosure, "")
	// The slot frees once the server notices the close, so retry briefly.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err == nil {
			conn.CloseNow()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not released after close: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, conn := range conns[1:] {
		conn.CloseNow()
	}
}
