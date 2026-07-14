package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/pkg/agentkit/ai/openai"
)

// autoStreamer resolves which wire protocol a model actually speaks.
// pi solves this with a shipped model registry (every model carries
// its api field); gateway aliases make a static registry impossible
// here, so the metadata is learned instead: start from a model-name
// heuristic, flip on the endpoint's misroute signal, persist the
// answer so the wrong first call happens at most once per model —
// never per run.
type autoStreamer struct {
	api      string
	locked   bool // explicit config: never switch
	switched bool // at most one flip per process
	make     func(api string) agentkit.Streamer
	persist  func(api string)
}

func newAutoStreamer(cfg Config, key string) *autoStreamer {
	a := &autoStreamer{
		make: func(api string) agentkit.Streamer {
			if api == "responses" {
				return &openai.ResponsesClient{BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model, Effort: cfg.Effort}
			}
			return &openai.Client{BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model}
		},
		persist: func(api string) { persistAPI(cfg.Model, api) },
	}
	switch cfg.API {
	case "completions", "responses":
		a.api, a.locked = cfg.API, true
	default: // "auto"
		a.api = guessAPI(cfg.Model)
	}
	return a
}

// Stream implements agentkit.Streamer. On a protocol-misroute error
// before any output, it flips the API, persists the discovery, and
// retries the same request once.
func (a *autoStreamer) Stream(ctx context.Context, req agentkit.Request, emit func(agentkit.StreamEvent) error) (agentkit.AssistantMessage, error) {
	meaningful := false
	wrapped := func(ev agentkit.StreamEvent) error {
		meaningful = true
		return emit(ev)
	}
	msg, err := a.make(a.api).Stream(ctx, req, wrapped)
	if err == nil || a.locked || a.switched || meaningful || ctx.Err() != nil {
		return msg, err
	}
	next, ok := misroute(a.api, err)
	if !ok {
		return msg, err
	}
	a.api = next
	a.switched = true
	if a.persist != nil {
		a.persist(next)
	}
	return a.make(a.api).Stream(ctx, req, emit)
}

// misroute classifies "wrong API for this model" errors.
func misroute(current string, err error) (string, bool) {
	var apiErr *openai.APIError
	if !errors.As(err, &apiErr) {
		return "", false
	}
	body := strings.ToLower(apiErr.Body)
	switch current {
	case "completions":
		// e.g. "Function tools with reasoning_effort are not supported
		// for <model> in /v1/chat/completions. To use function tools,
		// use /v1/responses ..." and OpenAI's "this model is only
		// supported in v1/responses".
		if apiErr.Status == 400 && strings.Contains(body, "responses") {
			return "responses", true
		}
	case "responses":
		// Endpoint absent on completions-only gateways/runtimes.
		if apiErr.Status == 404 || apiErr.Status == 405 ||
			(apiErr.Status == 400 && strings.Contains(body, "unknown") && strings.Contains(body, "path")) {
			return "completions", true
		}
	}
	return "", false
}

// guessAPI picks the starting protocol from the model family.
// Reasoning-first families need /responses for function tools;
// everything else speaks /chat/completions. Wrong guesses self-heal
// via misroute detection.
func guessAPI(model string) string {
	m := strings.ToLower(model)
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4", "codex"} {
		if strings.HasPrefix(m, prefix) {
			return "responses"
		}
	}
	return "completions"
}

// persistAPI records the discovered protocol in agent.json so the
// next run starts on the right API, and tells the user once.
func persistAPI(model, api string) {
	path := filepath.Join(configDir(), "agent.json")
	cfg := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &cfg) // best effort: unreadable -> rewrite minimal
	}
	cfg["api"] = api
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "\n(%s speaks the %s API — switched for this run, but saving %s failed: %v)\n", model, api, path, err)
		return
	}
	fmt.Fprintf(os.Stderr, "\n(%s speaks the %s API — switched automatically and saved to %s)\n", model, api, path)
}
