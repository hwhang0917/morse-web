package main

import (
	"bytes"
	"testing"
	"time"
)

const recvTimeout = time.Second

func fakeClient(nick string) *client {
	return &client{nick: nick, send: make(chan []byte, sendBuf), cancel: func() {}}
}

// recv blocks for the next frame pushed to c, or fails the test.
func recv(t *testing.T, c *client) ([]byte, bool) {
	t.Helper()
	select {
	case b, ok := <-c.send:
		return b, ok
	case <-time.After(recvTimeout):
		t.Fatalf("%s: no frame within %v", c.nick, recvTimeout)
		return nil, false
	}
}

func expect(t *testing.T, c *client, want []byte) {
	t.Helper()
	got, ok := recv(t, c)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q (open=%v), want %q", c.nick, got, ok, want)
	}
}

func paired(nick string) []byte { return append([]byte{opPaired}, nick...) }

// The exact sequence from the spec:
// A joins -> waits; B joins -> A+B; C joins -> waits; B leaves -> A+C;
// B rejoins -> waits; D joins -> B+D.
func TestHubRoundRobin(t *testing.T) {
	h := newHub()
	go h.run()

	a, b, c, d := fakeClient("A"), fakeClient("B"), fakeClient("C"), fakeClient("D")

	h.register <- a
	expect(t, a, []byte{opWaiting})

	h.register <- b
	expect(t, a, paired("B"))
	expect(t, b, paired("A"))

	h.register <- c
	expect(t, c, []byte{opWaiting})

	h.unregister <- b
	expect(t, a, paired("C"))
	expect(t, c, paired("A"))
	if _, ok := recv(t, b); ok {
		t.Fatal("B: send channel should be closed after leaving")
	}

	b = fakeClient("B")
	h.register <- b
	expect(t, b, []byte{opWaiting})

	h.register <- d
	expect(t, b, paired("D"))
	expect(t, d, paired("B"))

	if w, p := h.nWaiting.Load(), h.nPaired.Load(); w != 0 || p != 4 {
		t.Fatalf("counters: waiting=%d paired=%d, want 0/4", w, p)
	}
}

func TestHubBothLeave(t *testing.T) {
	h := newHub()
	go h.run()

	a, c, e := fakeClient("A"), fakeClient("C"), fakeClient("E")
	h.register <- a
	h.register <- c
	expect(t, a, []byte{opWaiting})
	expect(t, a, paired("C"))
	expect(t, c, paired("A"))

	h.unregister <- a
	h.unregister <- c
	expect(t, c, []byte{opWaiting})
	if _, ok := recv(t, c); ok {
		t.Fatal("C: send channel should be closed after leaving")
	}

	// Queue must not hold the departed C.
	h.register <- e
	expect(t, e, []byte{opWaiting})
	if w, p := h.nWaiting.Load(), h.nPaired.Load(); w != 1 || p != 0 {
		t.Fatalf("counters: waiting=%d paired=%d, want 1/0", w, p)
	}
}

func TestHubRelaysKeyToPartner(t *testing.T) {
	h := newHub()
	go h.run()

	a, b := fakeClient("A"), fakeClient("B")
	h.register <- a
	h.register <- b
	expect(t, a, []byte{opWaiting})
	expect(t, a, paired("B"))
	expect(t, b, paired("A"))

	h.relay <- keyMsg{from: a, down: true}
	expect(t, b, keyDown)
	h.relay <- keyMsg{from: b, down: false}
	expect(t, a, keyUp)
}
