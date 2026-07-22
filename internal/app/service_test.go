package app

import (
	"errors"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/lroolle/ihme-cli/pkg/filter"
)

type fakeAPI struct {
	emails    []api.HmeEmail
	genQueue  []string
	genErr    error
	reserved  []string
	updates   []string // "id|label|note"
	deactived []string
	deleted   []string
}

func (f *fakeAPI) ListHme() (*api.ListHmeResult, error) {
	return &api.ListHmeResult{HmeEmails: f.emails}, nil
}
func (f *fakeAPI) GenerateHme() (string, error) {
	if len(f.genQueue) == 0 {
		return "", f.genErr
	}
	hme := f.genQueue[0]
	f.genQueue = f.genQueue[1:]
	return hme, nil
}
func (f *fakeAPI) ReserveHme(hme, label, note string) (*api.HmeEmail, error) {
	f.reserved = append(f.reserved, hme+"|"+label+"|"+note)
	return &api.HmeEmail{AnonymousID: "id:" + hme, Hme: hme, Label: label, Note: note, IsActive: true}, nil
}
func (f *fakeAPI) UpdateHmeMetadata(id, label, note string) error {
	f.updates = append(f.updates, id+"|"+label+"|"+note)
	return nil
}
func (f *fakeAPI) DeactivateHme(id string) error {
	f.deactived = append(f.deactived, id)
	return nil
}

// DeleteHme models Apple's real contract: an ACTIVE address cannot be
// deleted; it must be deactivated first (see cmd/lifecycle delete).
func (f *fakeAPI) DeleteHme(id string) error {
	deactivated := false
	for _, d := range f.deactived {
		if d == id {
			deactivated = true
		}
	}
	if !deactivated {
		return errors.New("cannot delete an active address — deactivate first")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func TestGenerateDedupes(t *testing.T) {
	f := &fakeAPI{genQueue: []string{"a@icloud.com", "a@icloud.com", "b@icloud.com", "c@icloud.com"}}
	got, err := New(f).Generate(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "a@icloud.com" || got[2] != "c@icloud.com" {
		t.Fatalf("candidates = %v", got)
	}
}

func TestGeneratePartialPoolOnError(t *testing.T) {
	f := &fakeAPI{genQueue: []string{"a@icloud.com"}, genErr: errors.New("rate limited")}
	got, err := New(f).Generate(3)
	if err != nil {
		t.Fatalf("partial pool must not error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %v", got)
	}
	// But a first-call failure surfaces.
	f2 := &fakeAPI{genErr: errors.New("rate limited")}
	if _, err := New(f2).Generate(3); err == nil {
		t.Fatal("want error when nothing was generated")
	}
}

func TestRefreshCandidatesBurnsAThrowawayNetZero(t *testing.T) {
	// First generate is the sacrificial slot; the rest are the fresh pool.
	f := &fakeAPI{genQueue: []string{"burn@icloud.com", "fresh1@icloud.com", "fresh2@icloud.com", "fresh3@icloud.com"}}
	got, err := New(f).RefreshCandidates(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 3 || got.Candidates[0] != "fresh1@icloud.com" {
		t.Fatalf("fresh pool = %v", got.Candidates)
	}
	// Net-zero: the throwaway was deactivated THEN deleted (Apple's
	// required order) and is gone — no leftover.
	if got.Leftover != "" {
		t.Fatalf("common path must leave no litter, got leftover %q", got.Leftover)
	}
	if len(f.deactived) != 1 || len(f.deleted) != 1 || f.deleted[0] != "id:burn@icloud.com" {
		t.Fatalf("throwaway must be deactivated then deleted: deactived=%v deleted=%v", f.deactived, f.deleted)
	}
}

// When delete fails after deactivate (rare), the throwaway is left
// deactivated and reported — never silently dropped as litter.
func TestRefreshCandidatesSurfacesUndeletableLeftover(t *testing.T) {
	f := &fakeDeleteFailsAPI{fakeAPI: fakeAPI{genQueue: []string{"burn@icloud.com", "fresh1@icloud.com"}}}
	got, err := New(f).RefreshCandidates(1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Leftover != "burn@icloud.com" {
		t.Fatalf("undeletable throwaway must be surfaced, got %q", got.Leftover)
	}
}

// fakeDeleteFailsAPI models a transient delete failure after a
// successful deactivate.
type fakeDeleteFailsAPI struct{ fakeAPI }

func (f *fakeDeleteFailsAPI) DeleteHme(id string) error {
	return errors.New("temporarily unavailable")
}

func TestReserveSerializesTags(t *testing.T) {
	f := &fakeAPI{}
	if _, err := New(f).Reserve("x@icloud.com", "github", []string{"dev", "work"}, "signup"); err != nil {
		t.Fatal(err)
	}
	want := "x@icloud.com|github|#dev #work | signup"
	if len(f.reserved) != 1 || f.reserved[0] != want {
		t.Fatalf("reserved = %v, want %q", f.reserved, want)
	}
}

func TestDeactivateAlreadyInactive(t *testing.T) {
	f := &fakeAPI{emails: []api.HmeEmail{{AnonymousID: "abc123", Label: "github", Hme: "x@icloud.com", IsActive: false}}}
	hme, changed, err := New(f).Deactivate("github")
	if err != nil {
		t.Fatal(err)
	}
	if changed || hme.Hme != "x@icloud.com" {
		t.Fatalf("changed=%v hme=%+v", changed, hme)
	}
	if len(f.deactived) != 0 {
		t.Fatal("API called for already-inactive address")
	}
}

func TestDeactivateActive(t *testing.T) {
	f := &fakeAPI{emails: []api.HmeEmail{{AnonymousID: "abc123", Label: "github", IsActive: true}}}
	_, changed, err := New(f).Deactivate("github")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if len(f.deactived) != 1 || f.deactived[0] != "abc123" {
		t.Fatalf("deactivated = %v", f.deactived)
	}
}

func TestUpdateMetaPreservesUnspecified(t *testing.T) {
	f := &fakeAPI{emails: []api.HmeEmail{{
		AnonymousID: "abc123", Label: "github", Note: "#dev | old note", IsActive: true,
	}}}
	note := "new note"
	if _, err := New(f).UpdateMeta("github", MetaPatch{Note: &note}); err != nil {
		t.Fatal(err)
	}
	want := "abc123|github|#dev | new note"
	if len(f.updates) != 1 || f.updates[0] != want {
		t.Fatalf("update = %v, want %q", f.updates, want)
	}
	// Tags replace, not append.
	newTags := []string{"work"}
	if _, err := New(f).UpdateMeta("github", MetaPatch{Tags: &newTags}); err != nil {
		t.Fatal(err)
	}
	want = "abc123|github|#work | old note"
	if f.updates[1] != want {
		t.Fatalf("update = %v, want %q", f.updates[1], want)
	}
}

func TestListAppliesFilters(t *testing.T) {
	f := &fakeAPI{emails: []api.HmeEmail{
		{Label: "github", IsActive: true},
		{Label: "netflix", IsActive: false},
	}}
	got, err := New(f).List(filter.Options{Active: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Label != "github" {
		t.Fatalf("filtered = %+v", got)
	}
}
