package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mailbox is the one IMAP login 'ihme serve' reads mail with.
type mailbox struct {
	Server   string // host:port, implicit TLS
	User     string
	Password string
	From     string // where the login came from: envSource or the em file path
}

const envSource = "IHME_IMAP_*"

// emAccount is the part of em's accounts.json that serve needs. em
// verifies a login against the live server before it saves one, so
// an entry there is a known-good IMAP credential. ihme only reads
// the file.
type emAccount struct {
	Email    string `json:"email"`
	AuthType string `json:"auth_type"`
	Auth     struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"auth"`
}

// emAccountsPath mirrors em's own config dir: $XDG_CONFIG_HOME/em,
// else ~/.config/em, on every OS.
func emAccountsPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "em", "accounts.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "em", "accounts.json")
}

// resolveMailbox picks the login, most explicit first: --account
// names an em account; else IHME_IMAP_USER/IHME_IMAP_PASSWORD; else
// em's account, when it has exactly one. Half-set env vars are an
// error, never a quiet fall back to em.
func resolveMailbox(account string) (mailbox, error) {
	path := emAccountsPath()
	if account == "" && (os.Getenv("IHME_IMAP_USER") != "" || os.Getenv("IHME_IMAP_PASSWORD") != "") {
		return envMailbox()
	}

	all, err := loadEmAccounts(path)
	if err != nil {
		return mailbox{}, err
	}
	if account != "" {
		for _, a := range all {
			if strings.EqualFold(a.Email, account) || strings.EqualFold(a.Auth.Username, account) {
				return a.mailbox(path), nil
			}
		}
		if len(all) == 0 {
			return mailbox{}, fmt.Errorf("--account %s: no IMAP accounts in %s", account, path)
		}
		return mailbox{}, fmt.Errorf("--account %s: not in %s\n\n  Accounts there: %s", account, path, emails(all))
	}

	switch len(all) {
	case 0:
		return mailbox{}, errors.New("no mailbox to read: set IHME_IMAP_USER and IHME_IMAP_PASSWORD" + credentialHelp)
	case 1:
		return all[0].mailbox(path), nil
	default:
		return mailbox{}, fmt.Errorf("%s has %d accounts; pick one with --account\n\n  Accounts: %s\n  Example: ihme serve --account %s",
			path, len(all), emails(all), all[0].Email)
	}
}

const credentialHelp = `

  IHME_IMAP_USER is your @icloud.com address; IHME_IMAP_PASSWORD is an
  app-specific password from account.apple.com > Sign-In and Security.
  Example: IHME_IMAP_USER=me@icloud.com IHME_IMAP_PASSWORD=abcd-efgh-ijkl-mnop ihme serve
  Or, with em installed: em login icloud, then ihme serve`

func envMailbox() (mailbox, error) {
	user, password := os.Getenv("IHME_IMAP_USER"), os.Getenv("IHME_IMAP_PASSWORD")
	switch {
	case user == "":
		return mailbox{}, errors.New("IHME_IMAP_PASSWORD is set but IHME_IMAP_USER is not" + credentialHelp)
	case password == "":
		return mailbox{}, errors.New("IHME_IMAP_USER is set but IHME_IMAP_PASSWORD is not" + credentialHelp)
	}
	server := os.Getenv("IHME_IMAP_SERVER")
	if server == "" {
		server = defaultIMAPServer
	}
	return mailbox{Server: server, User: user, Password: password, From: envSource}, nil
}

// loadEmAccounts returns the accounts serve can log in with: password
// IMAP logins. A missing file is no accounts; OAuth entries carry no
// password and are skipped.
func loadEmAccounts(path string) ([]emAccount, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var all []emAccount
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var usable []emAccount
	for _, a := range all {
		if a.AuthType == "imap" && a.Auth.Host != "" && a.Auth.Password != "" {
			usable = append(usable, a)
		}
	}
	return usable, nil
}

func (a emAccount) mailbox(path string) mailbox {
	port := a.Auth.Port
	if port == 0 {
		port = 993
	}
	user := a.Auth.Username
	if user == "" {
		user = a.Email
	}
	return mailbox{
		Server:   net.JoinHostPort(a.Auth.Host, strconv.Itoa(port)),
		User:     user,
		Password: a.Auth.Password,
		From:     path,
	}
}

func emails(all []emAccount) string {
	out := make([]string, len(all))
	for i, a := range all {
		out[i] = a.Email
	}
	return strings.Join(out, ", ")
}
