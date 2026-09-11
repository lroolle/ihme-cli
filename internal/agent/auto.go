package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/pkg/agentkit/ai/anthropic"
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
	model    string
	api      string
	locked   bool // explicit config: never switch
	switched bool // at most one flip per process
	make     func(api string) agentkit.Streamer
	persist  func(api string)
}

func newAutoStreamer(cfg Config, key string) *autoStreamer {
	a := &autoStreamer{
		model: cfg.Model,
		make: func(api string) agentkit.Streamer {
			switch api {
			case "responses":
				// Summaries make the deliberation renderable as
				// thinking events — part of the product.
				return &openai.ResponsesClient{
					BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model,
					Effort: cfg.Effort, Summary: "auto",
				}
			case "anthropic":
				c := &anthropic.Client{BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model}
				// The thinking wire shape is generational: manual
				// budgets on 4.5-and-earlier, output_config effort on
				// 4.6+ (where the manual shape is a 400).
				if anthropic.LegacyThinking(cfg.Model) {
					c.ThinkingBudget = thinkingBudget(cfg.Effort)
				} else {
					c.Effort = anthropicEffort(cfg.Effort)
				}
				return c
			}
			return &openai.Client{
				BaseURL: cfg.BaseURL, APIKey: key, Model: cfg.Model, Effort: cfg.Effort,
			}
		},
		persist: func(api string) { persistAPI(cfg.Model, api) },
	}
	switch cfg.API {
	case "completions", "responses", "anthropic":
		a.api, a.locked = cfg.API, true
	default: // "auto": what this model taught us last time, else a guess
		if learned := cfg.APIs[cfg.Model]; learned != "" {
			a.api = learned
		} else {
			a.api = guessAPI(cfg.Model, cfg.BaseURL)
		}
	}
	return a
}

// thinkingBudget maps the shared effort vocabulary to a manual
// extended-thinking token budget for legacy (pre-4.6) Claude models.
// Unknown values map to 0 (thinking off) — the header reports that
// honestly rather than inventing a budget the user never asked for.
func thinkingBudget(effort string) int {
	switch effort {
	case "minimal":
		return 1024
	case "low":
		return 2048
	case "medium":
		return 8192
	case "high":
		return 16384
	}
	return 0
}

// anthropicEffort maps the shared effort vocabulary to Anthropic's
// output_config values for 4.6-and-later models. minimal folds into
// low (Anthropic's floor); native values pass through; unknown maps
// to "" (parameter omitted, model default applies).
func anthropicEffort(effort string) string {
	switch effort {
	case "minimal":
		return "low"
	case "low", "medium", "high", "xhigh", "max":
		return effort
	}
	return ""
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
	msg, err = a.make(a.api).Stream(ctx, req, emit)
	if _, again := misroute(a.api, err); again && !meaningful {
		// Both protocols refused: the model wants one the gateway
		// does not serve. Say that instead of surfacing the second
		// refusal as if it were the only problem.
		return msg, fmt.Errorf("%s needs the %s API, but this endpoint does not serve it: %w", a.model, a.api, err)
	}
	return msg, err
}

// misroute classifies "wrong API for this model" errors.
func misroute(current string, err error) (string, bool) {
	status, body, ok := apiFailure(err)
	if !ok {
		return "", false
	}
	switch current {
	case "completions":
		// e.g. "Function tools with reasoning_effort are not supported
		// for <model> in /v1/chat/completions. To use function tools,
		// use /v1/responses ..." and OpenAI's "this model is only
		// supported in v1/responses".
		if status == 400 && strings.Contains(body, "responses") {
			return "responses", true
		}
	case "responses":
		// Endpoint absent on completions-only gateways/runtimes.
		if endpointAbsent(status, body) {
			return "completions", true
		}
	case "anthropic":
		// A claude model on an OpenAI-protocol gateway: /v1/messages
		// does not exist there — fall back to chat completions.
		if endpointAbsent(status, body) {
			return "completions", true
		}
	}
	return "", false
}

// endpointAbsent classifies "this path does not exist here" replies.
// Beyond the plain 404/405, new-api-family gateways answer
// unimplemented endpoints with 500 {"message":"not implemented",
// "type":"new_api_error"} — permanent despite the 5xx dress, so it
// must flip the protocol rather than burn the transient retries.
func endpointAbsent(status int, body string) bool {
	switch status {
	case 404, 405, 501:
		return true
	case 400:
		return strings.Contains(body, "unknown") && strings.Contains(body, "path")
	case 500:
		return strings.Contains(body, "not implemented")
	}
	return false
}

// apiFailure extracts status and lowercased body from either
// provider's typed API error.
func apiFailure(err error) (int, string, bool) {
	var oaiErr *openai.APIError
	if errors.As(err, &oaiErr) {
		return oaiErr.Status, strings.ToLower(oaiErr.Body), true
	}
	var antErr *anthropic.APIError
	if errors.As(err, &antErr) {
		return antErr.Status, strings.ToLower(antErr.Body), true
	}
	return 0, "", false
}

// guessAPI picks the starting protocol from the endpoint and model
// family. Anthropic's own host always speaks the Messages API;
// claude models elsewhere start native and self-heal to completions
// when the gateway has no /v1/messages. Reasoning-first OpenAI
// families need /responses for function tools — gpt-5 and every
// later generation (gpt-6-astra, ...), the o-series, codex — and
// deepseek ships native /responses from v4 on; earlier generations
// of both stay on /chat/completions, as does everything else. Wrong
// guesses self-heal via misroute detection.
func guessAPI(model, baseURL string) string {
	if strings.Contains(baseURL, "anthropic.com") {
		return "anthropic"
	}
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "claude"):
		return "anthropic"
	case generation(m, "gpt-") >= 5, generation(m, "deepseek-v") >= 4:
		return "responses"
	case strings.HasPrefix(m, "codex"), oSeries.MatchString(m):
		return "responses"
	}
	return "completions"
}

// oSeries matches OpenAI's reasoning line: o1, o3-pro, o4-mini.
var oSeries = regexp.MustCompile(`^o[1-9]`)

// generation reads the major version that follows a family prefix:
// "gpt-6-astra" is generation 6 of "gpt-", "gpt-4o-mini" is 4.
// Returns -1 when the model is not in that family or carries no
// leading number, so a comparison against it is always false.
func generation(model, prefix string) int {
	rest, ok := strings.CutPrefix(model, prefix)
	if !ok {
		return -1
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return -1
	}
	return n
}

// persistAPI records the discovered protocol in agent.json, keyed
// by model, so the next run of THIS model starts on the right API,
// and tells the user once. It never touches the top-level "api"
// pin: that field is the user's, and a discovery written there
// once locked every later model to the first model's answer.
func persistAPI(model, api string) {
	path := filepath.Join(configDir(), "agent.json")
	cfg := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &cfg) // best effort: unreadable -> rewrite minimal
	}
	apis, _ := cfg["apis"].(map[string]any)
	if apis == nil {
		apis = map[string]any{}
	}
	apis[model] = api
	cfg["apis"] = apis
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "\n(%s speaks the %s API — switched for this run, but saving %s failed: %v)\n", model, api, path, err)
		return
	}
	fmt.Fprintf(os.Stderr, "\n(%s speaks the %s API — switched automatically and remembered in %s)\n", model, api, path)
}
