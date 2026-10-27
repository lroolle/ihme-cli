package inboxkey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavedURLPrecedenceAndSecretUnchanged(t *testing.T) {
	dir := machine(t)
	secret, _, err := CreateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveURL("https://saved.example.com/"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ flag, env, legacy, want string }{
		{"", "", "", "https://saved.example.com"},
		{"", "", "https://legacy.example.com", "https://legacy.example.com"},
		{"", "https://env.example.com", "https://legacy.example.com", "https://env.example.com"},
		{"https://flag.example.com/", "https://env.example.com", "", "https://flag.example.com"},
	} {
		t.Setenv("IHME_INBOX_URL", tc.env)
		t.Setenv("IHME_SERVE_URL", tc.legacy)
		got, err := BaseURL(tc.flag)
		if err != nil || got != tc.want {
			t.Errorf("URL = %q, %v; want %q", got, err, tc.want)
		}
	}
	if got, _, _, err := LoadSecret(); err != nil || got != secret {
		t.Fatal("saving URL changed secret", err)
	}
	if stat, err := os.Stat(filepath.Join(dir, "url")); err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatal("URL file permissions", err)
	}
}

func TestBadURLNeverReplacesDefault(t *testing.T) {
	dir := machine(t)
	if _, err := SaveURL("https://saved.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "mail.example.com", "http://mail.example.com", "https://user:password@mail.example.com", "https://mail.example.com?k=secret", "https://mail.example.com#key", "javascript:alert(1)", "https://", "https://mail.example.com?"} {
		if _, err := SaveURL(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got, err := BaseURL(""); err != nil || got != "https://saved.example.com" {
		t.Fatal("bad URL replaced saved default")
	}
	for _, local := range []string{"http://127.0.0.1:8025", "http://localhost:8025", "http://[::1]:8025"} {
		if _, err := SaveURL(local); err != nil {
			t.Errorf("local URL: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "url"), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BaseURL(""); err == nil {
		t.Fatal("bad saved URL silently fell back")
	}
}
