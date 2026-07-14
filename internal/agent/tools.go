package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/pkg/agentkit/schema"
	"github.com/lroolle/ihme-cli/pkg/filter"
)

// maxGenerateRounds caps candidate generation per run. The limit
// lives in the tool, not the prompt: SKILL.md asks the model to stop
// at 3, this makes it physics.
const maxGenerateRounds = 3

// runState is the per-run scope shared by tools and gate: what this
// run created is what unattended consent covers.
type runState struct {
	label          string
	generateRounds int
	reserves       int
	// reservedThisRun holds both the hme address and anonymousId of
	// every reservation made during this run.
	reservedThisRun map[string]bool
	lastReserved    *api.HmeEmail
}

func newRunState(label string) *runState {
	return &runState{label: label, reservedThisRun: map[string]bool{}}
}

func (st *runState) ownedRef(ref string) bool {
	return st.reservedThisRun[strings.ToLower(strings.TrimSpace(ref))]
}

func (st *runState) recordReservation(hme *api.HmeEmail) {
	st.reserves++
	st.reservedThisRun[strings.ToLower(hme.Hme)] = true
	st.reservedThisRun[strings.ToLower(hme.AnonymousID)] = true
	st.lastReserved = hme
}

// addressView is the trimmed, hint-free projection fed to the model.
// CLI hints reference shell commands — the wrong adapter here.
type addressView struct {
	AnonymousID string `json:"anonymousId"`
	Label       string `json:"label"`
	Hme         string `json:"hme"`
	IsActive    bool   `json:"isActive"`
	Note        string `json:"note,omitempty"`
}

func view(e api.HmeEmail) addressView {
	return addressView{
		AnonymousID: e.AnonymousID, Label: e.Label, Hme: e.Hme,
		IsActive: e.IsActive, Note: e.Note,
	}
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}

// tools builds the six in-process tools over the application
// service. authStatus is pre-verified before the agent starts.
func tools(svc *app.Service, st *runState, appleID string) []agentkit.Tool {
	return []agentkit.Tool{
		agentkit.FuncTool{
			ToolName: "auth_status",
			Desc:     "Check iCloud authentication status.",
			Params:   schema.Object(),
			Fn: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
				return marshal(map[string]any{
					"loggedIn": true, "appleId": appleID, "canAccessICloud": true,
					"note": "session verified before this run started",
				})
			},
		},
		agentkit.FuncTool{
			ToolName: "search_addresses",
			Desc:     "Search existing addresses by substring across label, address, and note. Use before creating anything.",
			Params: schema.Object(
				schema.Property("query", schema.String("search key, e.g. the registrable domain without suffix")).Required(),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var args struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				emails, err := svc.List(filter.Options{Search: args.Query})
				if err != nil {
					return nil, err
				}
				views := make([]addressView, len(emails))
				for i, e := range emails {
					views[i] = view(e)
				}
				return marshal(map[string]any{"addresses": views, "count": len(views)})
			},
		},
		agentkit.FuncTool{
			ToolName: "generate_candidates",
			Desc: fmt.Sprintf("Generate fresh candidate addresses from Apple. Hard limit: %d rounds per run.",
				maxGenerateRounds),
			Params: schema.Object(
				schema.Property("count", schema.Int("how many candidates (default 3)")),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				if st.generateRounds >= maxGenerateRounds {
					return nil, fmt.Errorf("generation limit reached (%d rounds) — pick from existing candidates or report", maxGenerateRounds)
				}
				st.generateRounds++
				var args struct {
					Count int `json:"count"`
				}
				_ = json.Unmarshal(raw, &args)
				if args.Count <= 0 || args.Count > 5 {
					args.Count = 3
				}
				candidates, err := svc.Generate(args.Count)
				if err != nil {
					return nil, err
				}
				return marshal(map[string]any{
					"candidates": candidates,
					"round":      st.generateRounds,
					"roundsLeft": maxGenerateRounds - st.generateRounds,
				})
			},
		},
		agentkit.FuncTool{
			ToolName: "reserve_address",
			Desc:     "Reserve one generated candidate under a label, with an optional durable note and tags.",
			Params: schema.Object(
				schema.Property("address", schema.String("the candidate address to reserve")).Required(),
				schema.Property("label", schema.String("service label, bare noun")).Required(),
				schema.Property("note", schema.String("compact durable note: why it exists, signup URL, context. Never secrets.")),
				schema.Property("tags", schema.Array("tags like dev, work", schema.String("tag"))),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var args struct {
					Address string   `json:"address"`
					Label   string   `json:"label"`
					Note    string   `json:"note"`
					Tags    []string `json:"tags"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				reserved, err := svc.Reserve(args.Address, args.Label, args.Tags, args.Note)
				if err != nil {
					return nil, err
				}
				st.recordReservation(reserved)
				return marshal(map[string]any{"status": "reserved", "address": view(*reserved)})
			},
		},
		agentkit.FuncTool{
			ToolName: "deactivate_address",
			Desc:     "Deactivate an address by id, email, or label. Mail to it is rejected afterwards.",
			Params: schema.Object(
				schema.Property("ref", schema.String("anonymousId, email, or label")).Required(),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var args struct {
					Ref string `json:"ref"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				hme, changed, err := svc.Deactivate(args.Ref)
				if err != nil {
					return nil, err
				}
				status := "deactivated"
				if !changed {
					status = "already_inactive"
				}
				return marshal(map[string]any{"status": status, "hme": hme.Hme, "id": hme.AnonymousID})
			},
		},
		agentkit.FuncTool{
			ToolName: "edit_note",
			Desc:     "Update label, note, or tags of an address. Omitted fields keep their value; tags replace all existing tags.",
			Params: schema.Object(
				schema.Property("ref", schema.String("anonymousId, email, or label")).Required(),
				schema.Property("label", schema.String("new label")),
				schema.Property("note", schema.String("new note")),
				schema.Property("tags", schema.Array("replacement tags", schema.String("tag"))),
			),
			Fn: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
				var args struct {
					Ref   string    `json:"ref"`
					Label *string   `json:"label"`
					Note  *string   `json:"note"`
					Tags  *[]string `json:"tags"`
				}
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, err
				}
				hme, err := svc.UpdateMeta(args.Ref, app.MetaPatch{Label: args.Label, Note: args.Note, Tags: args.Tags})
				if err != nil {
					return nil, err
				}
				return marshal(map[string]any{"status": "updated", "hme": hme.Hme, "id": hme.AnonymousID})
			},
		},
	}
}
