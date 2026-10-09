package inboxkey

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const alias = "brisk.heron_0q@icloud.com"

// machine is one computer with nothing set.
func machine(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IHME_INBOX_DIR", dir)
	t.Setenv("IHME_INBOX_SECRET", "")
	t.Setenv("IHME_INBOX_URL", "")
	t.Setenv("IHME_SERVE_URL", "")
	return dir
}

// testdata/v1.json is ihme-inbox's own file (key/testdata/v1.json
// there), computed outside either program. A key minted here that
// differs from it would be refused by the server.
func TestMintMatchesIhmeInbox(t *testing.T) {
	data, err := os.ReadFile("testdata/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Vectors []struct {
			Secret, Address, Key string
			Minted               int64
		}
	}
	if err := json.Unmarshal(data, &f); err != nil || len(f.Vectors) == 0 {
		t.Fatalf("testdata/v1.json: %v (%d vectors)", err, len(f.Vectors))
	}
	for _, v := range f.Vectors {
		secret, err := ParseSecret(v.Secret)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Mint(secret, v.Address, time.Unix(v.Minted, 0))
		if err != nil || got != v.Key {
			t.Errorf("Mint(%q, %d) = %q, %v; want %q", v.Address, v.Minted, got, err, v.Key)
		}
	}
}

// Only CreateSecret makes a secret. Issue on a fresh machine fails
// with the fix, and leaves no file behind.
func TestIssueNeedsASecretAndNeverInventsOne(t *testing.T) {
	dir := machine(t)
	_, err := Issue(alias, "", time.Now())
	if !errors.Is(err, ErrNoSecret) || !strings.Contains(err.Error(), "ihme share secret") {
		t.Fatalf("Issue with no secret: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secret")); err == nil {
		t.Fatal("Issue created a secret")
	}
}

func TestCreateSecretOnceForItsOwnerOnly(t *testing.T) {
	dir := machine(t)
	text, path, err := CreateSecret()
	if err != nil || path != filepath.Join(dir, "secret") || len(text) != 52 {
		t.Fatalf("CreateSecret = %q, %q, %v", text, path, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	// A second create (two first runs racing) must end with the first
	// one's secret, not a new one: every key handed out would die.
	if again, _, err := CreateSecret(); err != nil || again != text {
		t.Errorf("a second CreateSecret = %q, %v; want the first secret back", again, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
	loaded, _, from, err := LoadSecret()
	if err != nil || loaded != text || from != path {
		t.Errorf("LoadSecret = %q from %q, %v", loaded, from, err)
	}

	// The environment wins, as it does on a server.
	other := strings.Repeat("a", 51) + "q"
	t.Setenv("IHME_INBOX_SECRET", other)
	if loaded, _, from, err := LoadSecret(); err != nil || loaded != other || from != "IHME_INBOX_SECRET" {
		t.Errorf("LoadSecret with env = %q from %q, %v", loaded, from, err)
	}
}

// Eight first runs at once all end up with one secret.
func TestConcurrentFirstRunsAgree(t *testing.T) {
	machine(t)
	got := make(chan string, 8)
	for range 8 {
		go func() {
			text, _, err := CreateSecret()
			if err != nil {
				t.Error(err)
			}
			got <- text
		}()
	}
	first := <-got
	for range 7 {
		if text := <-got; text != first {
			t.Fatalf("two first runs kept different secrets")
		}
	}
	if loaded, _, _, err := LoadSecret(); err != nil || loaded != first {
		t.Errorf("the file holds %q, %v; the runs printed %q", loaded, err, first)
	}
}

// With no home directory the secret must not land in the current
// directory.
func TestNoHomeIsAnError(t *testing.T) {
	for _, k := range []string{"IHME_INBOX_DIR", "IHME_INBOX_SECRET", "XDG_CONFIG_HOME", "HOME", "USERPROFILE", "home"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	if _, _, err := CreateSecret(); err == nil || !strings.Contains(err.Error(), "IHME_INBOX_DIR") {
		t.Errorf("CreateSecret with no home: %v", err)
	}
	if _, _, _, err := LoadSecret(); err == nil || errors.Is(err, ErrNoSecret) {
		t.Errorf("LoadSecret with no home: %v; want an error that is not 'run ihme share secret'", err)
	}
	if left, _ := filepath.Glob("*"); len(left)+len(mustGlob(".config")) != 0 {
		t.Errorf("files appeared in the current directory: %v", left)
	}
}

func mustGlob(p string) []string { m, _ := filepath.Glob(p); return m }

func TestBrokenSecretIsAnError(t *testing.T) {
	dir := machine(t)
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Issue(alias, "", time.Now())
	if err == nil || errors.Is(err, ErrNoSecret) || !strings.Contains(err.Error(), path) {
		t.Errorf("Issue with a broken secret file: %v; want an error naming %s", err, path)
	}
	// The fix it names must not be the command that just failed, and
	// must say what deleting the file costs.
	if !strings.Contains(err.Error(), "every key handed out stops working") {
		t.Errorf("the error does not say what replacing the secret costs: %v", err)
	}
	if _, _, err := CreateSecret(); err == nil {
		t.Error("CreateSecret replaced a file it could not read")
	}
	for _, bad := range []string{"", "hunter2", strings.Repeat("a", 51), strings.Repeat("a", 51) + "b", strings.Repeat("a", 54)} {
		if _, err := ParseSecret(bad); err == nil {
			t.Errorf("ParseSecret(%q) accepted a weak or malformed secret", bad)
		}
	}
}

func TestIssue(t *testing.T) {
	machine(t)
	text, _, err := CreateSecret()
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := ParseSecret(text)
	at := time.Date(2026, 10, 9, 12, 0, 0, 500, time.UTC)

	g, err := Issue(" "+strings.ToUpper(alias), "https://mail.example.com/", at)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Mint(secret, alias, at)
	if Normalize(g.Key) != want || len(g.Key) != 39 || !g.MintedAt.Equal(at.Truncate(time.Second)) {
		t.Errorf("grant = %+v, want key %s", g, want)
	}
	if g.Link != "https://mail.example.com/k/"+alias+"/"+want {
		t.Errorf("link = %q", g.Link)
	}

	// Where the link points: the flag, then IHME_INBOX_URL, then the
	// v0.7.0 name, then this machine.
	for _, tc := range []struct{ env, old, want string }{
		{"", "", "http://127.0.0.1:8025/k/"},
		{"", "https://old.example.com", "https://old.example.com/k/"},
		{"https://new.example.com/", "https://old.example.com", "https://new.example.com/k/"},
	} {
		t.Setenv("IHME_INBOX_URL", tc.env)
		t.Setenv("IHME_SERVE_URL", tc.old)
		g, err := Issue(alias, "", at)
		if err != nil || !strings.HasPrefix(g.Link, tc.want) {
			t.Errorf("env %q/%q: link %q, %v; want prefix %q", tc.env, tc.old, g.Link, err, tc.want)
		}
	}
	// What ihme-inbox would refuse to verify must not be minted.
	for _, bad := range []string{"netflix", "über@example.com", "bris\u212a.heron_0q@icloud.com", "a b@icloud.com"} {
		if _, err := Issue(bad, "", at); err == nil {
			t.Errorf("issued a key for %q", bad)
		}
	}
}
