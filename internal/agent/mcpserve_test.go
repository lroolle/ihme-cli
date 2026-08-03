package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

// mcpPipe runs ServeMCP over in-memory pipes and returns a
// request/response round-tripper. The nil app.Service is safe for
// every tool exercised here: they either answer canned (auth_status),
// fail validation before touching the service (reserve_address with
// a hollow rationale), or are denied by the gate first.
func mcpPipe(t *testing.T, grant GrantMode) func(msg string) map[string]any {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- ServeMCP(context.Background(), nil, "user@icloud.com", "test", grant, "", inR, outW)
	}()
	t.Cleanup(func() {
		inW.Close()
		if err := <-done; err != nil {
			t.Errorf("ServeMCP: %v", err)
		}
	})
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return func(msg string) map[string]any {
		if _, err := io.WriteString(inW, msg+"\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
		if !sc.Scan() {
			t.Fatalf("no response to %s: %v", msg, sc.Err())
		}
		var out map[string]any
		if err := json.Unmarshal(sc.Bytes(), &out); err != nil {
			t.Fatalf("bad response %q: %v", sc.Text(), err)
		}
		return out
	}
}

func TestMCPInitializeAndList(t *testing.T) {
	rpc := mcpPipe(t, GrantAuto)

	init := rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	result, _ := init["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("known client version must be echoed, got %v", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "ihme" {
		t.Errorf("serverInfo = %v", info)
	}

	// Unknown client versions get our latest, not an error.
	init2 := rpc(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"9999-01-01"}}`)
	if r, _ := init2["result"].(map[string]any); r["protocolVersion"] != mcpLatest {
		t.Errorf("unknown version -> %v, want %s", r["protocolVersion"], mcpLatest)
	}

	list := rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	toolsRes, _ := list["result"].(map[string]any)
	names := map[string]bool{}
	tools, _ := toolsRes["tools"].([]any)
	for _, tl := range tools {
		m := tl.(map[string]any)
		names[m["name"].(string)] = true
		if m["inputSchema"] == nil {
			t.Errorf("tool %v has no inputSchema", m["name"])
		}
	}
	for _, want := range []string{"auth_status", "search_addresses", "generate_candidates", "refresh_candidates", "reserve_address", "deactivate_address", "edit_note", "recall_memory", "remember"} {
		if !names[want] {
			t.Errorf("tools/list missing %s (got %v)", want, names)
		}
	}
	// No terminal on a protocol channel: ask_user must not be served.
	if names["ask_user"] {
		t.Error("ask_user must not be exposed over MCP")
	}
}

func TestMCPToolCall(t *testing.T) {
	rpc := mcpPipe(t, GrantAuto)
	rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)

	call := rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"auth_status","arguments":{}}}`)
	text, isErr := toolResult(t, call)
	if isErr || !strings.Contains(text, "user@icloud.com") {
		t.Errorf("auth_status = %q (isError=%v)", text, isErr)
	}

	// The rationale floor is in-process physics — it holds with no
	// gate and no kernel, exactly what an unattended guest hits.
	short := rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"reserve_address","arguments":{"address":"a@icloud.com","label":"x","rationale":"nice","rejected":[]}}}`)
	text, isErr = toolResult(t, short)
	if !isErr || !strings.Contains(text, "rationale") {
		t.Errorf("hollow rationale must fail as a tool error, got %q (isError=%v)", text, isErr)
	}

	unknown := rpc(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	if unknown["error"] == nil {
		t.Errorf("unknown tool must be a protocol error, got %v", unknown)
	}

	missing := rpc(`{"jsonrpc":"2.0","id":5,"method":"bogus/method"}`)
	if missing["error"] == nil {
		t.Errorf("unknown method must be a protocol error, got %v", missing)
	}
}

// TestMCPGateDefault proves the safety posture this server ships
// with: GrantAsk and no consent channel means mutations outside the
// run's scope are DENIED as adaptable tool errors — verified live
// that the guest's own permission layer never asks, so this gate is
// the only one there is.
func TestMCPGateDefault(t *testing.T) {
	rpc := mcpPipe(t, GrantAsk)
	rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)

	deact := rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"deactivate_address","arguments":{"ref":"anything"}}}`)
	text, isErr := toolResult(t, deact)
	if !isErr || !strings.Contains(text, "denied") {
		t.Errorf("unattended deactivate must be denied, got %q (isError=%v)", text, isErr)
	}

	// The gate bounces a verdict-less reserve back to the model
	// before any consent question exists to ask.
	reserve := rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"reserve_address","arguments":{"address":"a@icloud.com","label":"x","rationale":"eh","rejected":[]}}}`)
	text, isErr = toolResult(t, reserve)
	if !isErr || !strings.Contains(text, "rationale") {
		t.Errorf("verdict-less reserve must bounce, got %q (isError=%v)", text, isErr)
	}

	// Reads stay free under the gate.
	auth := rpc(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"auth_status","arguments":{}}}`)
	if text, isErr := toolResult(t, auth); isErr {
		t.Errorf("auth_status must pass the gate, got %q", text)
	}
}

func toolResult(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("no result in %v", resp)
	}
	isErr, _ := result["isError"].(bool)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in %v", result)
	}
	first, _ := content[0].(map[string]any)
	return fmt.Sprintf("%v", first["text"]), isErr
}
