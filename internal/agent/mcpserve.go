package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// ServeMCP speaks the Model Context Protocol over in/out (stdio,
// newline-delimited JSON-RPC 2.0), exposing the same in-process
// tools the embedded agent uses. This is the plumbing half of
// `ihme agent --via <agent>`: the guest agent (claude-code, codex,
// opencode) is handed this server in session/new and calls the
// tools through it.
//
// The scoped-consent gate from the embedded agent runs HERE, in
// front of every call — the guest's own permission layer cannot be
// trusted with it (verified live: claude-agent-acp executes
// mutating MCP tools without any session/request_permission). The
// server's stdio is the protocol channel, so consentSock names the
// unix socket where the harness client renders our cards; without
// it, mutations outside the run's scope are denied with a reason
// the model can adapt to, same as a non-interactive embedded run.
// GrantAuto skips the gate; the tool layer's own physics (rationale
// floor, caps, journaling) apply on every path.
//
// ask_user is absent (asker nil in tools): a guest that needs the
// user talks to them through its own turn, not through a tool with
// no terminal.
func ServeMCP(ctx context.Context, svc *app.Service, appleID, version string, grant GrantMode, consentSock string, in io.Reader, out io.Writer) error {
	st := newRunState("")
	var ask asker
	if consentSock != "" {
		ask = socketAsker(consentSock)
	}
	if grant == "" {
		grant = GrantAsk
	}
	srv := &mcpServer{
		version: version,
		tools:   tools(svc, st, appleID, nil, memory.Open()),
		gate:    gate(grant, st, ask),
		enc:     json.NewEncoder(out),
	}
	return srv.serve(ctx, in)
}

// mcpVersions are the protocol revisions this server knows. An
// unknown client version gets the newest — per spec the client then
// decides whether to proceed.
var mcpVersions = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

const mcpLatest = "2025-06-18"

type mcpServer struct {
	version string
	tools   []agentkit.Tool
	gate    agentkit.Gate // nil = GrantAuto
	enc     *json.Encoder
	mu      sync.Mutex // one writer: responses come from the serve loop only
}

// rpcIn is one incoming JSON-RPC message. ID distinguishes requests
// (respond) from notifications (don't): absent means notification.
type rpcIn struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcParseError     = -32700
)

// serve reads one message per line and handles it synchronously —
// sequential tools are the kernel's stance and this server keeps it.
func (s *mcpServer) serve(ctx context.Context, in io.Reader) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcIn
		if err := json.Unmarshal(line, &msg); err != nil {
			s.reply(nil, nil, &rpcError{Code: rpcParseError, Message: err.Error()})
			continue
		}
		s.handle(ctx, msg)
	}
	return sc.Err()
}

func (s *mcpServer) handle(ctx context.Context, msg rpcIn) {
	isRequest := len(msg.ID) > 0 && string(msg.ID) != "null"
	switch msg.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		v := p.ProtocolVersion
		if !mcpVersions[v] {
			v = mcpLatest
		}
		s.reply(msg.ID, map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "ihme", "version": s.version},
		}, nil)

	case "notifications/initialized", "notifications/cancelled":
		// Nothing to do; notifications never get responses.

	case "ping":
		s.reply(msg.ID, map[string]any{}, nil)

	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			list = append(list, map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"inputSchema": t.Schema(),
			})
		}
		s.reply(msg.ID, map[string]any{"tools": list}, nil)

	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			s.reply(msg.ID, nil, &rpcError{Code: rpcInvalidParams, Message: err.Error()})
			return
		}
		tool := s.lookup(p.Name)
		if tool == nil {
			s.reply(msg.ID, nil, &rpcError{Code: rpcInvalidParams, Message: fmt.Sprintf("unknown tool %q", p.Name)})
			return
		}
		args := p.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		if s.gate != nil {
			decision := s.gate(ctx, agentkit.GateRequest{Call: agentkit.ToolCall{Name: p.Name, Args: args}})
			if !decision.Allowed {
				// A denial is a tool result the model can adapt to —
				// including the user's typed redirect riding the
				// reason — exactly as the kernel feeds it back.
				s.reply(msg.ID, toolText("denied: "+decision.Reason, true), nil)
				return
			}
		}
		result, err := tool.Execute(ctx, args)
		if err != nil {
			// Execution failure is a tool result the model can adapt
			// to (isError), mirroring how the kernel feeds denials and
			// errors back — never a dead protocol error.
			s.reply(msg.ID, toolText(err.Error(), true), nil)
			return
		}
		s.reply(msg.ID, toolText(string(result), false), nil)

	default:
		if isRequest {
			s.reply(msg.ID, nil, &rpcError{Code: rpcMethodNotFound, Message: fmt.Sprintf("method %q not supported", msg.Method)})
		}
	}
}

func (s *mcpServer) lookup(name string) agentkit.Tool {
	for _, t := range s.tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

func toolText(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isErr,
	}
}

func (s *mcpServer) reply(id json.RawMessage, result any, rpcErr *rpcError) {
	if id == nil && rpcErr == nil {
		return
	}
	if id == nil {
		id = json.RawMessage("null")
	}
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		out["error"] = rpcErr
	} else {
		out["result"] = result
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A write failure means the client is gone; the read loop will
	// hit EOF next and end the serve.
	_ = s.enc.Encode(out)
}
