package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoAccounts = `[
  {"id": "1", "provider": "icloud", "email": "me@icloud.com", "auth_type": "imap",
   "auth": {"host": "imap.mail.me.com", "port": 993, "username": "me@icloud.com", "password": "icloud-app-pass"}},
  {"id": "2", "provider": "imap", "email": "Work@Example.com", "auth_type": "imap",
   "auth": {"host": "mail.example.com", "port": 1993, "username": "work-login", "password": "work-pass"}},
  {"id": "3", "provider": "gmail", "email": "oauth@gmail.com", "auth_type": "oauth",
   "auth": {"host": "imap.gmail.com", "port": 993, "access_token": "t"}}
]`

// emConfig points em's config dir at a temp dir holding accounts and
// clears the env overrides, so each test sees only what it sets.
func emConfig(t *testing.T, accounts string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("IHME_IMAP_USER", "")
	t.Setenv("IHME_IMAP_PASSWORD", "")
	t.Setenv("IHME_IMAP_SERVER", "")
	path := filepath.Join(dir, "em", "accounts.json")
	if accounts != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(accounts), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestMailboxFromOnlyEmAccount(t *testing.T) {
	path := emConfig(t, `[{"email": "me@icloud.com", "auth_type": "imap",
	  "auth": {"host": "imap.mail.me.com", "password": "app-pass"}}]`)
	box, err := resolveMailbox("")
	if err != nil {
		t.Fatal(err)
	}
	want := mailbox{Server: "imap.mail.me.com:993", User: "me@icloud.com", Password: "app-pass", From: path}
	if box != want {
		t.Errorf("mailbox = %+v, want %+v (port and username default)", box, want)
	}
}

func TestMailboxAccountFlagPicksAmongSeveral(t *testing.T) {
	path := emConfig(t, twoAccounts)
	_, err := resolveMailbox("")
	if err == nil || !strings.Contains(err.Error(), "--account") || !strings.Contains(err.Error(), "Work@Example.com") {
		t.Errorf("several accounts, no --account: err = %v, want a pick-one error naming them", err)
	}
	if strings.Contains(err.Error(), "oauth@gmail.com") {
		t.Errorf("an OAuth account was offered, but serve cannot log in with it: %v", err)
	}

	box, err := resolveMailbox("work@example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := mailbox{Server: "mail.example.com:1993", User: "work-login", Password: "work-pass", From: path}
	if box != want {
		t.Errorf("mailbox = %+v, want %+v", box, want)
	}

	if _, err := resolveMailbox("nobody@icloud.com"); err == nil || !strings.Contains(err.Error(), "me@icloud.com") {
		t.Errorf("unknown --account: err = %v, want the list of accounts", err)
	}
}

func TestMailboxEnvBeatsEmFile(t *testing.T) {
	emConfig(t, twoAccounts)
	t.Setenv("IHME_IMAP_USER", "env@icloud.com")
	t.Setenv("IHME_IMAP_PASSWORD", "env-pass")
	box, err := resolveMailbox("")
	if err != nil {
		t.Fatal(err)
	}
	want := mailbox{Server: defaultIMAPServer, User: "env@icloud.com", Password: "env-pass", From: envSource}
	if box != want {
		t.Errorf("mailbox = %+v, want %+v", box, want)
	}

	// --account is more explicit than the environment.
	box, err = resolveMailbox("me@icloud.com")
	if err != nil || box.User != "me@icloud.com" {
		t.Errorf("--account with env set = %+v, %v; want the em account", box, err)
	}
}

func TestMailboxHalfSetEnvIsAnError(t *testing.T) {
	emConfig(t, `[{"email": "me@icloud.com", "auth_type": "imap",
	  "auth": {"host": "imap.mail.me.com", "password": "app-pass"}}]`)
	t.Setenv("IHME_IMAP_USER", "env@icloud.com")
	if _, err := resolveMailbox(""); err == nil || !strings.Contains(err.Error(), "IHME_IMAP_PASSWORD is not") {
		t.Errorf("user without password fell back to em: err = %v", err)
	}
}

func TestMailboxNothingConfigured(t *testing.T) {
	emConfig(t, "")
	_, err := resolveMailbox("")
	if err == nil || !strings.Contains(err.Error(), "IHME_IMAP_USER") {
		t.Errorf("err = %v, want how to set a login", err)
	}

	emConfig(t, `{not json`)
	if _, err := resolveMailbox(""); err == nil || !strings.Contains(err.Error(), "accounts.json") {
		t.Errorf("broken em file: err = %v, want its path", err)
	}
}
