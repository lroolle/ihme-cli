package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
	"github.com/lroolle/ihme-cli/skill"
)

// fakeHmeAPI is a minimal app.API for exercising tool Fns; every
// generate is unique so candidate pools are non-trivial. Delete
// models Apple's contract (must deactivate first) so the "no litter"
// assertion proves the real path, not a fantasy one.
type fakeHmeAPI struct {
	n           int
	reserved    []string
	deactivated map[string]bool
	deleted     []string
}

func (f *fakeHmeAPI) ListHme() (*api.ListHmeResult, error) { return &api.ListHmeResult{}, nil }
func (f *fakeHmeAPI) GenerateHme() (string, error) {
	f.n++
	return fmt.Sprintf("c%d@icloud.com", f.n), nil
}
func (f *fakeHmeAPI) ReserveHme(hme, label, note string) (*api.HmeEmail, error) {
	f.reserved = append(f.reserved, hme)
	return &api.HmeEmail{AnonymousID: "id:" + hme, Hme: hme, Label: label}, nil
}
func (f *fakeHmeAPI) UpdateHmeMetadata(id, label, note string) error { return nil }
func (f *fakeHmeAPI) DeactivateHme(id string) error {
	if f.deactivated == nil {
		f.deactivated = map[string]bool{}
	}
	f.deactivated[id] = true
	return nil
}
func (f *fakeHmeAPI) DeleteHme(id string) error {
	if !f.deactivated[id] {
		return fmt.Errorf("cannot delete an active address")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func refreshTool(t *testing.T, svc *app.Service, st *runState) agentkit.Tool {
	t.Helper()
	for _, tool := range tools(svc, st, "a@b", nil, nil) {
		if tool.Name() == "refresh_candidates" {
			return tool
		}
	}
	t.Fatal("refresh_candidates tool not built")
	return nil
}

func TestRefreshCandidatesCapIsPhysics(t *testing.T) {
	fake := &fakeHmeAPI{}
	st := newRunState("github")
	tool := refreshTool(t, app.New(fake), st)

	reason := `{"reason":"every candidate actively fails: leading digits, deficit word, gibberish"}`
	// A reason-less call is refused in code before touching Apple and
	// must not consume budget (GrantAuto runs have no gate in front).
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("refresh without a per-candidate reason must error")
	}
	for i := 0; i < maxRefreshCycles; i++ {
		if _, err := tool.Execute(context.Background(), json.RawMessage(reason)); err != nil {
			t.Fatalf("refresh %d within cap errored: %v", i+1, err)
		}
	}
	// One past the cap is refused in code, not left to the prompt.
	if _, err := tool.Execute(context.Background(), json.RawMessage(reason)); err == nil {
		t.Fatalf("refresh past the cap of %d must error", maxRefreshCycles)
	}
	// Each cycle burned exactly one throwaway and deleted it — no litter.
	if len(fake.reserved) != maxRefreshCycles || len(fake.deleted) != maxRefreshCycles {
		t.Fatalf("reserved=%v deleted=%v — each refresh must reserve then delete one throwaway", fake.reserved, fake.deleted)
	}

	// resetTurn restores the budget for the next request.
	st.resetTurn()
	if _, err := tool.Execute(context.Background(), json.RawMessage(reason)); err != nil {
		t.Fatalf("refresh budget did not reset for a new turn: %v", err)
	}
}

// skill/SKILL.md is runtime prompt content: the model reads it AND
// the tool schemas in the same run. If the skill stops mentioning a
// tool, or the reserve contract's required fields, the model reads
// two different contracts — that drift shipped once (the rejected[]
// split) and this test is its tombstone.
func TestSkillStaysInSyncWithEmbeddedTools(t *testing.T) {
	instructions := skill.Instructions()
	for _, tool := range tools(nil, newRunState(""), "a@b", scriptedAsker(), memory.At(t.TempDir())) {
		if !strings.Contains(instructions, tool.Name()) {
			t.Errorf("skill/SKILL.md never mentions embedded tool %q — update its execution-adapter section", tool.Name())
		}
		if tool.Name() != "reserve_address" {
			continue
		}
		schema, err := json.Marshal(tool.Schema())
		if err != nil {
			t.Fatal(err)
		}
		var parsed struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(schema, &parsed); err != nil {
			t.Fatal(err)
		}
		for _, field := range parsed.Required {
			if !strings.Contains(instructions, field) {
				t.Errorf("reserve_address requires %q but skill/SKILL.md never names it — the model reads both contracts", field)
			}
		}
	}
}
