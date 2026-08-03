package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// protocolVersion is the ACP major this client targets. v2 is a
// draft with a different turn lifecycle; ignore it until it ships.
const protocolVersion = 1

// Update is the flattened view of one session/update notification —
// exactly the fields the renderer and consent card need.
type Update struct {
	Kind string // agent_message_chunk | agent_thought_chunk | tool_call | tool_call_update | plan | ...

	Text string // chunk text (empty for non-text content)

	ToolCallID string
	Title      string
	Tool       string // ACP tool kind hint: read, edit, execute, think, fetch, other…
	Status     string // pending | in_progress | completed | failed
	RawInput   json.RawMessage
	RawOutput  json.RawMessage

	Plan []PlanEntry
}

type PlanEntry struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// PermissionRequest is the agent asking the user to approve one
// tool call. Options are agent-defined; the client only picks.
type PermissionRequest struct {
	SessionID string
	ToolCall  Update
	Options   []PermissionOption
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // allow_once | allow_always | reject_once | reject_always
}

// MCPServer is a tool server the client hands the agent at session
// start (stdio transport — the one every ACP agent must support).
type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []EnvVar `json:"env"`
}

type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// InitializeResult reports what the agent offered; kept raw-ish for
// the caller to log or ignore.
type InitializeResult struct {
	ProtocolVersion int             `json:"protocolVersion"`
	AgentInfo       json.RawMessage `json:"agentInfo,omitempty"`
	AuthMethods     json.RawMessage `json:"authMethods,omitempty"`
}

// Client drives one ACP agent process.
//
// OnUpdate and OnPermission must be set before Initialize.
// OnUpdate runs on the read loop — transcript order is delivery
// order. OnPermission runs on its own goroutine and may block on
// the user; return the chosen optionId, or "" to answer cancelled.
type Client struct {
	OnUpdate     func(Update)
	OnPermission func(PermissionRequest) string

	conn  *conn
	cmd   *exec.Cmd
	stdin io.Closer
}

// Spawn starts an agent process and attaches a client to its stdio.
// The process inherits the parent environment (that is where
// claude/codex subscription auth lives). stderr is the agent's log
// channel — hand it os.Stderr or io.Discard.
func Spawn(ctx context.Context, argv []string, stderr io.Writer) (*Client, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty agent command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}
	c := &Client{cmd: cmd, stdin: stdin}
	c.attach(stdout, stdin)
	return c, nil
}

// Attach wires a client over existing streams (tests, or an agent
// managed by the caller).
func Attach(r io.Reader, w io.Writer) *Client {
	c := &Client{}
	c.attach(r, w)
	return c
}

func (c *Client) attach(r io.Reader, w io.Writer) {
	c.conn = newConn(w, c.handleNotify, c.handleRequest)
	go c.conn.readLoop(r)
}

// Close ends the conversation: stdin closes (agents exit on EOF),
// then the process gets a grace period before the context kill
// takes it.
func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		_ = c.cmd.Process.Kill()
		return <-done
	}
}

func (c *Client) Initialize(ctx context.Context) (InitializeResult, error) {
	var res InitializeResult
	err := c.conn.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"clientCapabilities": map[string]any{
			// No fs or terminal delegation: the guest works through
			// the injected MCP tools, not through our filesystem.
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
		"clientInfo": map[string]any{"name": "ihme", "version": "embedded"},
	}, &res)
	if err != nil {
		return res, err
	}
	if res.ProtocolVersion != protocolVersion {
		return res, fmt.Errorf("agent speaks ACP v%d, this client speaks v%d", res.ProtocolVersion, protocolVersion)
	}
	return res, nil
}

func (c *Client) NewSession(ctx context.Context, cwd string, servers []MCPServer) (string, error) {
	if servers == nil {
		servers = []MCPServer{}
	}
	var res struct {
		SessionID string `json:"sessionId"`
	}
	err := c.conn.call(ctx, "session/new", map[string]any{
		"cwd": cwd, "mcpServers": servers,
	}, &res)
	return res.SessionID, err
}

// Prompt runs one turn and blocks until the agent finishes it.
// Cancelling ctx sends session/cancel and still waits (briefly) for
// the agent to wind down with stopReason "cancelled", per spec.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (string, error) {
	var res struct {
		StopReason string `json:"stopReason"`
	}
	inner, abort := context.WithCancel(context.Background())
	defer abort()
	done := make(chan error, 1)
	go func() {
		done <- c.conn.call(inner, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    []map[string]any{{"type": "text", "text": text}},
		}, &res)
	}()
	select {
	case err := <-done:
		return res.StopReason, err
	case <-ctx.Done():
		_ = c.conn.notify("session/cancel", map[string]any{"sessionId": sessionID})
		select {
		case err := <-done:
			if err != nil {
				return "", err
			}
			return res.StopReason, nil
		case <-time.After(5 * time.Second):
			return "", ctx.Err()
		}
	}
}

func (c *Client) handleNotify(method string, params json.RawMessage) {
	if method != "session/update" || c.OnUpdate == nil {
		return
	}
	if u, ok := parseUpdate(params); ok {
		c.OnUpdate(u)
	}
}

func (c *Client) handleRequest(method string, params json.RawMessage) (any, *RPCError) {
	if method != "session/request_permission" {
		return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("client does not support %s", method)}
	}
	var p struct {
		SessionID string          `json:"sessionId"`
		ToolCall  json.RawMessage `json:"toolCall"`
		Options   []PermissionOption `json:"options"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	req := PermissionRequest{SessionID: p.SessionID, Options: p.Options}
	req.ToolCall = parseToolCall(p.ToolCall)
	choice := ""
	if c.OnPermission != nil {
		choice = c.OnPermission(req)
	}
	if choice == "" {
		return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil
	}
	return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": choice}}, nil
}

// rawUpdate is the wire shape shared by session/update payloads and
// the toolCall embedded in permission requests.
type rawUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	RawInput      json.RawMessage `json:"rawInput"`
	RawOutput     json.RawMessage `json:"rawOutput"`
	Entries       []PlanEntry     `json:"entries"`
}

func parseUpdate(params json.RawMessage) (Update, bool) {
	var p struct {
		Update rawUpdate `json:"update"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return Update{}, false
	}
	return flatten(p.Update), true
}

func parseToolCall(raw json.RawMessage) Update {
	var u rawUpdate
	_ = json.Unmarshal(raw, &u)
	out := flatten(u)
	out.Kind = "tool_call"
	return out
}

func flatten(r rawUpdate) Update {
	u := Update{
		Kind:       r.SessionUpdate,
		ToolCallID: r.ToolCallID,
		Title:      r.Title,
		Tool:       r.Kind,
		Status:     r.Status,
		RawInput:   r.RawInput,
		RawOutput:  r.RawOutput,
		Plan:       r.Entries,
	}
	// Chunk content is a single ContentBlock; only text renders.
	if len(r.Content) > 0 && r.Content[0] == '{' {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(r.Content, &block) == nil && block.Type == "text" {
			u.Text = block.Text
		}
	}
	return u
}
