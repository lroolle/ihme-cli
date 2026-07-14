package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrRepeatedDenial terminates a run when the model repeats a tool
// call that the gate already denied with identical arguments.
var ErrRepeatedDenial = errors.New("identical tool call denied twice")

// maxAttempts bounds transient retries per model request: one
// attempt plus two retries.
const maxAttempts = 3

// RunConfig configures one run. Renderer, configuration, consent
// policy, and domain logic stay outside the loop: the kernel sees
// them only through Streamer, Tools, Gate, and OnEvent.
type RunConfig struct {
	Streamer Streamer
	System   string
	Tools    []Tool
	Gate     Gate              // nil allows every call
	Limits   Limits            // zero fields take defaults
	OnEvent  func(Event) error // nil for no observation; an error aborts the run
}

// Run drives the agent loop over the given transcript until the
// model completes without tool calls, a limit trips, the gate
// terminates the run, or ctx is canceled. It returns the full
// updated transcript, including on error — the caller owns
// persistence and resumption (call Run again with more messages).
func Run(ctx context.Context, cfg RunConfig, transcript []Message) ([]Message, error) {
	limits := cfg.Limits.withDefaults()
	emit := func(Event) error { return nil }
	if cfg.OnEvent != nil {
		emit = cfg.OnEvent
	}
	toolset := make(map[string]Tool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		toolset[t.Name()] = t
	}
	defs := Defs(cfg.Tools)

	var requests, toolCalls int
	var usage Usage
	denied := make(map[string]bool)

	fail := func(err error) ([]Message, error) {
		_ = emit(RunEnd{Reason: err.Error(), Usage: usage})
		return transcript, err
	}

	if err := emit(RunStart{}); err != nil {
		return transcript, err
	}

	for turn := 1; ; turn++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if turn > limits.MaxTurns {
			return fail(LimitError{Limit: "turns", Max: limits.MaxTurns})
		}
		if err := emit(TurnStart{Turn: turn}); err != nil {
			return transcript, err
		}

		msg, err := streamWithRetry(ctx, cfg.Streamer, Request{
			System:   cfg.System,
			Messages: transcript,
			Tools:    defs,
		}, turn, emit, &requests, limits.MaxRequests)
		if err != nil {
			var lim LimitError
			if errors.As(err, &lim) {
				return fail(err)
			}
			return fail(fmt.Errorf("model request: %w", err))
		}
		usage.InputTokens += msg.Usage.InputTokens
		usage.OutputTokens += msg.Usage.OutputTokens
		transcript = append(transcript, msg.Message())

		if len(msg.ToolCalls) == 0 {
			if err := emit(TurnEnd{Turn: turn, Message: msg}); err != nil {
				return transcript, err
			}
			if err := emit(RunEnd{Reason: "done", Usage: usage}); err != nil {
				return transcript, err
			}
			return transcript, nil
		}

		// A "length" stop means the output was cut off, so every tool
		// call may carry truncated arguments. Fail them all instead of
		// executing potentially broken calls.
		if msg.StopReason == StopLength {
			for _, call := range msg.ToolCalls {
				const reason = "not executed: the response was truncated by the token limit; re-issue the call"
				if err := emit(ToolEnd{Turn: turn, Call: call, Err: reason}); err != nil {
					return transcript, err
				}
				transcript = append(transcript, toolErrorMessage(call, reason))
			}
			if err := emit(TurnEnd{Turn: turn, Message: msg}); err != nil {
				return transcript, err
			}
			continue
		}

		for _, call := range msg.ToolCalls {
			toolCalls++
			if toolCalls > limits.MaxToolCalls {
				return fail(LimitError{Limit: "tool_calls", Max: limits.MaxToolCalls})
			}

			// Validate before gating: the gate never sees garbage.
			if call.ParseErr != "" {
				reason := fmt.Sprintf("invalid arguments (not executed): %s; raw arguments: %s", call.ParseErr, call.RawArgs)
				if err := emit(ToolEnd{Turn: turn, Call: call, Err: reason}); err != nil {
					return transcript, err
				}
				transcript = append(transcript, toolErrorMessage(call, reason))
				continue
			}
			tool, ok := toolset[call.Name]
			if !ok {
				reason := fmt.Sprintf("unknown tool %q", call.Name)
				if err := emit(ToolEnd{Turn: turn, Call: call, Err: reason}); err != nil {
					return transcript, err
				}
				transcript = append(transcript, toolErrorMessage(call, reason))
				continue
			}

			if cfg.Gate != nil {
				decision := cfg.Gate(ctx, GateRequest{Turn: turn, Call: call})
				if !decision.Allowed {
					reason := "denied: " + decision.Reason
					if err := emit(ToolEnd{Turn: turn, Call: call, Err: reason, Denied: true}); err != nil {
						return transcript, err
					}
					transcript = append(transcript, toolErrorMessage(call, reason))
					sig := call.Name + "\x00" + string(call.Args)
					if denied[sig] {
						return fail(ErrRepeatedDenial)
					}
					denied[sig] = true
					continue
				}
			}

			if err := emit(ToolStart{Turn: turn, Call: call}); err != nil {
				return transcript, err
			}
			// Execute exactly once — mutations must never auto-retry.
			result, execErr := tool.Execute(ctx, call.Args)
			if execErr != nil {
				reason := "error: " + execErr.Error()
				if err := emit(ToolEnd{Turn: turn, Call: call, Err: reason}); err != nil {
					return transcript, err
				}
				transcript = append(transcript, toolErrorMessage(call, reason))
				continue
			}
			if err := emit(ToolEnd{Turn: turn, Call: call, Result: result}); err != nil {
				return transcript, err
			}
			transcript = append(transcript, Message{
				Role: RoleTool, ToolCallID: call.ID, ToolName: call.Name, Text: string(result),
			})
		}

		if err := emit(TurnEnd{Turn: turn, Message: msg}); err != nil {
			return transcript, err
		}
	}
}

// streamWithRetry makes one model request, retrying only transient
// failures that occur before any meaningful stream output reached
// the consumer. Every attempt counts against the request limit.
func streamWithRetry(
	ctx context.Context,
	s Streamer,
	req Request,
	turn int,
	emit func(Event) error,
	requests *int,
	maxRequests int,
) (AssistantMessage, error) {
	for attempt := 1; ; attempt++ {
		*requests++
		if *requests > maxRequests {
			return AssistantMessage{}, LimitError{Limit: "requests", Max: maxRequests}
		}
		meaningful := false
		var emitErr error
		msg, err := s.Stream(ctx, req, func(ev StreamEvent) error {
			meaningful = true
			if e := emit(ModelEvent{Turn: turn, Stream: ev}); e != nil {
				emitErr = e
				return e
			}
			return nil
		})
		if emitErr != nil {
			return AssistantMessage{}, emitErr
		}
		if err == nil {
			return msg, nil
		}
		if IsTransient(err) && !meaningful && attempt < maxAttempts && ctx.Err() == nil {
			continue
		}
		return AssistantMessage{}, err
	}
}

func toolErrorMessage(call ToolCall, reason string) Message {
	// JSON-encode so tool results stay uniformly parseable text.
	body, _ := json.Marshal(map[string]string{"error": reason})
	return Message{
		Role: RoleTool, ToolCallID: call.ID, ToolName: call.Name,
		Text: string(body), IsError: true,
	}
}
