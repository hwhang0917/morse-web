package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP: lets an LLM client send and receive Morse without writing a WebSocket
// client. One MCP session owns one hub client, flagged as a bot. The server
// does the keying and decoding so the agent only deals in text.

const (
	mcpSessionTimeout    = 10 * time.Minute // abandoned sessions are reaped by the SDK
	mcpSendBuf           = 256              // a drain goroutine keeps this nearly empty
	mcpDefaultWait       = 30 * time.Second
	mcpMaxWait           = 120 * time.Second
	mcpDefaultWPM        = 15
	mcpMinWPM, mcpMaxWPM = 5, 40
	mcpDefaultListen     = 5 * time.Second
	mcpMaxListen         = 60 * time.Second
)

var errNotConnected = errors.New("not connected: call connect first")
var errNoPartner = errors.New("no partner yet: wait for a pairing before sending")

type botSession struct {
	id    string
	mgr   *mcpManager
	c     *client
	ipKey string

	mu         sync.Mutex
	keys       []keyEvent // partner key events since the last receive
	partner    string
	partnerBot bool

	paired       chan struct{} // closed on first pairing
	pairedOnce   sync.Once
	baseCancel   context.CancelFunc
	teardownOnce sync.Once
}

type mcpManager struct {
	hub      *hub
	gauge    *connGauge
	sessions sync.Map // MCP session ID -> *botSession
}

func (m *mcpManager) get(req *mcp.CallToolRequest) (*botSession, error) {
	v, ok := m.sessions.Load(req.Session.ID())
	if !ok {
		return nil, errNotConnected
	}
	return v.(*botSession), nil
}

// drain moves hub frames into the session's state so the 16-frame hub buffer
// never fills while the agent is between tool calls.
func (bs *botSession) drain() {
	for frame := range bs.c.send {
		switch frame[0] {
		case opKeyDown, opKeyUp:
			bs.mu.Lock()
			bs.keys = append(bs.keys, keyEvent{down: frame[0] == opKeyDown, at: time.Now()})
			bs.mu.Unlock()
		case opPaired:
			bs.mu.Lock()
			bs.partner = string(frame[2:])
			bs.partnerBot = frame[1]&flagBot != 0
			bs.mu.Unlock()
			bs.pairedOnce.Do(func() { close(bs.paired) })
		case opWaiting:
			bs.mu.Lock()
			bs.partner = ""
			bs.mu.Unlock()
		}
	}
}

func (bs *botSession) partnerInfo() (string, bool) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.partner, bs.partnerBot
}

// teardown leaves the hub exactly once, whichever of the disconnect tool,
// the MCP session ending, or a hub-side cancel gets there first.
func (bs *botSession) teardown() {
	bs.teardownOnce.Do(func() {
		bs.baseCancel()
		bs.mgr.hub.unregister <- bs.c
		bs.mgr.sessions.Delete(bs.id)
		bs.mgr.gauge.release(bs.ipKey)
	})
}

// ipKey mirrors clientIP for MCP calls, where only the headers are available:
// the proxy-appended X-Forwarded-For hop, or one shared bucket when direct.
func ipKey(h http.Header) string {
	if xff := h.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		return strings.TrimSpace(hops[len(hops)-1])
	}
	return "direct"
}

type connectIn struct {
	Nick        string `json:"nick,omitempty" jsonschema:"nickname to use; one is generated when omitted or invalid"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for a partner (default 30, max 120)"`
}

type connectOut struct {
	Nick         string `json:"nick"`
	Partner      string `json:"partner"`
	PartnerIsBot bool   `json:"partner_is_bot"`
	Status       string `json:"status"`
}

func (m *mcpManager) connect(ctx context.Context, req *mcp.CallToolRequest, in connectIn) (*mcp.CallToolResult, connectOut, error) {
	id := req.Session.ID()
	if _, exists := m.sessions.Load(id); exists {
		return nil, connectOut{}, errors.New("already connected: call disconnect first")
	}
	key := "direct"
	if req.Extra != nil {
		key = ipKey(req.Extra.Header)
	}
	if !m.gauge.acquire(key) {
		return nil, connectOut{}, errors.New("too many connections from this address")
	}

	nick := in.Nick
	if !nickRE.MatchString(nick) {
		nick = newNick()
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	bs := &botSession{id: id, mgr: m, ipKey: key, paired: make(chan struct{}), baseCancel: cancel}
	bs.c = &client{nick: nick, bot: true, send: make(chan []byte, mcpSendBuf), ctx: baseCtx}
	// The hub calls cancel when it drops a client; that must also leave the hub.
	bs.c.cancel = func() { go bs.teardown() }
	m.sessions.Store(id, bs)

	go bs.drain()
	m.hub.register <- bs.c
	// Reap the hub client when the MCP session ends without a disconnect call.
	go func() {
		req.Session.Wait()
		bs.teardown()
	}()

	wait := mcpDefaultWait
	if in.WaitSeconds > 0 {
		wait = min(time.Duration(in.WaitSeconds)*time.Second, mcpMaxWait)
	}
	select {
	case <-bs.paired:
	case <-time.After(wait):
	case <-ctx.Done():
		return nil, connectOut{}, ctx.Err()
	}
	partner, isBot := bs.partnerInfo()
	status := "paired"
	if partner == "" {
		status = "waiting: no partner yet, call connect_status or send later"
	}
	return nil, connectOut{Nick: nick, Partner: partner, PartnerIsBot: isBot, Status: status}, nil
}

type sendIn struct {
	Text string `json:"text" jsonschema:"text to key as Morse; letters, digits and . , ? / = are sent, other characters skipped"`
	WPM  int    `json:"wpm,omitempty" jsonschema:"speed in words per minute (default 15, range 5-40)"`
}

type sendOut struct {
	Sent    string  `json:"sent"`
	Seconds float64 `json:"seconds"`
}

func (m *mcpManager) send(ctx context.Context, req *mcp.CallToolRequest, in sendIn) (*mcp.CallToolResult, sendOut, error) {
	bs, err := m.get(req)
	if err != nil {
		return nil, sendOut{}, err
	}
	if p, _ := bs.partnerInfo(); p == "" {
		return nil, sendOut{}, errNoPartner
	}
	wpm := in.WPM
	if wpm == 0 {
		wpm = mcpDefaultWPM
	}
	if wpm < mcpMinWPM || wpm > mcpMaxWPM {
		return nil, sendOut{}, fmt.Errorf("wpm must be between %d and %d", mcpMinWPM, mcpMaxWPM)
	}
	start := time.Now()
	for _, step := range keyPlan(in.Text, wpm) {
		if p, _ := bs.partnerInfo(); p == "" {
			return nil, sendOut{}, errors.New("partner left mid-message")
		}
		m.hub.relay <- keyMsg{from: bs.c, down: step.down}
		if step.dur == 0 {
			continue
		}
		select {
		case <-time.After(step.dur):
		case <-ctx.Done():
			m.hub.relay <- keyMsg{from: bs.c, down: false}
			return nil, sendOut{}, ctx.Err()
		case <-bs.c.ctx.Done():
			return nil, sendOut{}, errors.New("disconnected")
		}
	}
	return nil, sendOut{Sent: in.Text, Seconds: time.Since(start).Seconds()}, nil
}

type receiveIn struct {
	Seconds int `json:"seconds,omitempty" jsonschema:"how long to listen before decoding (default 5, max 60)"`
}

type receiveOut struct {
	Text    string `json:"text"`
	Raw     string `json:"raw"`
	UnitMs  int    `json:"unit_ms"`
	Partner string `json:"partner"`
}

func (m *mcpManager) receive(ctx context.Context, req *mcp.CallToolRequest, in receiveIn) (*mcp.CallToolResult, receiveOut, error) {
	bs, err := m.get(req)
	if err != nil {
		return nil, receiveOut{}, err
	}
	listen := mcpDefaultListen
	if in.Seconds > 0 {
		listen = min(time.Duration(in.Seconds)*time.Second, mcpMaxListen)
	}
	select {
	case <-time.After(listen):
	case <-ctx.Done():
		return nil, receiveOut{}, ctx.Err()
	}
	// Everything the partner keyed since connect or the last receive.
	bs.mu.Lock()
	ev := bs.keys
	bs.keys = nil
	partner := bs.partner
	bs.mu.Unlock()
	text, raw, unit := decodeMorse(ev)
	return nil, receiveOut{Text: text, Raw: raw, UnitMs: int(unit / time.Millisecond), Partner: partner}, nil
}

type statusOut struct {
	Partner      string `json:"partner"`
	PartnerIsBot bool   `json:"partner_is_bot"`
}

func (m *mcpManager) status(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statusOut, error) {
	bs, err := m.get(req)
	if err != nil {
		return nil, statusOut{}, err
	}
	p, isBot := bs.partnerInfo()
	return nil, statusOut{Partner: p, PartnerIsBot: isBot}, nil
}

func (m *mcpManager) disconnect(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
	bs, err := m.get(req)
	if err != nil {
		return nil, struct{}{}, err
	}
	bs.teardown()
	return nil, struct{}{}, nil
}

func newMCPHandler(h *hub, gauge *connGauge) http.Handler {
	m := &mcpManager{hub: h, gauge: gauge}
	server := mcp.NewServer(&mcp.Implementation{Name: "morse-web", Version: version}, &mcp.ServerOptions{
		Instructions: "Send and receive Morse code with whoever is waiting on morse-web. " +
			"Call connect first (it waits for a partner), then send text and receive replies. " +
			"The person on the other end sees you as a bot.",
	})
	mcp.AddTool(server, &mcp.Tool{Name: "connect", Description: "Join the queue as a bot and wait to be paired with a partner."}, m.connect)
	mcp.AddTool(server, &mcp.Tool{Name: "send", Description: "Key text as Morse to your partner at the given speed. Blocks for the duration of the message."}, m.send)
	mcp.AddTool(server, &mcp.Tool{Name: "receive", Description: "Listen for the given number of seconds and return what the partner keyed, decoded to text plus the raw dots and dashes."}, m.receive)
	mcp.AddTool(server, &mcp.Tool{Name: "connect_status", Description: "Report the current partner, if any."}, m.status)
	mcp.AddTool(server, &mcp.Tool{Name: "disconnect", Description: "Leave the queue or the current conversation."}, m.disconnect)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionTimeout})
}
