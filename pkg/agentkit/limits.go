package agentkit

import "fmt"

// Limits bounds one run. Zero values take the defaults below; the
// kernel always enforces some bound — there is no unlimited mode.
// MaxRequests counts actual model requests including retries, not
// turns: on gateways that report zero cost, requests are the only
// enforceable budget unit.
type Limits struct {
	MaxTurns     int
	MaxRequests  int
	MaxToolCalls int
}

const (
	DefaultMaxTurns     = 16
	DefaultMaxRequests  = 24
	DefaultMaxToolCalls = 48
)

func (l Limits) withDefaults() Limits {
	if l.MaxTurns <= 0 {
		l.MaxTurns = DefaultMaxTurns
	}
	if l.MaxRequests <= 0 {
		l.MaxRequests = DefaultMaxRequests
	}
	if l.MaxToolCalls <= 0 {
		l.MaxToolCalls = DefaultMaxToolCalls
	}
	return l
}

// LimitError reports which limit terminated the run.
type LimitError struct {
	Limit string // "turns", "requests", "tool_calls"
	Max   int
}

func (e LimitError) Error() string {
	return fmt.Sprintf("run exceeded %s limit (%d)", e.Limit, e.Max)
}
