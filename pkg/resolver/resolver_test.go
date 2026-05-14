package resolver

import (
	"testing"

	"github.com/lroolle/ihme-cli/api"
)

var testEmails = []api.HmeEmail{
	{AnonymousID: "abc123", Hme: "abc@privaterelay.appleid.com", Label: "github.com"},
	{AnonymousID: "def456", Hme: "def@privaterelay.appleid.com", Label: "amazon.com"},
	{AnonymousID: "ghi789", Hme: "ghi@privaterelay.appleid.com", Label: "netflix.com"},
}

func TestResolveByID(t *testing.T) {
	hme, err := Resolve("abc123", testEmails)
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
	if hme.AnonymousID != "ghi789" {
		t.Errorf("got ID %q, want ghi789", hme.AnonymousID)
	}
}

func TestResolveByLabelCaseInsensitive(t *testing.T) {
	hme, err := Resolve("GitHub.com", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.AnonymousID != "abc123" {
		t.Errorf("got ID %q, want abc123", hme.AnonymousID)
	}
}

func TestResolveFuzzyLabel(t *testing.T) {
	hme, err := Resolve("netflix", testEmails)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hme.AnonymousID != "ghi789" {
		t.Errorf("got ID %q, want ghi789", hme.AnonymousID)
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
