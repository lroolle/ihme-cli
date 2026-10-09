package root

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lroolle/ihme-cli/internal/inboxkey"
)

const alias = "brisk.heron_0q@icloud.com"

// icloud is a stand-in for the HME service with one address to hand
// out and, once reserved, to list.
type icloud struct {
	*httptest.Server
	calls, reserves atomic.Int32
}

func newICloud(t *testing.T) *icloud {
	t.Helper()
	s := &icloud{}
	hme := fmt.Sprintf(`{"anonymousId":"abc123def456","hme":%q,"label":"netflix","isActive":true,"createTimestamp":1791504000000}`, alias)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/hme/generate"):
			fmt.Fprintf(w, `{"success":true,"result":{"hme":%q}}`, alias)
		case strings.HasSuffix(r.URL.Path, "/v1/hme/reserve"):
			s.reserves.Add(1)
			fmt.Fprintf(w, `{"success":true,"result":{"hme":%s}}`, hme)
		case strings.HasSuffix(r.URL.Path, "/v2/hme/list"):
			fmt.Fprintf(w, `{"success":true,"result":{"hmeEmails":[%s]}}`, hme)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// signedIn points this process at a fresh home whose saved session
// (confirmed a moment ago, so nothing revalidates) talks to s. The
// session carries no session or trust token on purpose: if s ever
// answered 401, recovery would stop there instead of posting to
// Apple's real accountLogin. These tests cannot reach Apple.
func signedIn(t *testing.T, s *icloud) {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_DATA_HOME": filepath.Join(home, "data"),
		"IHME_SESSION_PATH": filepath.Join(home, "session.json"), "IHME_MEMORY_PATH": filepath.Join(home, "memory"),
		"IHME_INBOX_DIR": filepath.Join(home, "inbox"), "IHME_INBOX_SECRET": "", "IHME_INBOX_URL": "", "IHME_SERVE_URL": "",
		"IHME_NO_PROMPT": "1",
	} {
		t.Setenv(k, v)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	session := fmt.Sprintf(`{"appleId":"me@example.com","sessionToken":"","trustToken":"","dsid":"111",
	  "webservices":{"premiummailsettings":{"url":%q,"status":"active"}},
	  "cookies":[{"name":"X-APPLE-WEBAUTH-TOKEN","value":"v","domain":".icloud.com","path":"/"}],
	  "clientId":"test-client","savedAt":%q,"validatedAt":%q}`, s.URL, now, now)
	if err := os.WriteFile(filepath.Join(home, "session.json"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ihme runs one command line and returns what it printed to stdout.
func ihme(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	real := os.Stdout
	os.Stdout = w
	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()
	cmd := NewCmdRoot("test")
	cmd.SetArgs(args)
	cmd.SetErr(io.Discard)
	err = cmd.Execute()
	os.Stdout = real
	w.Close()
	return <-out, err
}

// opens reports whether key is the one ihme-inbox would accept for
// address at minted: the server recomputes exactly this.
func opens(t *testing.T, address, key string, minted time.Time) bool {
	t.Helper()
	_, secret, _, err := inboxkey.LoadSecret()
	if err != nil {
		t.Fatal(err)
	}
	want, err := inboxkey.Mint(secret, address, minted)
	return err == nil && inboxkey.Normalize(key) == want
}

// The feature: one command makes the address and hands back what
// opens its mail, without a word to the server.
func TestNewShareReservesAndPrintsTheKey(t *testing.T) {
	s := newICloud(t)
	signedIn(t, s)
	if _, err := ihme(t, "share", "secret"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IHME_INBOX_URL", "https://mail.example.com")

	out, err := ihme(t, "new", "netflix", "--share", "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hme, Label, AnonymousID string
		Share                   *inboxkey.Grant
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Share == nil {
		t.Fatalf("output is not a reservation with a share: %v\n%s", err, out)
	}
	if got.Hme != alias || got.Label != "netflix" || got.AnonymousID == "" {
		t.Errorf("reservation fields lost beside the share: %+v", got)
	}
	if !opens(t, alias, got.Share.Key, got.Share.MintedAt) {
		t.Errorf("the printed key does not open %s", alias)
	}
	if want := "https://mail.example.com/k/" + alias + "/" + inboxkey.Normalize(got.Share.Key); got.Share.Link != want {
		t.Errorf("link = %q, want %q", got.Share.Link, want)
	}

	// For a person: the address, then the key and the link.
	out, err = ihme(t, "new", "netflix", "--share", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Reserved: " + alias + " (label: netflix)", "  Key   ", "  Link  https://mail.example.com/k/" + alias + "/", "ihme-inbox revoke " + alias} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// Without --share, exactly what it printed before.
	out, _ = ihme(t, "new", "netflix", "--yes", "--json")
	if strings.Contains(out, `"share"`) || !strings.Contains(out, alias) {
		t.Errorf("new without --share changed:\n%s", out)
	}
}

// --jq asks for machine output as --json does. Alone in a pipe it used
// to fall through to "no terminal: reserve the first candidate".
func TestNewWithJQAloneReservesNothing(t *testing.T) {
	s := newICloud(t)
	signedIn(t, s)
	ihme(t, "new", "netflix", "--jq", ".candidates") // jq itself may be absent; the reservation is the point
	if n := s.reserves.Load(); n != 0 {
		t.Errorf("`new --jq .candidates` reserved %d address(es)", n)
	}
}

// An address that was made and then could not be shared is a surprise
// to clean up, so everything --share needs is checked first.
func TestNewShareFailsBeforeReserving(t *testing.T) {
	s := newICloud(t)
	signedIn(t, s)

	_, err := ihme(t, "new", "netflix", "--share", "--yes", "--json")
	if err == nil || !strings.Contains(err.Error(), "ihme share secret") {
		t.Errorf("no secret: %v; want how to make one", err)
	}
	if _, err := ihme(t, "share", "secret"); err != nil {
		t.Fatal(err)
	}
	for _, machine := range [][]string{{"--json"}, {"--jq", ".candidates"}} {
		_, err = ihme(t, append([]string{"new", "netflix", "--share"}, machine...)...)
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Errorf("--share with candidates-only %v: %v", machine, err)
		}
	}
	if n := s.calls.Load(); n != 0 {
		t.Errorf("%d calls reached iCloud before the checks failed (%d reserves)", n, s.reserves.Load())
	}
}

func TestShareAnExistingAddress(t *testing.T) {
	s := newICloud(t)
	signedIn(t, s)

	if _, err := ihme(t, "share", "netflix", "--json"); err == nil || !strings.Contains(err.Error(), "ihme share secret") || s.calls.Load() != 0 {
		t.Fatalf("share with no secret: %v after %d iCloud calls", err, s.calls.Load())
	}
	if _, err := ihme(t, "share", "secret"); err != nil {
		t.Fatal(err)
	}
	out, err := ihme(t, "share", "netflix", "--json", "--url", "https://mail.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Address, Label, Key, Link string
		MintedAt                  time.Time
		Hints                     map[string]string
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.Address != alias || got.Label != "netflix" || !opens(t, alias, got.Key, got.MintedAt) {
		t.Errorf("share = %+v", got)
	}
	if !strings.HasPrefix(got.Link, "https://mail.example.com/k/"+alias+"/") || got.Hints["revoke"] != "ihme-inbox revoke "+alias {
		t.Errorf("link or hints wrong: %+v", got)
	}
	if s.reserves.Load() != 0 {
		t.Error("sharing reserved something")
	}
}

func TestShareSecretPrintsOneStableLine(t *testing.T) {
	signedIn(t, newICloud(t))
	first, err := ihme(t, "share", "secret")
	if err != nil || !strings.HasPrefix(first, "IHME_INBOX_SECRET=") || strings.Count(first, "\n") != 1 {
		t.Fatalf("share secret = %q, %v", first, err)
	}
	if _, err := inboxkey.ParseSecret(strings.TrimPrefix(strings.TrimSpace(first), "IHME_INBOX_SECRET=")); err != nil {
		t.Errorf("the printed secret is not usable: %v", err)
	}
	if again, _ := ihme(t, "share", "secret"); again != first {
		t.Error("a second run printed a different secret")
	}
	var got struct {
		Secret, From string
		Created      bool
	}
	out, err := ihme(t, "share", "secret", "--json")
	if err != nil || json.Unmarshal([]byte(out), &got) != nil || "IHME_INBOX_SECRET="+got.Secret+"\n" != first || got.Created || got.From == "" {
		t.Errorf("share secret --json = %q, %v", out, err)
	}
}

// v0.7.0 had `serve`, `share list`, `share revoke`. They say where
// they went, whatever flags an old script passes.
func TestMovedCommandsSayWhere(t *testing.T) {
	s := newICloud(t)
	signedIn(t, s)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"serve", "--listen", "0.0.0.0:8025", "--days", "14"}, "ihme-inbox serve"},
		{[]string{"share", "list", "--json"}, "ihme share <ref>"},
		{[]string{"share", "revoke", "netflix"}, "ihme-inbox revoke <address>"},
		{[]string{"share", "revoke", "--all"}, "ihme-inbox revoke <address>"},
	} {
		out, err := ihme(t, tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) || out != "" {
			t.Errorf("%v: out=%q err=%v; want an error naming %q", tc.args, out, err, tc.want)
		}
	}
	if s.calls.Load() != 0 {
		t.Error("a moved command called iCloud")
	}
	if out, err := ihme(t, "help", "serve"); err != nil || !strings.Contains(out, "ihme-inbox serve") {
		t.Errorf("help serve = %q, %v; want where it went", out, err)
	}
	help, _ := ihme(t, "--help")
	if strings.Contains(help, "\n  serve ") || !strings.Contains(help, "\n  share ") {
		t.Errorf("help still lists serve:\n%s", help)
	}
}
