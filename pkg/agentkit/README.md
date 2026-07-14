# agentkit

A minimal embeddable agent kernel: a provider-neutral loop, one
streaming backend, typed tools, a pre-execution gate, hard limits,
and explicit skill invocation. Stdlib-only. Designed to be audited
in one sitting.

agentkit is not a framework. It holds no sessions, no memory, no
config, no renderer, no consent policy, and no domain logic — those
belong to the embedding application. The first consumer is
`ihme new <label> --agent`; `examples/toyagent` is the minimal one.

## Shape

```
pkg/agentkit            the kernel: loop, tools, gate, limits,
                        events, skill invocation, transcript types
pkg/agentkit/schema     fluent JSON-schema builder (map[string]any)
pkg/agentkit/ai/openai  Streamer over any OpenAI-compatible
                        /chat/completions endpoint (SSE)
```

Essential contracts:

```go
type Streamer interface {
    Stream(ctx context.Context, req Request,
           emit func(StreamEvent) error) (AssistantMessage, error)
}

type Tool interface {
    Name() string
    Description() string
    Schema() map[string]any
    Execute(context.Context, json.RawMessage) (json.RawMessage, error)
}

type Gate  func(context.Context, GateRequest) GateDecision
type Skill struct{ Name, Instructions string }

func Run(ctx context.Context, cfg RunConfig, transcript []Message) ([]Message, error)
```

Events are synchronous callbacks, not channels: natural
backpressure, renderer errors propagate (an emit error cancels the
run), no goroutine-leak obligations. Model-stream events arrive
wrapped inside agent lifecycle events (`ModelEvent`) — two
vocabularies, layered. Wrap the callback in a channel if you need
one.

## Embedding

```go
transcript, err := agentkit.Run(ctx, agentkit.RunConfig{
    Streamer: &openai.Client{BaseURL: url, APIKey: key, Model: model},
    System:   executorRules,            // stable rules, NOT the procedure
    Tools:    tools,                    // in-process functions
    Gate:     gate,                     // nil allows everything
    Limits:   agentkit.Limits{},        // zeros take defaults
    OnEvent:  render,                   // nil for silent runs
}, []agentkit.Message{skill.Invocation(task)})
```

A skill is an operational procedure invoked as a task turn — never
baked into the system prompt. The caller owns transcript
persistence: call Run again with more messages to continue.

## Loop invariants

These are the kernel's contract; each has a test.

1. Arguments are validated before gating — the gate never sees garbage.
2. Every tool call passes the gate; interactive consent lives in the
   gate implementation, not the kernel.
3. A denial becomes a tool-result error the model can adapt to.
4. An identical denied call repeated once terminates the run.
5. Tool calls from a length-truncated response are all failed, never
   executed.
6. Malformed argument text and parse diagnostics are preserved; a
   "{}" fallback is never executed.
7. Turns, actual model requests (including retries), and tool calls
   are enforced in code. There is no unlimited mode.
8. context.Context propagates through model calls, gates, and tools.
9. Only transient model failures before meaningful stream output are
   retried; tool executions never retry.
10. Renderer, configuration, consent policy, and domain logic stay
    outside the loop.

## Out of the kernel — by design

Sessions/persistence, compaction, subagents, memory, MCP, TUI,
skill discovery, OAuth, provider registries, reflection schemas,
parallel tool execution, cost accounting beyond request counting.
Each is an application concern; adding them here would trade away
the one property that makes the kernel reusable: you can read all
of it.

The design rationale, review history, and the survey of prior art
(pi, agentcore, agent-sdk-golang) live outside this repo; the
package guard test (`TestImportDirection`) enforces that the kernel
never imports application packages or third-party modules.
