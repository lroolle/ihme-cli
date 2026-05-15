package filter

import (
	"testing"

	"github.com/lroolle/ihme-cli/api"
)

var testEmails = []api.HmeEmail{
	{AnonymousID: "1", Hme: "a@relay.com", Label: "github.com", IsActive: true, Note: "#dev | main"},
	{AnonymousID: "2", Hme: "b@relay.com", Label: "amazon.com", IsActive: true, Note: "#shopping"},
	{AnonymousID: "3", Hme: "c@relay.com", Label: "old-site.com", IsActive: false, Note: "#dev | deprecated"},
	{AnonymousID: "4", Hme: "d@relay.com", Label: "newsletter", IsActive: false, Note: ""},
}

func TestFilterNone(t *testing.T) {
	result := Apply(testEmails, Options{})
	if len(result) != 4 {
		t.Errorf("no filter: got %d, want 4", len(result))
	}
}

func TestFilterActive(t *testing.T) {
	result := Apply(testEmails, Options{Active: true})
	if len(result) != 2 {
		t.Errorf("active: got %d, want 2", len(result))
	}
	for _, e := range result {
		if !e.IsActive {
			t.Errorf("inactive email in active filter: %s", e.Label)
		}
	}
}

func TestFilterInactive(t *testing.T) {
	result := Apply(testEmails, Options{Inactive: true})
	if len(result) != 2 {
		t.Errorf("inactive: got %d, want 2", len(result))
	}
	for _, e := range result {
		if e.IsActive {
			t.Errorf("active email in inactive filter: %s", e.Label)
		}
	}
}

func TestFilterByTag(t *testing.T) {
	result := Apply(testEmails, Options{Tag: "dev"})
	if len(result) != 2 {
		t.Errorf("tag=dev: got %d, want 2", len(result))
	}
}

func TestFilterByTagNoMatch(t *testing.T) {
	result := Apply(testEmails, Options{Tag: "nonexistent"})
	if len(result) != 0 {
		t.Errorf("tag=nonexistent: got %d, want 0", len(result))
	}
}

func TestFilterCombined(t *testing.T) {
	result := Apply(testEmails, Options{Active: true, Tag: "dev"})
	if len(result) != 1 {
		t.Errorf("active+tag=dev: got %d, want 1", len(result))
	}
	if result[0].Label != "github.com" {
		t.Errorf("expected github.com, got %s", result[0].Label)
	}
}

func TestFilterSearch(t *testing.T) {
	result := Apply(testEmails, Options{Search: "github"})
	if len(result) != 1 || result[0].Label != "github.com" {
		t.Errorf("search 'github': got %d results", len(result))
	}
}

func TestFilterSearchCaseInsensitive(t *testing.T) {
	result := Apply(testEmails, Options{Search: "AMAZON"})
	if len(result) != 1 || result[0].Label != "amazon.com" {
		t.Errorf("search 'AMAZON': got %d results", len(result))
	}
}

func TestFilterSearchByEmail(t *testing.T) {
	result := Apply(testEmails, Options{Search: "b@relay"})
	if len(result) != 1 || result[0].Label != "amazon.com" {
		t.Errorf("search by email: got %d results", len(result))
	}
}

func TestFilterSearchByNote(t *testing.T) {
	result := Apply(testEmails, Options{Search: "deprecated"})
	if len(result) != 1 || result[0].Label != "old-site.com" {
		t.Errorf("search by note: got %d results", len(result))
	}
}

func TestFilterSearchNoMatch(t *testing.T) {
	result := Apply(testEmails, Options{Search: "nonexistent"})
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestSortByLabel(t *testing.T) {
	result := Apply(testEmails, Options{Sort: "label"})
	if result[0].Label != "amazon.com" {
		t.Errorf("first by label should be amazon.com, got %s", result[0].Label)
	}
}

func TestSortByLabelDesc(t *testing.T) {
	result := Apply(testEmails, Options{Sort: "label:desc"})
	if result[0].Label != "old-site.com" {
		t.Errorf("first by label:desc should be old-site.com, got %s", result[0].Label)
	}
}

func TestFilterEmpty(t *testing.T) {
	result := Apply(nil, Options{Active: true})
	if len(result) != 0 {
		t.Errorf("nil input: got %d, want 0", len(result))
	}
}
