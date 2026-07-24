package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/internal/memory"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

func memTool(t *testing.T, mem *memory.Store, name string) agentkit.Tool {
	t.Helper()
	for _, tool := range memoryTools(mem) {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("memory tool %q not built", name)
	return nil
}

func TestRememberToolAppendsAndFlagsFlashcards(t *testing.T) {
	mem := memory.At(t.TempDir())
	remember := memTool(t, mem, "remember")

	// A normal topic is not always-loaded.
	out, err := remember.Execute(context.Background(), json.RawMessage(`{"topic":"github","fact":"work account"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"alwaysLoaded":true`) {
		t.Errorf("a normal topic must not be flagged always-loaded: %s", out)
	}
	if !strings.Contains(string(out), `"status":"created"`) {
		t.Errorf("first write to a topic must report created: %s", out)
	}
	if page, ok := mem.ReadPage("github"); !ok || !strings.Contains(page, "work account") {
		t.Errorf("remember did not persist: %q", page)
	}

	// A second fact on the same topic is an update, not a creation.
	out, err = remember.Execute(context.Background(), json.RawMessage(`{"topic":"github","fact":"prefers short addresses"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"status":"updated"`) {
		t.Errorf("second write to a topic must report updated: %s", out)
	}

	// The flashcards topic IS always-loaded — the model must be told.
	out, err = remember.Execute(context.Background(), json.RawMessage(`{"topic":"flashcards","fact":"prefer short addresses"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"alwaysLoaded":true`) {
		t.Errorf("flashcards must be flagged always-loaded so the model spends it sparingly: %s", out)
	}
}

func TestRecallToolFindsWhatWasRemembered(t *testing.T) {
	mem := memory.At(t.TempDir())
	if err := mem.PageAppend("github", "reserved calm_mule@icloud.com"); err != nil {
		t.Fatal(err)
	}
	recall := memTool(t, mem, "recall_memory")

	out, err := recall.Execute(context.Background(), json.RawMessage(`{"query":"github"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "calm_mule@icloud.com") {
		t.Errorf("recall did not surface the note: %s", out)
	}

	// A miss is an empty result, not an error.
	out, err = recall.Execute(context.Background(), json.RawMessage(`{"query":"netflix"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"count":0`) {
		t.Errorf("a miss must report count 0, not error: %s", out)
	}
}

func TestWriteReservationLinksJournalAndPage(t *testing.T) {
	mem := memory.At(t.TempDir())
	hme := &api.HmeEmail{Hme: "calm_mule@icloud.com", Label: "github"}
	rejected := []Rejection{{Address: "turbo3_placard@icloud.com", Reason: "leading noise"}}

	note := writeReservation(mem, hme, "a calm animal, keeps clean", rejected)
	if note.Status != "created" || note.Topic != "github" {
		t.Fatalf("first reservation should create the topic page, got %+v", note)
	}

	// The journal links the service page so the graph gets an edge.
	nodes := mem.Graph()
	var gh memory.Node
	for _, n := range nodes {
		if n.Title == "github" {
			gh = n
		}
	}
	if gh.Backlinks == 0 {
		t.Errorf("reservation did not link [[github]] in the journal: %+v", nodes)
	}
	// The service page accumulates its own dated history.
	page, ok := mem.ReadPage("github")
	if !ok || !strings.Contains(page, "calm_mule@icloud.com") {
		t.Errorf("github page missing the reservation: %q", page)
	}

	// A second reservation for the same service updates, not creates.
	note = writeReservation(mem, hme, "still calm", nil)
	if note.Status != "updated" {
		t.Errorf("repeat reservation should report updated, got %+v", note)
	}
}

func TestMemoryLineStatesTheActualOperation(t *testing.T) {
	cases := map[memoryNote]string{
		{Status: "created", Topic: "undetectable"}: `Memory created for "undetectable"`,
		{Status: "updated", Topic: "github"}:       `Memory updated for "github"`,
		{}:                                         "",
	}
	for note, want := range cases {
		if got := memoryLine(note); got != want {
			t.Errorf("memoryLine(%+v) = %q, want %q", note, got, want)
		}
	}
	if got := memoryLine(memoryNote{Status: "failed", Topic: "x"}); !strings.Contains(got, "failed") {
		t.Errorf("a failed write must say so, got %q", got)
	}
}

func TestMemoryContextInjectsFlashcardsAndJournal(t *testing.T) {
	mem := memory.At(t.TempDir())
	if got := memoryContext(mem); got != "" {
		t.Errorf("cold graph should inject nothing, got %q", got)
	}

	if err := mem.PageAppend(memory.FlashcardsPage, "prefer short addresses"); err != nil {
		t.Fatal(err)
	}
	if err := mem.JournalAppend("- reserved x for [[github]]"); err != nil {
		t.Fatal(err)
	}

	got := memoryContext(mem)
	if !strings.Contains(got, "prefer short addresses") {
		t.Errorf("flashcards not injected: %q", got)
	}
	if !strings.Contains(got, "github") {
		t.Errorf("recent journal not injected: %q", got)
	}
	if !strings.HasPrefix(got, "<memory>") {
		t.Errorf("context should be framed as memory, not a live instruction: %q", got)
	}
}

func TestWriteReservationIsNilSafe(t *testing.T) {
	if note := writeReservation(nil, nil, "x", nil); note.Status != "" {
		t.Errorf("nil store/address must be a no-op, got %+v", note)
	}
}

func TestMemoryContextBoundsFlashcards(t *testing.T) {
	mem := memory.At(t.TempDir())
	for i := 0; i < 200; i++ {
		if err := mem.PageAppend(memory.FlashcardsPage, strings.Repeat("card ", 10)); err != nil {
			t.Fatal(err)
		}
	}
	got := memoryContext(mem)
	// The whole page is far over budget; the injected slice must be capped.
	if len(got) > flashcardsBudget+400 {
		t.Errorf("flashcards injection unbounded: %d bytes", len(got))
	}
	// It must still keep the NEWEST cards (append-only tail).
	if err := mem.PageAppend(memory.FlashcardsPage, "the-freshest-marker"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(memoryContext(mem), "the-freshest-marker") {
		t.Error("budget dropped the newest card")
	}
}
