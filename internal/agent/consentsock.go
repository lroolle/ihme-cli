package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// The consent socket carries ihme's consent cards across the
// process boundary that --via creates. The MCP server child cannot
// prompt anyone — its stdio IS the protocol channel — and the guest
// agent's own permission layer cannot be trusted with our consent
// invariant (verified live: claude-agent-acp executes mutating MCP
// tools without any session/request_permission). So the gate runs
// where the tools run, and only the QUESTION travels: the server
// dials the socket with a userPrompt, the harness client renders it
// on the terminal it owns and returns the user's raw answer. Both
// ends are this same binary.
//
// Wire shape: one JSON line each way per connection —
// {"prompt": userPrompt} in, {"answer": "..."} or {"error": "..."} out.

type consentReq struct {
	Prompt userPrompt `json:"prompt"`
}

type consentResp struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

// socketAsker is the MCP-server side: an asker that forwards every
// prompt to the harness client over the unix socket.
func socketAsker(path string) asker {
	return func(ctx context.Context, prompt userPrompt) (string, error) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", path)
		if err != nil {
			return "", fmt.Errorf("consent channel gone: %w", err)
		}
		defer conn.Close()
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}
		if err := json.NewEncoder(conn).Encode(consentReq{Prompt: prompt}); err != nil {
			return "", err
		}
		var resp consentResp
		if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
			return "", fmt.Errorf("no consent answer: %w", err)
		}
		if resp.Error != "" {
			return "", fmt.Errorf("consent: %s", resp.Error)
		}
		return resp.Answer, nil
	}
}

// serveConsentSocket is the harness-client side: answer consent
// requests with the session's input authority until the listener
// closes. Sequential on purpose — one user, one card at a time.
func serveConsentSocket(l net.Listener, ask asker) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return // listener closed: run is over
		}
		handleConsentConn(conn, ask)
	}
}

func handleConsentConn(conn net.Conn, ask asker) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
	var req consentReq
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
		return
	}
	var resp consentResp
	answer, err := ask(context.Background(), req.Prompt)
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Answer = answer
	}
	_ = json.NewEncoder(conn).Encode(resp)
}
