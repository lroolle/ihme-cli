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
	return &api.HmeEmail{Hme: hme, Label: label, Note: note, IsActive: true}, nil
}
func (f *fakeAPI) UpdateHmeMetadata(id, label, note string) error {
	f.updates = append(f.updates, id+"|"+label+"|"+note)
	return nil
}
func (f *fakeAPI) DeactivateHme(id string) error {
	f.deactived = append(f.deactived, id)
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
