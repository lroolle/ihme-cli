package resolver

import (
	"testing"

	"github.com/lroolle/ihme-cli/api"
)

var testEmails = []api.HmeEmail{
	{AnonymousID: "abc123full", Hme: "abc@privaterelay.appleid.com", Label: "github.com"},
	{AnonymousID: "def456full", Hme: "def@privaterelay.appleid.com", Label: "amazon.com"},
	{AnonymousID: "ghi789full", Hme: "ghi@privaterelay.appleid.com", Label: "netflix.com"},
	{AnonymousID: "abc123other", Hme: "xyz@privaterelay.appleid.com", Label: "gitlab.com"},
}

func TestResolveByID(t *testing.T) {
	hme, err := Resolve("abc123full", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.Label != "github.com" {
		t.Errorf("got label %q, want github.com", hme.Label)
	}
}

func TestResolveByEmail(t *testing.T) {
	hme, err := Resolve("def@privaterelay.appleid.com", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.Label != "amazon.com" {
		t.Errorf("got label %q, want amazon.com", hme.Label)
	}
}

func TestResolveByExactLabel(t *testing.T) {
	hme, err := Resolve("netflix.com", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.AnonymousID != "ghi789full" {
		t.Errorf("got ID %q, want ghi789full", hme.AnonymousID)
	}
}

func TestResolveByLabelCaseInsensitive(t *testing.T) {
	hme, err := Resolve("GitHub.com", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.AnonymousID != "abc123full" {
		t.Errorf("got ID %q, want abc123full", hme.AnonymousID)
	}
}

func TestResolveFuzzyLabel(t *testing.T) {
	hme, err := Resolve("netflix", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.AnonymousID != "ghi789full" {
		t.Errorf("got ID %q, want ghi789full", hme.AnonymousID)
	}
}

func TestResolveByIDPrefix(t *testing.T) {
	hme, err := Resolve("def456", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.AnonymousID != "def456full" {
		t.Errorf("got ID %q, want def456full", hme.AnonymousID)
	}
}

func TestResolveByIDPrefixAmbiguous(t *testing.T) {
	_, err := Resolve("abc123", testEmails)
	if err == nil {
		t.Error("expected error for ambiguous prefix matching abc123full and abc123other")
	}
}

func TestResolveByIDPrefixTooShort(t *testing.T) {
	_, err := Resolve("def45", testEmails)
	if err == nil {
		t.Error("expected error: prefix under 6 chars should not match")
	}
}

func TestResolveNotFound(t *testing.T) {
	_, err := Resolve("nonexistent", testEmails)
	if err == nil {
		t.Error("expected error for nonexistent ref")
	}
}

func TestResolveEmpty(t *testing.T) {
	_, err := Resolve("", testEmails)
	if err == nil {
		t.Error("expected error for empty ref")
	}
}
