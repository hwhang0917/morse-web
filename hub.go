package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Wire protocol: every frame is binary, first byte is the opcode.
const (
	opKeyUp   = 0x00 // both directions, no payload
	opKeyDown = 0x01 // both directions, no payload
	opWaiting = 0x10 // server->client; also means "partner left"
	opPaired  = 0x11 // server->client, followed by a flags byte then partner nick (UTF-8)
	opOnline  = 0x20 // server->client, followed by uint16 big-endian online count
)

// Flags byte in opPaired frames.
const flagBot = 0x01 // partner declared itself a bot (?bot=1); honour system

// Shared frames so relaying allocates nothing. Never mutate.
var (
	keyUp   = []byte{opKeyUp}
	keyDown = []byte{opKeyDown}
)

const (
	sendBuf      = 16
	pingInterval = 30 * time.Second
	pingTimeout  = 5 * time.Second
)

type client struct {
	conn   *websocket.Conn
	nick   string
	bot    bool
	send   chan []byte // closed only by the hub
	ctx    context.Context
	cancel context.CancelFunc
	// partner is owned by the hub goroutine; pumps never touch it.
	partner *client
}

// push never blocks the hub: a full buffer means the writer is stuck, so the
// client is torn down instead of stalling everyone else.
func (c *client) push(b []byte) {
	select {
	case c.send <- b:
	default:
		c.cancel()
	}
}

type keyMsg struct {
	from *client
	down bool
}

type hub struct {
	register   chan *client
	unregister chan *client
	relay      chan keyMsg
	waiting    []*client // FIFO queue
	clients    map[*client]struct{}
	// nPaired counts clients, not pairs.
	nWaiting, nPaired atomic.Int32
}

func newHub() *hub {
	return &hub{
		register:   make(chan *client),
		unregister: make(chan *client),
		relay:      make(chan keyMsg),
		clients:    map[*client]struct{}{},
	}
}

// run owns waiting and every partner pointer; nothing else touches them.
// Invariant: a client is queued XOR paired XOR being torn down.
func (h *hub) run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = struct{}{}
			kind := "user"
			if c.bot {
				kind = "bot"
			}
			log.Printf("connect %s %s (online=%d)", kind, c.nick, len(h.clients))
			h.enqueue(c)
			h.broadcastOnline()
		case c := <-h.unregister:
			delete(h.clients, c)
			log.Printf("disconnect %s (online=%d)", c.nick, len(h.clients))
			for i, w := range h.waiting {
				if w == c {
					h.waiting = append(h.waiting[:i], h.waiting[i+1:]...)
					h.nWaiting.Add(-1)
					break
				}
			}
			if p := c.partner; p != nil {
				c.partner, p.partner = nil, nil
				h.nPaired.Add(-2)
				h.enqueue(p)
			}
			close(c.send)
			h.broadcastOnline()
		case m := <-h.relay:
			if p := m.from.partner; p != nil {
				if m.down {
					p.push(keyDown)
				} else {
					p.push(keyUp)
				}
			}
		}
	}
}

// broadcastOnline tells everyone how many sockets are connected.
// ponytail: O(n) per join/leave; coalesce on a ticker if churn ever matters.
func (h *hub) broadcastOnline() {
	n := len(h.clients)
	frame := []byte{opOnline, byte(n >> 8), byte(n)}
	for c := range h.clients {
		c.push(frame)
	}
}

// enqueue pairs c with the longest-waiting client, or parks c in the queue.
func (h *hub) enqueue(c *client) {
	if len(h.waiting) == 0 {
		h.waiting = append(h.waiting, c)
		h.nWaiting.Add(1)
		c.push([]byte{opWaiting})
		return
	}
	p := h.waiting[0]
	h.waiting = h.waiting[1:]
	h.nWaiting.Add(-1)
	c.partner, p.partner = p, c
	h.nPaired.Add(2)
	c.push(pairedFrame(p))
	p.push(pairedFrame(c))
}

func pairedFrame(partner *client) []byte {
	var flags byte
	if partner.bot {
		flags |= flagBot
	}
	return append([]byte{opPaired, flags}, partner.nick...)
}

// readPump forwards key frames to the hub until the socket dies. Reading
// continuously is also what lets the library answer pings and close frames.
func (c *client) readPump(h *hub) {
	for {
		typ, b, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(b) != 1 || b[0] > opKeyDown {
			c.conn.Close(websocket.StatusUnsupportedData, "expected 1-byte key frame")
			return
		}
		h.relay <- keyMsg{from: c, down: b[0] == opKeyDown}
	}
}

// writePump drains send and pings the peer. Browsers never send pings, so
// without ours a phone that loses radio would stay paired until the kernel
// TCP timeout, leaving its partner with a ghost.
func (c *client) writePump() {
	defer c.cancel()
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case b, ok := <-c.send:
			if !ok || c.conn.Write(c.ctx, websocket.MessageBinary, b) != nil {
				return
			}
		case <-t.C:
			ctx, cancel := context.WithTimeout(c.ctx, pingTimeout)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				log.Printf("%s: ping failed: %v", c.nick, err)
				return
			}
		}
	}
}
