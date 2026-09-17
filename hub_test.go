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

// recvAny blocks for the next frame pushed to c, or fails the test.
func recvAny(t *testing.T, c *client) ([]byte, bool) {
	t.Helper()
	select {
	case b, ok := <-c.send:
		return b, ok
	case <-time.After(recvTimeout):
		t.Fatalf("%s: no frame within %v", c.nick, recvTimeout)
		return nil, false
	}
}

// recv is recvAny minus online-count broadcasts, which have their own test.
func recv(t *testing.T, c *client) ([]byte, bool) {
	t.Helper()
	for {
		b, ok := recvAny(t, c)
		if !ok || b[0] != opOnline {
			return b, ok
		}
	}
}

func online(n int) []byte { return []byte{opOnline, byte(n >> 8), byte(n)} }

func expectAny(t *testing.T, c *client, want []byte) {
	t.Helper()
	got, ok := recvAny(t, c)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q (open=%v), want %q", c.nick, got, ok, want)
	}
}

func expect(t *testing.T, c *client, want []byte) {
	t.Helper()
	got, ok := recv(t, c)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q (open=%v), want %q", c.nick, got, ok, want)
	}
}

func paired(nick string) []byte    { return append([]byte{opPaired, 0}, nick...) }
func pairedBot(nick string) []byte { return append([]byte{opPaired, flagBot}, nick...) }

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

func TestHubBroadcastsOnlineCount(t *testing.T) {
	h := newHub()
	go h.run()

	a, b := fakeClient("A"), fakeClient("B")
	h.register <- a
	expectAny(t, a, []byte{opWaiting})
	expectAny(t, a, online(1))

	h.register <- b
	expectAny(t, a, paired("B"))
	expectAny(t, b, paired("A"))
	expectAny(t, a, online(2))
	expectAny(t, b, online(2))

	h.unregister <- a
	expectAny(t, b, []byte{opWaiting})
	expectAny(t, b, online(1))
}

func TestHubFlagsBotPartner(t *testing.T) {
	h := newHub()
	go h.run()
	human, bot := fakeClient("H"), fakeClient("B")
	bot.bot = true
	h.register <- human
	h.register <- bot
	expect(t, human, []byte{opWaiting})
	expect(t, human, pairedBot("B"))
	expect(t, bot, paired("H"))
}
