package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// One MCP session owns one hub client flagged as a bot. The server keys and
// decodes so the agent only deals in text.

const (
	mcpSessionTimeout = 10 * time.Minute
	mcpSendBuf        = 256
	mcpDefaultWait    = 30 * time.Second
	mcpMaxWait        = 120 * time.Second
	mcpDefaultListen  = 5 * time.Second
	mcpMaxListen      = 60 * time.Second
	directIPKey       = "direct" // gauge bucket when no proxy header is present
)

var (
	errNotConnected = errors.New("not connected: call connect first")
	errNoPartner    = errors.New("no partner yet: wait for a pairing before sending")
	errPartnerLeft  = errors.New("partner left mid-message")
	errDisconnected = errors.New("disconnected")
)

type botSession struct {
	id        string
	manager   *mcpManager
	hubClient *client
	ipKey     string

	mu         sync.Mutex
	symbols    []byte // partner symbols since the last receive
	partner    string
	partnerBot bool

	paired       chan struct{} // closed on first pairing
	pairedOnce   sync.Once
	cancel       context.CancelFunc
	teardownOnce sync.Once
}

type mcpManager struct {
	hub      *hub
	gauge    *connGauge
	sessions sync.Map // MCP session ID -> *botSession
}

func (mgr *mcpManager) session(req *mcp.CallToolRequest) (*botSession, error) {
	value, found := mgr.sessions.Load(req.Session.ID())
	if !found {
		return nil, errNotConnected
	}
	return value.(*botSession), nil
}

// drain moves hub frames into session state so the hub's buffer never fills
// while the agent is between tool calls.
func (session *botSession) drain() {
	for frame := range session.hubClient.send {
		switch frame[0] {
		case opDot, opDash, opLetterGap, opWordGap:
			session.mu.Lock()
			session.symbols = append(session.symbols, frame[0])
			session.mu.Unlock()
		case opPaired:
			session.mu.Lock()
			session.partner = string(frame[2:])
			session.partnerBot = frame[1]&flagBot != 0
			session.mu.Unlock()
			session.pairedOnce.Do(func() { close(session.paired) })
		case opWaiting:
			session.mu.Lock()
			session.partner = ""
			session.mu.Unlock()
		}
	}
}

func (session *botSession) partnerInfo() (nick string, isBot bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.partner, session.partnerBot
}

func (session *botSession) hasPartner() bool {
	nick, _ := session.partnerInfo()
	return nick != ""
}

// teardown leaves the hub exactly once, whichever of the disconnect tool, the
// MCP session ending, or a hub-side cancel gets there first.
func (session *botSession) teardown() {
	session.teardownOnce.Do(func() {
		session.cancel()
		session.manager.hub.unregister <- session.hubClient
		session.manager.sessions.Delete(session.id)
		session.manager.gauge.release(session.ipKey)
	})
}

// ipKey mirrors clientIP for tool calls, where only headers are available.
func ipKey(req *mcp.CallToolRequest) string {
	if req.Extra == nil {
		return directIPKey
	}
	if forwarded, ok := forwardedClient(req.Extra.Header); ok {
		return forwarded
	}
	return directIPKey
}

func clampSeconds(seconds int, fallback, ceiling time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return min(time.Duration(seconds)*time.Second, ceiling)
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

func (mgr *mcpManager) connect(ctx context.Context, req *mcp.CallToolRequest, args connectIn) (*mcp.CallToolResult, connectOut, error) {
	id := req.Session.ID()
	if _, exists := mgr.sessions.Load(id); exists {
		return nil, connectOut{}, errors.New("already connected: call disconnect first")
	}
	key := ipKey(req)
	if !mgr.gauge.acquire(key) {
		return nil, connectOut{}, errors.New("too many connections from this address")
	}

	nick := args.Nick
	if !nickRE.MatchString(nick) {
		nick = newNick()
	}
	ctxForHub, cancel := context.WithCancel(context.Background())
	session := &botSession{id: id, manager: mgr, ipKey: key, paired: make(chan struct{}), cancel: cancel}
	session.hubClient = &client{
		nick:   nick,
		bot:    true,
		send:   make(chan []byte, mcpSendBuf),
		ctx:    ctxForHub,
		cancel: func() { go session.teardown() }, // a hub-side drop must also leave the hub
	}
	mgr.sessions.Store(id, session)

	go session.drain()
	mgr.hub.register <- session.hubClient
	go func() {
		req.Session.Wait()
		session.teardown()
	}()

	select {
	case <-session.paired:
	case <-time.After(clampSeconds(args.WaitSeconds, mcpDefaultWait, mcpMaxWait)):
	case <-ctx.Done():
		return nil, connectOut{}, ctx.Err()
	}
	partner, isBot := session.partnerInfo()
	status := "paired"
	if partner == "" {
		status = "waiting: no partner yet, call connect_status or send later"
	}
	return nil, connectOut{Nick: nick, Partner: partner, PartnerIsBot: isBot, Status: status}, nil
}

type sendIn struct {
	Text string `json:"text" jsonschema:"text to send as Morse; letters, digits and . , ? / = are sent, other characters skipped"`
}

type sendOut struct {
	Sent    string `json:"sent"`
	Symbols int    `json:"symbols"`
}

func (mgr *mcpManager) send(ctx context.Context, req *mcp.CallToolRequest, args sendIn) (*mcp.CallToolResult, sendOut, error) {
	session, err := mgr.session(req)
	if err != nil {
		return nil, sendOut{}, err
	}
	if !session.hasPartner() {
		return nil, sendOut{}, errNoPartner
	}
	symbols := encodeSymbols(args.Text)
	for _, op := range symbols {
		if !session.hasPartner() {
			return nil, sendOut{}, errPartnerLeft
		}
		select {
		case mgr.hub.relay <- symbolMsg{from: session.hubClient, op: op}:
		case <-ctx.Done():
			return nil, sendOut{}, ctx.Err()
		case <-session.hubClient.ctx.Done():
			return nil, sendOut{}, errDisconnected
		}
	}
	return nil, sendOut{Sent: args.Text, Symbols: len(symbols)}, nil
}

type receiveIn struct {
	Seconds int `json:"seconds,omitempty" jsonschema:"how long to listen before decoding (default 5, max 60)"`
}

type receiveOut struct {
	Text    string `json:"text"`
	Raw     string `json:"raw"`
	Partner string `json:"partner"`
}

func (mgr *mcpManager) receive(ctx context.Context, req *mcp.CallToolRequest, args receiveIn) (*mcp.CallToolResult, receiveOut, error) {
	session, err := mgr.session(req)
	if err != nil {
		return nil, receiveOut{}, err
	}
	select {
	case <-time.After(clampSeconds(args.Seconds, mcpDefaultListen, mcpMaxListen)):
	case <-ctx.Done():
		return nil, receiveOut{}, ctx.Err()
	}

	session.mu.Lock()
	symbols := session.symbols
	session.symbols = nil
	partner := session.partner
	session.mu.Unlock()

	text, raw := decodeSymbols(symbols)
	return nil, receiveOut{Text: text, Raw: raw, Partner: partner}, nil
}

type statusOut struct {
	Partner      string `json:"partner"`
	PartnerIsBot bool   `json:"partner_is_bot"`
}

func (mgr *mcpManager) status(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statusOut, error) {
	session, err := mgr.session(req)
	if err != nil {
		return nil, statusOut{}, err
	}
	partner, isBot := session.partnerInfo()
	return nil, statusOut{Partner: partner, PartnerIsBot: isBot}, nil
}

func (mgr *mcpManager) disconnect(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
	session, err := mgr.session(req)
	if err != nil {
		return nil, struct{}{}, err
	}
	session.teardown()
	return nil, struct{}{}, nil
}

func newMCPHandler(matchmaker *hub, gauge *connGauge) http.Handler {
	mgr := &mcpManager{hub: matchmaker, gauge: gauge}
	server := mcp.NewServer(&mcp.Implementation{Name: "morse-web", Version: version}, &mcp.ServerOptions{
		Instructions: "Send and receive Morse code with whoever is waiting on morse-web. " +
			"Call connect first (it waits for a partner), then send text and receive replies. " +
			"The person on the other end sees you as a bot.",
	})
	mcp.AddTool(server, &mcp.Tool{Name: "connect", Description: "Join the queue as a bot and wait to be paired with a partner."}, mgr.connect)
	mcp.AddTool(server, &mcp.Tool{Name: "send", Description: "Send text as Morse to your partner. Their client plays it back at its own speed."}, mgr.send)
	mcp.AddTool(server, &mcp.Tool{Name: "receive", Description: "Listen for the given number of seconds and return what the partner sent, decoded to text plus the raw dots and dashes."}, mgr.receive)
	mcp.AddTool(server, &mcp.Tool{Name: "connect_status", Description: "Report the current partner, if any."}, mgr.status)
	mcp.AddTool(server, &mcp.Tool{Name: "disconnect", Description: "Leave the queue or the current conversation."}, mgr.disconnect)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionTimeout})
}
