package shares

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrantStoresOnlyTheHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	key, rotated, err := Grant(path, "Brisk.Heron_0q@icloud.com", "abc", "netflix", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rotated {
		t.Error("first grant cannot rotate")
	}
	if len(key) != 26 || strings.ToLower(key) != key {
		t.Errorf("key %q: want 26 lowercase base32 chars", key)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), key) {
		t.Error("the key itself reached the file")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
	list, _ := Load(path)
	s, ok := Lookup(list, key)
	if !ok || s.Address != "brisk.heron_0q@icloud.com" {
		t.Fatalf("lookup = %+v, %v", s, ok)
	}
}

func TestLookupForgivesRetyping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	key, _, _ := Grant(path, "a@icloud.com", "", "", time.Now())
	list, _ := Load(path)
	retyped := strings.ToUpper(key[:13]) + " - " + key[13:] + "\n"
	if _, ok := Lookup(list, retyped); !ok {
		t.Errorf("retyped key %q did not open the share", retyped)
	}
	for _, bad := range []string{"", "   ", key[:25], key + "x"} {
		if _, ok := Lookup(list, bad); ok {
			t.Errorf("key %q opened a share", bad)
		}
	}
}

func TestGrantAgainRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	old, _, _ := Grant(path, "a@icloud.com", "", "", time.Now())
	Grant(path, "b@icloud.com", "", "", time.Now())
	fresh, rotated, err := Grant(path, "A@icloud.com", "", "", time.Now())
	if err != nil || !rotated {
		t.Fatalf("regrant: rotated=%v err=%v", rotated, err)
	}
	list, _ := Load(path)
	if len(list) != 2 {
		t.Fatalf("one share per address: got %d", len(list))
	}
	if _, ok := Lookup(list, old); ok {
		t.Error("the rotated key still opens")
	}
	if _, ok := Lookup(list, fresh); !ok {
		t.Error("the new key does not open")
	}
}

func TestRevoke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shares.json")
	key, _, _ := Grant(path, "a@icloud.com", "", "", time.Now())
	Grant(path, "b@icloud.com", "", "", time.Now())
	removed, err := Revoke(path, func(s Share) bool { return s.Address == "a@icloud.com" })
	if err != nil || len(removed) != 1 {
		t.Fatalf("revoke: %v %v", removed, err)
	}
	list, _ := Load(path)
	if _, ok := Lookup(list, key); ok {
		t.Error("a revoked key still opens")
	}
	if len(list) != 1 {
		t.Errorf("the other share went too: %v", list)
	}
	if removed, _ := Revoke(path, func(Share) bool { return false }); removed != nil {
		t.Error("revoking nothing removed something")
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	list, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || list != nil {
		t.Errorf("missing file: %v %v", list, err)
	}
}

func TestDefaultPathFollowsSession(t *testing.T) {
	t.Setenv("IHME_SHARES_PATH", "")
	if got := DefaultPath("/x/ihme/session.json"); got != "/x/ihme/shares.json" {
		t.Errorf("DefaultPath = %q", got)
	}
	t.Setenv("IHME_SHARES_PATH", "/y/s.json")
	if got := DefaultPath("/x/ihme/session.json"); got != "/y/s.json" {
		t.Errorf("override ignored: %q", got)
	}
}
