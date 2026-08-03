package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lroolle/ihme-cli/internal/acp"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/skill"
	"golang.org/x/term"
)

// RunVia harnesses a full coding agent as the model provider:
// ihme is the ACP client, the guest (claude-code/codex through
// their ACP adapters, opencode natively) owns the loop, and the
// HME operations come back to this binary through the MCP server
// injected at session/new (`ihme mcp`). Same procedure, same tool
// physics (caps, rationale floor, journaling — they live in the
// tool layer, not the loop); consent rides the guest's
// session/request_permission into the same card the embedded
// agent shows.
//
// The guest's subscription auth is inherited from the parent
// environment — that is the point: `--via codex` runs on a ChatGPT
// plan, `--via claude` on a Claude plan, no BYOK key.

// viaPreface adapts the shared procedure to a guest that has its
// own tools: HME work is confined to the injected MCP server so
// nothing bypasses the caps and journaling.
const viaPreface = `You are the agent provider harnessed by the ihme CLI. All Hide My
Email operations MUST go through the MCP tools of the server named
"ihme" (your runtime may prefix the names, e.g.
mcp__ihme__reserve_address). Never run the ihme shell command,
never touch files, never use your own tools for HME work — the
ihme tools enforce rate caps and journal reservations automatically.
The procedure's "Embedded agent" execution adapter applies.
ask_user is not available: decide within the task and state your
assumptions in the final summary.`

// ViaResult is the --json shape of one harnessed run.
type ViaResult struct {
	Via        string       `json:"via"`
	StopReason string       `json:"stopReason"`
	Reserved   *addressView `json:"reserved"`
	Rationale  string       `json:"rationale,omitempty"`
	Rejected   []Rejection  `json:"rejected,omitempty"`
	Summary    string       `json:"summary"`
}

// resolveVia maps a guest name to its spawn command: an installed
// binary when present, the npx-fetched adapter otherwise. opencode
// speaks ACP natively, so only its own binary will do.
func resolveVia(name string) ([]string, error) {
	bin := func(names ...string) string {
		for _, n := range names {
			if p, err := exec.LookPath(n); err == nil {
				return p
			}
		}
		return ""
	}
	switch name {
	case "codex":
		if p := bin("codex-acp"); p != "" {
			return []string{p}, nil
		}
		return []string{"npx", "-y", "@agentclientprotocol/codex-acp"}, nil
	case "claude":
		if p := bin("claude-agent-acp", "claude-code-acp"); p != "" {
			return []string{p}, nil
		}
		return []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp"}, nil
	case "opencode":
		if p := bin("opencode"); p != "" {
			return []string{p, "acp"}, nil
		}
		return nil, fmt.Errorf("opencode not found on PATH — install it from https://opencode.ai")
	default:
		return nil, fmt.Errorf("unknown agent %q — use codex, claude, or opencode", name)
	}
}

// RunVia executes one task through a harnessed guest agent. The
// caller has already authenticated (fail fast, and the freshly
// validated session lets the MCP child start inside the TTL).
func RunVia(ctx context.Context, task, via string, grant GrantMode, jsonOut, verbose bool) (*ViaResult, error) {
	argv, err := resolveVia(via)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locating own binary for the mcp server: %w", err)
	}

	textOut := io.Writer(os.Stderr)
	if !jsonOut {
		textOut = os.Stdout
	}
	meta := io.Writer(os.Stderr)
	agentLog := io.Discard
	if verbose {
		agentLog = os.Stderr
	}

	client, err := acp.Spawn(ctx, argv, agentLog)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	res := &ViaResult{Via: via}
	v := &viaRun{
		res: res, grant: grant, ask: stdinAsker(),
		meta: meta, titles: map[string]string{},
	}

	// The consent gate runs inside the MCP child (the guest's own
	// permission layer is not trusted with it); when this session can
	// ask, open the socket that carries its cards to our terminal.
	mcpArgs := []string{"mcp"}
	if grant == GrantAuto {
		mcpArgs = append(mcpArgs, "--grant", "auto")
	} else if v.ask != nil {
		sock := filepath.Join(os.TempDir(), fmt.Sprintf("ihme-consent-%d.sock", os.Getpid()))
		_ = os.Remove(sock)
		l, err := net.Listen("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("opening consent socket: %w", err)
		}
		defer l.Close()
		defer os.Remove(sock)
		go serveConsentSocket(l, v.ask)
		mcpArgs = append(mcpArgs, "--consent-socket", sock)
	}
	v.text = mdPassthrough{w: textOut}
	if f, ok := textOut.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		v.text = newMDANSI(textOut)
	}
	client.OnUpdate = v.update
	client.OnPermission = v.permission

	if _, err := client.Initialize(ctx); err != nil {
		return nil, viaHintErr(via, err)
	}
	cwd, _ := os.Getwd()
	servers := []acp.MCPServer{{
		Name: "ihme", Command: exe, Args: mcpArgs, Env: mcpEnv(),
	}}
	sid, err := client.NewSession(ctx, cwd, servers)
	if err != nil {
		return nil, viaHintErr(via, err)
	}
	fmt.Fprintf(meta, "Agent: %s (%s) · tools: ihme mcp\n", via, strings.Join(argv, " "))

	turn := viaInvocation(task)
	for {
		stop, err := client.Prompt(ctx, sid, turn)
		if err != nil {
			return nil, viaHintErr(via, err)
		}
		_ = v.text.Close()
		res.StopReason = stop
		// A typed consent reply is direction, not a verdict — carry
		// it into the next turn exactly like the embedded gate does
		// through denial reasons.
		if redirect := v.takeRedirect(); redirect != "" {
			turn = fmt.Sprintf("The user rejected that action and redirected you: %q. "+
				"This is direction, not rejection of the task — adapt and continue within scope.", redirect)
			continue
		}
		break
	}
	res.Summary = v.summary.String()
	fmt.Fprintf(meta, "\n[%s]\n", res.StopReason)
	return res, nil
}

// viaInvocation composes the guest's task turn: harness preface,
// the shared procedure, the task, and the memory continuity block —
// the same layering the embedded agent gets.
func viaInvocation(task string) string {
	if ctx := memoryContext(memory.Open()); ctx != "" {
		task = task + "\n\n" + ctx
	}
	inv := agentkit.Skill{Name: "ihme", Instructions: skill.Instructions()}.Invocation(task)
	return viaPreface + "\n\n" + inv.Text
}

// mcpEnv forwards the session-path override to the MCP child when
// set; everything else it needs is ambient.
func mcpEnv() []acp.EnvVar {
	env := []acp.EnvVar{}
	if p := os.Getenv("IHME_SESSION_PATH"); p != "" {
		env = append(env, acp.EnvVar{Name: "IHME_SESSION_PATH", Value: p})
	}
	return env
}

// viaHintErr decorates the guest failures a user can actually fix.
func viaHintErr(via string, err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "auth") || strings.Contains(msg, "login") || strings.Contains(msg, "unauthorized") {
		login := map[string]string{"codex": "codex login", "claude": "claude (then /login)", "opencode": "opencode auth login"}[via]
		return fmt.Errorf("%w\n\nThe %s agent is not signed in — run `%s` first; --via uses its subscription auth, no API key needed", err, via, login)
	}
	return err
}

// viaRun is the render + consent state of one harnessed run.
type viaRun struct {
	res   *ViaResult
	grant GrantMode
	ask   asker
	text  mdWriter
	meta  io.Writer

	mu       sync.Mutex
	titles   map[string]string // toolCallId -> title (updates may omit it)
	redirect string
	summary  strings.Builder
	thinking bool
}

// update renders one session/update. It runs on the read loop, so
// transcript order is delivery order.
func (v *viaRun) update(u acp.Update) {
	switch u.Kind {
	case "agent_message_chunk":
		v.endThinking()
		v.summary.WriteString(u.Text)
		_ = v.text.WriteText(u.Text)
	case "agent_thought_chunk":
		v.thinking = true
		fmt.Fprintf(v.meta, "\x1b[2m%s\x1b[0m", u.Text)
	case "tool_call":
		v.endThinking()
		v.mu.Lock()
		v.titles[u.ToolCallID] = u.Title
		v.mu.Unlock()
		fmt.Fprintf(v.meta, "\n-> %s %s\n", u.Title, compact(u.RawInput))
	case "tool_call_update":
		v.mu.Lock()
		title := v.titles[u.ToolCallID]
		if u.Title != "" {
			title, v.titles[u.ToolCallID] = u.Title, u.Title
		}
		v.mu.Unlock()
		switch u.Status {
		case "completed":
			v.captureReserve(title, u)
			fmt.Fprintf(v.meta, "<- %s %s\n", title, compact(u.RawOutput))
		case "failed":
			fmt.Fprintf(v.meta, "<- %s: failed %s\n", title, compact(u.RawOutput))
		}
	case "plan":
		for _, e := range u.Plan {
			if e.Status == "in_progress" {
				fmt.Fprintf(v.meta, "\x1b[2m· %s\x1b[0m\n", e.Content)
			}
		}
	}
}

func (v *viaRun) endThinking() {
	if v.thinking {
		fmt.Fprintln(v.meta)
		v.thinking = false
	}
}

// captureReserve lifts the reservation out of a completed
// reserve_address call so --json reports it structurally, whatever
// prose the guest writes. rawOutput arrives adapter-mangled: try
// the MCP envelope first, the bare result second.
func (v *viaRun) captureReserve(title string, u acp.Update) {
	if !isIhmeTool(title, "reserve_address") {
		return
	}
	var args struct {
		Rationale string      `json:"rationale"`
		Rejected  []Rejection `json:"rejected"`
	}
	_ = json.Unmarshal(u.RawInput, &args)
	var out struct {
		Address *addressView `json:"address"`
	}
	payload := u.RawOutput
	var envelope struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(payload, &envelope) == nil && len(envelope.Content) > 0 && envelope.Content[0].Text != "" {
		payload = json.RawMessage(envelope.Content[0].Text)
	}
	if json.Unmarshal(payload, &out) == nil && out.Address != nil {
		v.mu.Lock()
		v.res.Reserved = out.Address
		v.res.Rationale = args.Rationale
		v.res.Rejected = args.Rejected
		v.mu.Unlock()
	}
}

// isIhmeTool matches a guest-side tool label against one of our MCP
// tool names, tolerating runtime prefixes (mcp__ihme__reserve_address,
// "ihme: reserve_address", …).
func isIhmeTool(title, tool string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, tool)
}

// permission answers one session/request_permission. Runs on its
// own goroutine; the guest is blocked on the answer.
func (v *viaRun) permission(req acp.PermissionRequest) string {
	// Our own tools are governed by the gate inside the MCP child —
	// the card the user sees comes from there, over the consent
	// socket. Approving the guest-level ask here prevents the same
	// action carding twice on guests that do forward MCP calls.
	if strings.Contains(strings.ToLower(req.ToolCall.Title), "ihme") {
		return pickOption(req.Options, "allow_once", "allow_always")
	}
	if v.grant == GrantAuto {
		return pickOption(req.Options, "allow_once", "allow_always")
	}
	if v.ask == nil {
		// Non-interactive without --grant auto: reject with the
		// same posture as the embedded gate — report, don't stall.
		return pickOption(req.Options, "reject_once", "reject_always")
	}
	prompt := consentPrompt(req.ToolCall)
	for tries := 0; tries < 3; tries++ {
		answer, err := v.ask(context.Background(), prompt)
		if err != nil {
			return "" // cancelled
		}
		switch text := strings.TrimSpace(answer); strings.ToLower(text) {
		case "":
			continue // hesitation or stale newline, never a decision
		case "y", "yes":
			return pickOption(req.Options, "allow_once", "allow_always")
		case "a", "always":
			return pickOption(req.Options, "allow_always", "allow_once")
		case "n", "no":
			return pickOption(req.Options, "reject_once", "reject_always")
		default:
			v.mu.Lock()
			v.redirect = text
			v.mu.Unlock()
			return pickOption(req.Options, "reject_once", "reject_always")
		}
	}
	return pickOption(req.Options, "reject_once", "reject_always")
}

func (v *viaRun) takeRedirect() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	r := v.redirect
	v.redirect = ""
	return r
}

// pickOption returns the first option matching the preferred kinds,
// then any allow/reject fallback in that family, then cancelled.
func pickOption(options []acp.PermissionOption, kinds ...string) string {
	for _, k := range kinds {
		for _, o := range options {
			if o.Kind == k {
				return o.OptionID
			}
		}
	}
	if len(kinds) > 0 {
		family := "allow"
		if strings.HasPrefix(kinds[0], "reject") {
			family = "reject"
		}
		for _, o := range options {
			if strings.HasPrefix(o.Kind, family) {
				return o.OptionID
			}
		}
	}
	return ""
}

// consentPrompt renders the guest's tool call as the same card the
// embedded agent shows: for our reserve tool the full verdict —
// address, facts, rationale, rejected candidates; for anything else
// an honest generic card.
func consentPrompt(tc acp.Update) userPrompt {
	if isIhmeTool(tc.Title, "reserve_address") {
		var args struct {
			Address   string      `json:"address"`
			Label     string      `json:"label"`
			Rationale string      `json:"rationale"`
			Rejected  []Rejection `json:"rejected"`
			Note      string      `json:"note"`
			Tags      []string    `json:"tags"`
		}
		if json.Unmarshal(tc.RawInput, &args) == nil && args.Address != "" {
			facts := [][2]string{{"label", args.Label}}
			if args.Note != "" {
				facts = append(facts, [2]string{"note", args.Note})
			}
			if len(args.Tags) > 0 {
				facts = append(facts, [2]string{"tags", strings.Join(args.Tags, " · ")})
			}
			passed := make([][2]string, 0, len(args.Rejected))
			for _, r := range args.Rejected {
				passed = append(passed, [2]string{r.Address, r.Reason})
			}
			return userPrompt{
				Kind: promptConsent, Title: "Create this Hide My Email address?",
				Subject: args.Address, Facts: facts,
				Why: strings.TrimSpace(args.Rationale), Passed: passed,
			}
		}
	}
	subject := tc.Title
	if subject == "" {
		subject = "(unnamed tool)"
	}
	return userPrompt{
		Kind: promptConsent, Title: "Allow this agent action?",
		Subject: subject,
		Facts:   [][2]string{{"input", compact(tc.RawInput)}},
	}
}
