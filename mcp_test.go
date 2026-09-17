package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An MCP agent connects, gets paired with a WebSocket user, sends text that
// arrives as key frames, and decodes what the user keys back.
func TestMCPEndToEnd(t *testing.T) {
	h := newHub()
	go h.run()
	srv := httptest.NewServer(newMux(h))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := cl.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s: tool error: %v", name, res.Content)
		}
		out, _ := res.StructuredContent.(map[string]any)
		return out
	}

	// A human arrives after the agent starts waiting.
	go func() {
		time.Sleep(200 * time.Millisecond)
		c, _, err := websocket.Dial(ctx, strings.Replace(srv.URL, "http", "ws", 1)+"/ws?nick=sleepy_beaver_aaaa", nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.CloseNow()
		// Expect: paired-with-bot frame, then key frames spelling "E" (one dot).
		var downs int
		deadline := time.After(10 * time.Second)
		for downs < 1 {
			select {
			case <-deadline:
				t.Error("human never received a key down")
				return
			default:
			}
			_, b, err := c.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			if b[0] == opPaired && b[1]&flagBot == 0 {
				t.Error("agent should be flagged as bot")
			}
			if b[0] == opKeyDown {
				downs++
			}
		}
		// Key "I" back: two dots at 20 WPM (60ms unit).
		for i := 0; i < 2; i++ {
			c.Write(ctx, websocket.MessageBinary, keyDown)
			time.Sleep(60 * time.Millisecond)
			c.Write(ctx, websocket.MessageBinary, keyUp)
			time.Sleep(60 * time.Millisecond)
		}
		time.Sleep(2 * time.Second) // stay until the agent has received
	}()

	out := call("connect", map[string]any{"wait_seconds": 10})
	if out["partner"] != "sleepy_beaver_aaaa" {
		t.Fatalf("connect: %v", out)
	}
	out = call("send", map[string]any{"text": "E", "wpm": 20})
	if out["sent"] != "E" {
		t.Fatalf("send: %v", out)
	}
	out = call("receive", map[string]any{"seconds": 2})
	if out["text"] != "I" {
		t.Fatalf("receive: %v", out)
	}
	call("disconnect", map[string]any{})
}
