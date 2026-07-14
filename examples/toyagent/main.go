// Command toyagent is the minimal agentkit consumer: two pure-Go
// tools, a plain-line renderer, one run. It exists to prove the
// kernel + OpenAI-compatible backend end to end against a live
// endpoint:
//
//	export OPENAI_BASE_URL=https://api.example.com/v1
//	export OPENAI_API_KEY=sk-...
//	go run ./examples/toyagent -model gpt-4o-mini "what time is it in UTC? multiply 17 by 23"
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/pkg/agentkit/ai/openai"
	"github.com/lroolle/ihme-cli/pkg/agentkit/schema"
)

func tools() []agentkit.Tool {
	utcNow := agentkit.FuncTool{
		ToolName: "utc_now",
		Desc:     "Current time in UTC (RFC 3339).",
		Params:   schema.Object(),
		Fn: func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			return json.Marshal(map[string]string{"utc": time.Now().UTC().Format(time.RFC3339)})
		},
	}
	calc := agentkit.FuncTool{
		ToolName: "calc",
		Desc:     "Basic arithmetic on two numbers.",
		Params: schema.Object(
			schema.Property("op", schema.Enum("operation", "add", "sub", "mul", "div")).Required(),
			schema.Property("a", schema.Number("left operand")).Required(),
			schema.Property("b", schema.Number("right operand")).Required(),
		),
		Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var args struct {
				Op string  `json:"op"`
				A  float64 `json:"a"`
				B  float64 `json:"b"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, err
			}
			var r float64
			switch args.Op {
			case "add":
				r = args.A + args.B
			case "sub":
				r = args.A - args.B
			case "mul":
				r = args.A * args.B
			case "div":
				if args.B == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				r = args.A / args.B
			default:
				return nil, fmt.Errorf("unknown op %q", args.Op)
			}
			return json.Marshal(map[string]float64{"result": r})
		},
	}
	return []agentkit.Tool{utcNow, calc}
}

func main() {
	baseURL := flag.String("base-url", os.Getenv("OPENAI_BASE_URL"), "OpenAI-compatible base URL")
	model := flag.String("model", os.Getenv("OPENAI_MODEL"), "model id")
	flag.Parse()
	apiKey := os.Getenv("OPENAI_API_KEY")
	task := strings.Join(flag.Args(), " ")
	if *baseURL == "" || *model == "" || apiKey == "" || task == "" {
		fmt.Fprintln(os.Stderr, "usage: OPENAI_API_KEY=... toyagent -base-url URL -model ID \"task\"")
		os.Exit(1)
	}

	streamer := &openai.Client{BaseURL: *baseURL, APIKey: apiKey, Model: *model}
	transcript := []agentkit.Message{{Role: agentkit.RoleUser, Text: task}}

	_, err := agentkit.Run(context.Background(), agentkit.RunConfig{
		Streamer: streamer,
		System:   "You are a precise assistant. Use the tools for time and arithmetic; do not guess.",
		Tools:    tools(),
		OnEvent:  render,
	}, transcript)
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		os.Exit(1)
	}
}

// render prints assistant text to stdout and lifecycle lines to
// stderr, so piping stdout captures only the answer.
func render(ev agentkit.Event) error {
	switch e := ev.(type) {
	case agentkit.ModelEvent:
		if e.Stream.Type == agentkit.StreamText {
			fmt.Print(e.Stream.Text)
		}
	case agentkit.ToolStart:
		fmt.Fprintf(os.Stderr, "\n-> %s %s\n", e.Call.Name, compact(e.Call.Args))
	case agentkit.ToolEnd:
		if e.Err != "" {
			fmt.Fprintf(os.Stderr, "<- %s error: %s\n", e.Call.Name, e.Err)
		} else {
			fmt.Fprintf(os.Stderr, "<- %s %s\n", e.Call.Name, compact(e.Result))
		}
	case agentkit.RunEnd:
		fmt.Fprintf(os.Stderr, "\n[%s | tokens in=%d out=%d]\n", e.Reason, e.Usage.InputTokens, e.Usage.OutputTokens)
	}
	return nil
}

func compact(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
