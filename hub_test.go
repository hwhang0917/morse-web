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

func recvAny(t *testing.T, peer *client) ([]byte, bool) {
	t.Helper()
	select {
	case frame, open := <-peer.send:
		return frame, open
	case <-time.After(recvTimeout):
		t.Fatalf("%s: no frame within %v", peer.nick, recvTimeout)
		return nil, false
	}
}

// recv is recvAny minus online-count broadcasts, which have their own test.
func recv(t *testing.T, peer *client) ([]byte, bool) {
	t.Helper()
	for {
		frame, open := recvAny(t, peer)
		if !open || frame[0] != opOnline {
			return frame, open
		}
	}
}

func expectAny(t *testing.T, peer *client, want []byte) {
	t.Helper()
	got, open := recvAny(t, peer)
	if !open || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q (open=%v), want %q", peer.nick, got, open, want)
	}
}

func expect(t *testing.T, peer *client, want []byte) {
	t.Helper()
	got, open := recv(t, peer)
	if !open || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q (open=%v), want %q", peer.nick, got, open, want)
	}
}

func expectClosed(t *testing.T, peer *client) {
	t.Helper()
	if _, open := recv(t, peer); open {
		t.Fatalf("%s: send channel should be closed after leaving", peer.nick)
	}
}

func expectCounts(t *testing.T, matchmaker *hub, wantWaiting, wantPaired int32) {
	t.Helper()
	waiting, paired := matchmaker.nWaiting.Load(), matchmaker.nPaired.Load()
	if waiting != wantWaiting || paired != wantPaired {
		t.Fatalf("counters: waiting=%d paired=%d, want %d/%d", waiting, paired, wantWaiting, wantPaired)
	}
}

func online(count int) []byte      { return []byte{opOnline, byte(count >> 8), byte(count)} }
func paired(nick string) []byte    { return append([]byte{opPaired, 0}, nick...) }
func pairedBot(nick string) []byte { return append([]byte{opPaired, flagBot}, nick...) }

var waitingFrame = []byte{opWaiting}

// The exact sequence from the spec: A joins and waits; B joins and pairs with
// A; C joins and waits; B leaves so A pairs with C; B rejoins and waits; D
// joins and pairs with B.
func TestHubRoundRobin(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()

	alice, bob, carol, dave := fakeClient("A"), fakeClient("B"), fakeClient("C"), fakeClient("D")

	matchmaker.register <- alice
	expect(t, alice, waitingFrame)

	matchmaker.register <- bob
	expect(t, alice, paired("B"))
	expect(t, bob, paired("A"))

	matchmaker.register <- carol
	expect(t, carol, waitingFrame)

	matchmaker.unregister <- bob
	expect(t, alice, paired("C"))
	expect(t, carol, paired("A"))
	expectClosed(t, bob)

	bob = fakeClient("B")
	matchmaker.register <- bob
	expect(t, bob, waitingFrame)

	matchmaker.register <- dave
	expect(t, bob, paired("D"))
	expect(t, dave, paired("B"))

	expectCounts(t, matchmaker, 0, 4)
}

func TestHubBothLeave(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()

	alice, carol, eve := fakeClient("A"), fakeClient("C"), fakeClient("E")
	matchmaker.register <- alice
	matchmaker.register <- carol
	expect(t, alice, waitingFrame)
	expect(t, alice, paired("C"))
	expect(t, carol, paired("A"))

	matchmaker.unregister <- alice
	matchmaker.unregister <- carol
	expect(t, carol, waitingFrame)
	expectClosed(t, carol)

	matchmaker.register <- eve
	expect(t, eve, waitingFrame)
	expectCounts(t, matchmaker, 1, 0)
}

func TestHubRelaysSymbolsToPartner(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()

	alice, bob := fakeClient("A"), fakeClient("B")
	matchmaker.register <- alice
	matchmaker.register <- bob
	expect(t, alice, waitingFrame)
	expect(t, alice, paired("B"))
	expect(t, bob, paired("A"))

	matchmaker.relay <- symbolMsg{from: alice, op: opDash}
	expect(t, bob, []byte{opDash})
	matchmaker.relay <- symbolMsg{from: bob, op: opLetterGap}
	expect(t, alice, []byte{opLetterGap})
}

func TestHubBroadcastsOnlineCount(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()

	alice, bob := fakeClient("A"), fakeClient("B")
	matchmaker.register <- alice
	expectAny(t, alice, waitingFrame)
	expectAny(t, alice, online(1))

	matchmaker.register <- bob
	expectAny(t, alice, paired("B"))
	expectAny(t, bob, paired("A"))
	expectAny(t, alice, online(2))
	expectAny(t, bob, online(2))

	matchmaker.unregister <- alice
	expectAny(t, bob, waitingFrame)
	expectAny(t, bob, online(1))
}

func TestHubFlagsBotPartner(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()

	human, bot := fakeClient("H"), fakeClient("B")
	bot.bot = true
	matchmaker.register <- human
	matchmaker.register <- bot
	expect(t, human, waitingFrame)
	expect(t, human, pairedBot("B"))
	expect(t, bot, paired("H"))
}

// A client that prefers humans is never paired with a bot, in either order,
// and pairing stays FIFO among compatible clients.
func TestHubPreferHumanSkipsBots(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()

	picky := fakeClient("P")
	picky.humanOnly = true
	bot := fakeClient("B")
	bot.bot = true
	human := fakeClient("H")
	anyone := fakeClient("A")

	matchmaker.register <- picky
	expect(t, picky, waitingFrame)
	matchmaker.register <- bot
	expect(t, bot, waitingFrame)
	matchmaker.register <- human
	expect(t, picky, paired("H"))
	expect(t, human, paired("P"))
	matchmaker.register <- anyone
	expect(t, anyone, pairedBot("B"))
	expect(t, bot, paired("A"))

	expectCounts(t, matchmaker, 0, 4)
}
