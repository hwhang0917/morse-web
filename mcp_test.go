package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An MCP agent connects, gets paired with a WebSocket user, sends text that
// arrives as key frames, and decodes what the user keys back.
func TestMCPEndToEnd(t *testing.T) {
	matchmaker := newHub()
	go matchmaker.run()
	server := httptest.NewServer(newMux(matchmaker))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	agent := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := agent.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("%s: tool error: %v", name, result.Content)
		}
		output, _ := result.StructuredContent.(map[string]any)
		return output
	}

	// A human arrives after the agent starts waiting.
	go func() {
		time.Sleep(200 * time.Millisecond)
		human, _, err := websocket.Dial(ctx, wsURL(server, "nick=sleepy_beaver_aaaa"), nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer human.CloseNow()
		// Expect: paired-with-bot frame, then the symbols spelling "E" (one dot).
		var dots int
		deadline := time.After(10 * time.Second)
		for dots < 1 {
			select {
			case <-deadline:
				t.Error("human never received a dot")
				return
			default:
			}
			_, frame, err := human.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			if frame[0] == opPaired && frame[1]&flagBot == 0 {
				t.Error("agent should be flagged as bot")
			}
			if frame[0] == opDot {
				dots++
			}
		}
		// Send "I" back: two dots and a letter gap.
		for _, op := range []byte{opDot, opDot, opLetterGap} {
			human.Write(ctx, websocket.MessageBinary, []byte{op})
		}
		time.Sleep(2 * time.Second) // stay until the agent has received
	}()

	output := call("connect", map[string]any{"wait_seconds": 10})
	if output["partner"] != "sleepy_beaver_aaaa" {
		t.Fatalf("connect: %v", output)
	}
	output = call("send", map[string]any{"text": "E"})
	if output["sent"] != "E" {
		t.Fatalf("send: %v", output)
	}
	output = call("receive", map[string]any{"seconds": 2})
	if output["text"] != "I" {
		t.Fatalf("receive: %v", output)
	}
	call("disconnect", map[string]any{})
}
