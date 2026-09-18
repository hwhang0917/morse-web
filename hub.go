package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Wire protocol: every frame is binary and the first byte is the opcode.
const (
	opKeyUp   = 0x00 // both directions
	opKeyDown = 0x01 // both directions
	opWaiting = 0x10 // server -> client; also sent when the partner leaves
	opPaired  = 0x11 // server -> client, then a flags byte, then the partner nick
	opOnline  = 0x20 // server -> client, then a big-endian uint16 client count
)

const flagBot = 0x01 // opPaired flags byte: the partner declared itself a bot

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
	conn      *websocket.Conn
	nick      string
	bot       bool
	humanOnly bool
	send      chan []byte // closed only by the hub
	ctx       context.Context
	cancel    context.CancelFunc
	partner   *client // owned by the hub goroutine
}

// push never blocks the hub: a full buffer means the writer is stuck, so the
// client is torn down instead of stalling everyone else.
func (c *client) push(frame []byte) {
	select {
	case c.send <- frame:
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
	waiting    []*client
	clients    map[*client]struct{}
	nWaiting   atomic.Int32
	nPaired    atomic.Int32 // clients, not pairs
}

func newHub() *hub {
	return &hub{
		register:   make(chan *client),
		unregister: make(chan *client),
		relay:      make(chan keyMsg),
		clients:    map[*client]struct{}{},
	}
}

// run owns the queue and every partner pointer. A client is queued, paired,
// or being torn down, never two of those at once.
func (h *hub) run() {
	for {
		select {
		case joined := <-h.register:
			h.clients[joined] = struct{}{}
			log.Printf("connect %s %s (online=%d)", clientKind(joined), joined.nick, len(h.clients))
			h.enqueue(joined)
			h.broadcastOnline()

		case left := <-h.unregister:
			delete(h.clients, left)
			log.Printf("disconnect %s (online=%d)", left.nick, len(h.clients))
			h.dequeue(left)
			if partner := left.partner; partner != nil {
				left.partner, partner.partner = nil, nil
				h.nPaired.Add(-2)
				h.enqueue(partner)
			}
			close(left.send)
			h.broadcastOnline()

		case msg := <-h.relay:
			partner := msg.from.partner
			if partner == nil {
				continue
			}
			if msg.down {
				partner.push(keyDown)
			} else {
				partner.push(keyUp)
			}
		}
	}
}

func clientKind(c *client) string {
	if c.bot {
		return "bot"
	}
	return "user"
}

// ponytail: O(n) per join/leave; coalesce on a ticker if churn ever matters.
func (h *hub) broadcastOnline() {
	count := len(h.clients)
	frame := []byte{opOnline, byte(count >> 8), byte(count)}
	for member := range h.clients {
		member.push(frame)
	}
}

func compatible(first, second *client) bool {
	return !(first.humanOnly && second.bot) && !(second.humanOnly && first.bot)
}

// enqueue pairs the newcomer with the longest-waiting compatible client, or
// parks it in the queue.
func (h *hub) enqueue(newcomer *client) {
	for _, candidate := range h.waiting {
		if !compatible(newcomer, candidate) {
			continue
		}
		h.dequeue(candidate)
		newcomer.partner, candidate.partner = candidate, newcomer
		h.nPaired.Add(2)
		newcomer.push(pairedFrame(candidate))
		candidate.push(pairedFrame(newcomer))
		return
	}
	h.waiting = append(h.waiting, newcomer)
	h.nWaiting.Add(1)
	newcomer.push([]byte{opWaiting})
}

func (h *hub) dequeue(target *client) {
	for index, queued := range h.waiting {
		if queued == target {
			h.waiting = append(h.waiting[:index], h.waiting[index+1:]...)
			h.nWaiting.Add(-1)
			return
		}
	}
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
		kind, frame, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageBinary || len(frame) != 1 || frame[0] > opKeyDown {
			c.conn.Close(websocket.StatusUnsupportedData, "expected 1-byte key frame")
			return
		}
		h.relay <- keyMsg{from: c, down: frame[0] == opKeyDown}
	}
}

// writePump drains send and pings the peer. Browsers never send pings, so
// without ours a phone that loses radio would stay paired until the kernel
// TCP timeout, leaving its partner with a ghost.
func (c *client) writePump() {
	defer c.cancel()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case frame, open := <-c.send:
			if !open || c.conn.Write(c.ctx, websocket.MessageBinary, frame) != nil {
				return
			}
		case <-ticker.C:
			if err := c.ping(); err != nil {
				log.Printf("%s: ping failed: %v", c.nick, err)
				return
			}
		}
	}
}

func (c *client) ping() error {
	ctx, cancel := context.WithTimeout(c.ctx, pingTimeout)
	defer cancel()
	return c.conn.Ping(ctx)
}
