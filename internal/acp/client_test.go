package acp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// fakeAgent scripts the agent side of one ACP conversation over
// pipes: handshake, one session, one prompt turn that streams
// updates and asks permission twice before finishing.
func fakeAgent(t *testing.T, r io.Reader, w io.Writer) {
	t.Helper()
	dec := json.NewDecoder(r)
	enc := json.NewEncoder(w)
	reply := func(id json.RawMessage, result any) {
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}); err != nil {
			t.Errorf("agent write: %v", err)
		}
	}
	notify := func(update map[string]any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update",
			"params": map[string]any{"sessionId": "s1", "update": update}})
	}
	request := func(id int, params map[string]any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/request_permission", "params": params})
	}

	for {
		var msg rpcMsg
		if err := dec.Decode(&msg); err != nil {
			return
		}
		switch msg.Method {
		case "initialize":
			var p struct {
				ProtocolVersion int `json:"protocolVersion"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.ProtocolVersion != 1 {
				t.Errorf("client sent protocolVersion %d", p.ProtocolVersion)
			}
			reply(msg.ID, map[string]any{"protocolVersion": 1})

		case "session/new":
			var p struct {
				Cwd        string      `json:"cwd"`
				MCPServers []MCPServer `json:"mcpServers"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if len(p.MCPServers) != 1 || p.MCPServers[0].Name != "ihme" {
				t.Errorf("mcpServers not injected: %+v", p.MCPServers)
			}
			if p.MCPServers[0].Env == nil {
				t.Error("env must serialize as [], not null")
			}
			reply(msg.ID, map[string]any{"sessionId": "s1"})

		case "session/prompt":
			notify(map[string]any{"sessionUpdate": "agent_thought_chunk",
				"content": map[string]any{"type": "text", "text": "thinking…"}})
			notify(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1",
				"title": "reserve_address", "kind": "other", "status": "pending",
				"rawInput": map[string]any{"address": "glen_arbor@icloud.com", "rationale": "a place on a map"}})

			// First ask: the harness should approve.
			request(100, map[string]any{"sessionId": "s1",
				"toolCall": map[string]any{"toolCallId": "t1", "title": "reserve_address",
					"rawInput": map[string]any{"address": "glen_arbor@icloud.com"}},
				"options": []map[string]any{
					{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
					{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
				}})
			var resp rpcMsg
			if err := dec.Decode(&resp); err != nil {
				t.Errorf("no permission response: %v", err)
				return
			}
			var out struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			_ = json.Unmarshal(resp.Result, &out)
			if out.Outcome.Outcome != "selected" || out.Outcome.OptionID != "allow" {
				t.Errorf("first permission = %+v, want selected/allow", out.Outcome)
			}

			// Second ask: the harness answers "" -> cancelled.
			request(101, map[string]any{"sessionId": "s1",
				"toolCall": map[string]any{"toolCallId": "t2", "title": "deactivate_address"},
				"options": []map[string]any{
					{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
				}})
			if err := dec.Decode(&resp); err != nil {
				t.Errorf("no second permission response: %v", err)
				return
			}
			_ = json.Unmarshal(resp.Result, &out)
			if out.Outcome.Outcome != "cancelled" {
				t.Errorf("second permission = %+v, want cancelled", out.Outcome)
			}

			notify(map[string]any{"sessionUpdate": "agent_message_chunk",
				"content": map[string]any{"type": "text", "text": "done: **glen_arbor@icloud.com**"}})
			reply(msg.ID, map[string]any{"stopReason": "end_turn"})
		}
	}
}

func TestClientConversation(t *testing.T) {
	agentIn, clientOut := io.Pipe()  // client -> agent
	clientIn, agentOut := io.Pipe() // agent -> client
	go fakeAgent(t, agentIn, agentOut)

	c := Attach(clientIn, clientOut)
	var updates []Update
	c.OnUpdate = func(u Update) { updates = append(updates, u) }
	asked := 0
	c.OnPermission = func(req PermissionRequest) string {
		asked++
		if asked == 1 {
			if req.ToolCall.Title != "reserve_address" || len(req.Options) != 2 {
				t.Errorf("unexpected permission request: %+v", req)
			}
			if !json.Valid(req.ToolCall.RawInput) {
				t.Error("rawInput lost in transit")
			}
			return "allow"
		}
		return "" // decline -> cancelled outcome
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	sid, err := c.NewSession(ctx, "/tmp", []MCPServer{{Name: "ihme", Command: "/bin/ihme", Args: []string{"mcp"}, Env: []EnvVar{}}})
	if err != nil || sid != "s1" {
		t.Fatalf("session/new = %q, %v", sid, err)
	}
	stop, err := c.Prompt(ctx, sid, "reserve for github")
	if err != nil || stop != "end_turn" {
		t.Fatalf("prompt = %q, %v", stop, err)
	}

	if asked != 2 {
		t.Errorf("permission asked %d times, want 2", asked)
	}
	var kinds []string
	text := ""
	for _, u := range updates {
		kinds = append(kinds, u.Kind)
		if u.Kind == "agent_message_chunk" {
			text += u.Text
		}
		if u.Kind == "tool_call" && !json.Valid(u.RawInput) {
			t.Error("tool_call rawInput lost")
		}
	}
	want := []string{"agent_thought_chunk", "tool_call", "agent_message_chunk"}
	if len(kinds) != len(want) {
		t.Fatalf("updates = %v, want kinds %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("update[%d] = %s, want %s (order must be delivery order)", i, kinds[i], want[i])
		}
	}
	if text != "done: **glen_arbor@icloud.com**" {
		t.Errorf("message text = %q", text)
	}
}

func TestClientVersionMismatch(t *testing.T) {
	agentIn, clientOut := io.Pipe()
	clientIn, agentOut := io.Pipe()
	go func() {
		dec := json.NewDecoder(agentIn)
		enc := json.NewEncoder(agentOut)
		var msg rpcMsg
		if err := dec.Decode(&msg); err != nil {
			return
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID,
			"result": map[string]any{"protocolVersion": 2}})
	}()
	c := Attach(clientIn, clientOut)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx); err == nil {
		t.Fatal("v2-only agent must be refused, not half-spoken to")
	}
}
