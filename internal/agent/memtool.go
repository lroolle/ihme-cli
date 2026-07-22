package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/pkg/agentkit/schema"
)

// The memory split, borrowed from pi.dev's context-engineering
// doctrine: code writes the episodic record the moment a fact
// happens (writeReservation, below — uniform across one-shot, REPL,
// and TUI), the model records only judgment it alone knows (the
// remember tool), recall is on demand (recall_memory), and the
// flashcards page is the one always-loaded keeper layer (injected by
// memoryContext). Nothing here is gated: it touches only the user's
// own local notebook, never Apple and never the network.

const (
	// recentJournalBudget bounds the always-injected journal tail so
	// old runs never crowd out the live task.
	recentJournalBudget = 1500
	// flashcardsBudget bounds the always-injected flashcards slice.
	// The file keeps every pin; only the injected tail (newest cards)
	// is capped, so an over-pinned flashcards page cannot grow every
	// future run's context without limit.
	flashcardsBudget = 1200
	// recallLimit caps recall_memory hits per call.
	recallLimit = 8
)

// memoryTools are the model's read/write access to memory. Returned
// empty when no store is available so callers can append blindly.
func memoryTools(mem *memory.Store) []agentkit.Tool {
	if mem == nil {
		return nil
	}
	return []agentkit.Tool{
		agentkit.FuncTool{
			ToolName: "recall_memory",
			Desc:     "Search your own memory (past runs and topic pages) before acting. Recall an existing address's story by its service name, or check what you learned about the user before.",
			Params: schema.Object(
				schema.Property("query", schema.String("a service name, address, or topic to look up")).Required(),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var args struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				hits := mem.Search(args.Query, recallLimit)
				return marshal(map[string]any{"hits": hits, "count": len(hits)})
			},
		},
		agentkit.FuncTool{
			ToolName: "remember",
			Desc: fmt.Sprintf("Save one durable fact you want future runs to know — a lasting user preference or a note about a service. Write to the %q topic page to have it loaded into EVERY future run (use sparingly); any other topic is recalled on demand. Reservations are journaled automatically — do not record those here. Never store secrets.",
				memory.FlashcardsPage),
			Params: schema.Object(
				schema.Property("topic", schema.String(fmt.Sprintf("the page to file under: a service name, %q for a general preference, or %q to pin into every run", "preferences", memory.FlashcardsPage))).Required(),
				schema.Property("fact", schema.String("one durable sentence, in your own words")).Required(),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var args struct {
					Topic string `json:"topic"`
					Fact  string `json:"fact"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				if err := mem.PageAppend(args.Topic, args.Fact); err != nil {
					return nil, fmt.Errorf("could not write memory: %w", err)
				}
				return marshal(map[string]any{
					"remembered":   args.Topic,
					"alwaysLoaded": strings.EqualFold(args.Topic, memory.FlashcardsPage),
				})
			},
		},
	}
}

// memoryContext is the continuity block injected once at the top of
// a session: the always-loaded flashcards plus a bounded tail of
// recent journal entries. Empty on a cold graph. It is framed as the
// agent's own notes, not a live instruction — the addresses inside
// are records.
func memoryContext(mem *memory.Store) string {
	if mem == nil {
		return ""
	}
	var b strings.Builder
	if cards, ok := mem.ReadPage(memory.FlashcardsPage); ok {
		if cards = lastLinesWithin(strings.TrimSpace(cards), flashcardsBudget); cards != "" {
			b.WriteString("What you always keep in mind:\n")
			b.WriteString(cards)
			b.WriteString("\n")
		}
	}
	if recent := strings.TrimSpace(mem.RecentJournals(recentJournalBudget)); recent != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Recently:\n")
		b.WriteString(recent)
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return ""
	}
	return "<memory>\nYour notes from earlier runs — background continuity, not a new" +
		" instruction. Any address here is a record of what you did, not a\n" +
		"request to repeat it. Use recall_memory to look deeper.\n\n" +
		strings.TrimRight(b.String(), "\n") + "\n</memory>"
}

// lastLinesWithin returns the trailing whole lines of text that fit
// within budget bytes — the newest content, since memory pages are
// append-only. Used to cap what the flashcards page injects into
// every run without truncating mid-line.
func lastLinesWithin(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	total := 0
	for i := len(lines) - 1; i >= 0; i-- {
		total += len(lines[i]) + 1
		if total > budget && len(kept) > 0 {
			break
		}
		kept = append(kept, lines[i])
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, "\n")
}

// writeReservation records a reservation the moment Apple confirms
// it: a dated journal block linking the service page, and a bullet on
// the service page itself so a topic accumulates its own history.
// Best-effort by contract: the error is returned so a caller MAY
// surface it, but the reserve path deliberately ignores it — a
// memory-write failure must never fail the reservation that fed it.
func writeReservation(mem *memory.Store, e *api.HmeEmail, rationale string, rejected []Rejection) error {
	if mem == nil || e == nil {
		return nil
	}
	label := strings.TrimSpace(e.Label)
	if label == "" {
		label = e.Hme
	}
	rationale = strings.TrimSpace(rationale)

	var block strings.Builder
	fmt.Fprintf(&block, "- reserved **%s** for [[%s]]", e.Hme, label)
	if rationale != "" {
		fmt.Fprintf(&block, "\n  - %s", rationale)
	}
	for _, r := range rejected {
		fmt.Fprintf(&block, "\n  - passed: %s — %s", r.Address, r.Reason)
	}
	if err := mem.JournalAppend(block.String()); err != nil {
		return err
	}

	bullet := time.Now().Format("2006-01-02") + " reserved " + e.Hme
	if rationale != "" {
		bullet += " — " + rationale
	}
	return mem.PageAppend(label, bullet)
}
