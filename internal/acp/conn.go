// Package acp is a minimal Agent Client Protocol client — the
// harness side. ihme spawns a full coding agent (claude-code and
// codex via their ACP adapters, opencode natively) as a subprocess
// and drives it: task in via session/prompt, streamed progress back
// via session/update, consent routed to the user through
// session/request_permission.
//
// This is a deliberate hand-rolled subset of protocol version 1
// (initialize, session/new with MCP server injection,
// session/prompt, session/cancel), mirroring the agentkit stance:
// small enough to audit in one sitting, no dependency on a pre-1.0
// SDK tracking a moving schema. Anything the guest sends that we do
// not model is ignored, not an error.
package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// rpcMsg is one JSON-RPC 2.0 message in either direction. Method
// set means request/notification (ID distinguishes); otherwise it
// is a response to one of our requests.
type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a protocol-level failure from the agent (auth
// required, unknown session, internal error). It is the error type
// callers should errors.As for.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("agent error %d: %s", e.Code, e.Message) }

// conn is the bidirectional peer: outgoing requests with response
// matching, incoming requests dispatched to onRequest, incoming
// notifications to onNotify.
type conn struct {
	w     io.Writer
	wMu   sync.Mutex
	next  atomic.Int64
	pend  map[int64]chan rpcMsg
	pendM sync.Mutex

	// onNotify runs synchronously in the read loop: session/update
	// ordering IS the transcript, so it must not be re-ordered by
	// goroutine scheduling. onRequest runs in its own goroutine: it
	// blocks on the user (consent), and the agent waits for the
	// response anyway.
	onNotify  func(method string, params json.RawMessage)
	onRequest func(method string, params json.RawMessage) (any, *RPCError)

	readDone chan struct{}
	readErr  error
}

func newConn(w io.Writer, onNotify func(string, json.RawMessage), onRequest func(string, json.RawMessage) (any, *RPCError)) *conn {
	return &conn{
		w: w, pend: map[int64]chan rpcMsg{},
		onNotify: onNotify, onRequest: onRequest,
		readDone: make(chan struct{}),
	}
}

// readLoop consumes newline-delimited JSON until r closes. On exit
// every pending call is failed so callers never hang on a dead
// agent.
func (c *conn) readLoop(r io.Reader) {
	dec := json.NewDecoder(r)
	for {
		var msg rpcMsg
		if err := dec.Decode(&msg); err != nil {
			if err != io.EOF {
				c.readErr = err
			}
			break
		}
		switch {
		case msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null":
			go c.serveRequest(msg)
		case msg.Method != "":
			if c.onNotify != nil {
				c.onNotify(msg.Method, msg.Params)
			}
		default:
			c.settle(msg)
		}
	}
	c.pendM.Lock()
	for id, ch := range c.pend {
		close(ch)
		delete(c.pend, id)
	}
	c.pendM.Unlock()
	close(c.readDone)
}

func (c *conn) serveRequest(msg rpcMsg) {
	var result any
	var rpcErr *RPCError
	if c.onRequest == nil {
		rpcErr = &RPCError{Code: -32601, Message: "unsupported"}
	} else {
		result, rpcErr = c.onRequest(msg.Method, msg.Params)
	}
	out := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
	if rpcErr != nil {
		out["error"] = rpcErr
	} else {
		out["result"] = result
	}
	_ = c.write(out)
}

func (c *conn) settle(msg rpcMsg) {
	var id int64
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return // not an id we issued
	}
	c.pendM.Lock()
	ch := c.pend[id]
	delete(c.pend, id)
	c.pendM.Unlock()
	if ch != nil {
		ch <- msg
		close(ch)
	}
}

// call sends one request and waits for its response or ctx.
func (c *conn) call(ctx context.Context, method string, params, result any) error {
	id := c.next.Add(1)
	ch := make(chan rpcMsg, 1)
	c.pendM.Lock()
	c.pend[id] = ch
	c.pendM.Unlock()

	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.pendM.Lock()
		delete(c.pend, id)
		c.pendM.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return fmt.Errorf("%s: agent exited before responding (read error: %v)", method, c.readErr)
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %w", method, msg.Error)
		}
		if result != nil {
			if err := json.Unmarshal(msg.Result, result); err != nil {
				return fmt.Errorf("%s: bad result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.pendM.Lock()
		delete(c.pend, id)
		c.pendM.Unlock()
		return ctx.Err()
	}
}

func (c *conn) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *conn) write(v any) error {
	c.wMu.Lock()
	defer c.wMu.Unlock()
	enc := json.NewEncoder(c.w)
	return enc.Encode(v)
}
