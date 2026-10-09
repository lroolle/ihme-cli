// Package inboxkey mints the access keys that ihme-inbox checks.
//
// ihme-inbox (github.com/lroolle/ihme-inbox) is the web inbox for
// shared addresses, and it owns the key scheme (its package key, v1).
// This is the minting half, kept as a copy instead of an import so
// the CLI does not carry a mail server's dependency tree.
// testdata/v1.json is ihme-inbox's own vector file: if the two ever
// disagree, the test here fails.
//
//	secret  32 random bytes, written as 52 lowercase base32 characters
//	key     base32( minted || mac ), 32 characters
//	minted  Unix seconds, 4 bytes big-endian
//	mac     HMAC-SHA256(secret, "ihme-inbox/key/v1" 0x00 address 0x00 minted)[:16]
//	address trimmed, ASCII only, letters lowercased
//
// A key is not stored and the server is not told about it: the server
// holds the same secret and recomputes the MAC. Whoever holds the
// secret can make a key for any address, so it is kept like a
// password, in a file only its owner can read.
package inboxkey

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	secretSize = 32
	macSize    = 16
	label      = "ihme-inbox/key/v1\x00"
	defaultURL = "http://127.0.0.1:8025"
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrNoSecret is a machine that has never been given the inbox
// secret. Only `ihme share secret` creates one: a share that made its
// own would print keys the server refuses, with nothing to say why.
var ErrNoSecret = errors.New("no inbox secret on this machine\n\n" +
	"  Fix: ihme share secret\n" +
	"  It creates one and prints the line your ihme-inbox server needs.\n" +
	"  If a server already runs, use its secret instead: export IHME_INBOX_SECRET=<its value>")

// Grant is what a share hands out: the key, and the link that carries it.
type Grant struct {
	// Key is written in dashed fours, the way it is read aloud.
	Key      string    `json:"key"`
	Link     string    `json:"link"`
	MintedAt time.Time `json:"mintedAt"`
}

// Text is how a grant reads on a terminal, after the line that names
// the address.
func (g *Grant) Text(address string) string {
	return fmt.Sprintf("\n  Key   %s\n  Link  %s\n\n"+
		"Whoever has the key or the link reads this address's mail in ihme-inbox,\n"+
		"and nothing else. Revoke, on the server: ihme-inbox revoke %s\n", g.Key, g.Link, address)
}

// Issue mints a key for address now. baseURL is where ihme-inbox is
// served; empty means $IHME_INBOX_URL, else this machine.
func Issue(address, baseURL string, now time.Time) (*Grant, error) {
	_, secret, _, err := LoadSecret()
	if err != nil {
		return nil, err
	}
	a, err := Canonical(address)
	if err != nil {
		return nil, err
	}
	k, err := Mint(secret, a, now)
	if err != nil {
		return nil, err
	}
	return &Grant{Key: Group(k), Link: link(baseURL, a, k), MintedAt: now.UTC().Truncate(time.Second)}, nil
}

func link(baseURL, address, k string) string {
	if baseURL == "" {
		baseURL = os.Getenv("IHME_INBOX_URL")
	}
	if baseURL == "" {
		baseURL = os.Getenv("IHME_SERVE_URL") // its name through v0.7.0
	}
	if baseURL == "" {
		baseURL = defaultURL
	}
	return strings.TrimRight(baseURL, "/") + "/k/" + url.PathEscape(address) + "/" + k
}

// Dir is ihme-inbox's own directory, so that this machine has one
// secret whichever program reads it: $IHME_INBOX_DIR, else
// $XDG_CONFIG_HOME/ihme-inbox, else ~/.config/ihme-inbox. With no
// home directory it is an error, never the current directory: a
// secret must not land wherever the command happened to run.
func Dir() (string, error) {
	if d := os.Getenv("IHME_INBOX_DIR"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ihme-inbox"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("no home directory to keep the inbox secret in: set IHME_INBOX_DIR")
	}
	return filepath.Join(home, ".config", "ihme-inbox"), nil
}

// LoadSecret finds the secret: $IHME_INBOX_SECRET, else the file
// "secret" in Dir. from names where it came from.
func LoadSecret() (text string, secret []byte, from string, err error) {
	text, from = os.Getenv("IHME_INBOX_SECRET"), "IHME_INBOX_SECRET"
	inFile := text == ""
	if inFile {
		dir, derr := Dir()
		if derr != nil {
			return "", nil, "", derr
		}
		from = filepath.Join(dir, "secret")
		data, rerr := os.ReadFile(from)
		if errors.Is(rerr, fs.ErrNotExist) {
			return "", nil, from, ErrNoSecret
		}
		if rerr != nil {
			return "", nil, from, fmt.Errorf("reading the inbox secret: %w", rerr)
		}
		text = string(data)
	}
	text = Normalize(text)
	secret, err = ParseSecret(text)
	switch {
	case err != nil && inFile:
		return "", nil, from, fmt.Errorf("%s: %w\n\n"+
			"  Restore the file, or copy the secret back from your server's IHME_INBOX_SECRET.\n"+
			"  Deleting it and running 'ihme share secret' makes a new secret: every key handed out stops working", from, err)
	case err != nil:
		return "", nil, from, fmt.Errorf("%s: %w", from, err)
	}
	return text, secret, from, nil
}

// CreateSecret makes this machine's secret file, for its owner only.
// The file is written whole under another name and then linked into
// place, so it is never seen empty or half-written, and when two first
// runs race, the loser reads the winner's secret instead of keeping
// its own.
func CreateSecret() (text, path string, err error) {
	dir, err := Dir()
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(dir, "secret")
	raw := make([]byte, secretSize)
	if _, err := rand.Read(raw); err != nil {
		return "", path, fmt.Errorf("generating the inbox secret: %w", err)
	}
	text = strings.ToLower(encoding.EncodeToString(raw))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", path, fmt.Errorf("saving the inbox secret: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "secret-*.tmp") // 0600
	if err != nil {
		return "", path, fmt.Errorf("saving the inbox secret: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text + "\n"); err != nil {
		tmp.Close()
		return "", path, fmt.Errorf("saving the inbox secret: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", path, fmt.Errorf("saving the inbox secret: %w", err)
	}
	switch err := os.Link(tmp.Name(), path); {
	case errors.Is(err, fs.ErrExist):
		text, _, _, err = LoadSecret()
		return text, path, err
	case err != nil:
		return "", path, fmt.Errorf("saving the inbox secret: %w", err)
	}
	return text, path, nil
}

// ParseSecret reads a secret's written form. Anything that is not 32
// bytes of base32 is refused: a short or hand-typed secret would make
// every key guessable.
func ParseSecret(s string) ([]byte, error) {
	text := Normalize(s)
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 { // Unicode has letters that uppercase into ASCII ones
			return nil, errors.New("not an inbox secret (52 base32 characters, as 'ihme share secret' prints)")
		}
	}
	text = strings.ToUpper(text)
	b, err := encoding.DecodeString(text)
	// The decoder lets spare bits through; a secret has one spelling.
	if err != nil || len(b) != secretSize || encoding.EncodeToString(b) != text {
		return nil, errors.New("not an inbox secret (52 base32 characters, as 'ihme share secret' prints)")
	}
	return b, nil
}

// Canonical is the form of an address a key is bound to: trimmed, with
// letters lowercased. Printable ASCII only, as ihme-inbox requires.
func Canonical(address string) (string, error) {
	a := strings.TrimSpace(address)
	at := strings.LastIndexByte(a, '@')
	if at <= 0 || at == len(a)-1 || len(a) > 254 {
		return "", fmt.Errorf("%q is not an email address", address)
	}
	b := []byte(a)
	for i, c := range b {
		switch {
		case c <= ' ' || c >= 0x7f:
			return "", fmt.Errorf("%q is not an email address ihme-inbox can make a key for (printable ASCII only)", address)
		case c >= 'A' && c <= 'Z':
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b), nil
}

// Mint returns the key that opens address, minted at at.
func Mint(secret []byte, address string, at time.Time) (string, error) {
	if len(secret) != secretSize {
		return "", errors.New("inbox key: wrong secret size")
	}
	a, err := Canonical(address)
	if err != nil {
		return "", err
	}
	sec := at.Unix()
	if sec < 0 || sec > math.MaxUint32 {
		return "", errors.New("inbox key: mint time out of range")
	}
	raw := binary.BigEndian.AppendUint32(make([]byte, 0, 4+macSize), uint32(sec))
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(label))
	h.Write([]byte(a))
	h.Write([]byte{0})
	h.Write(raw)
	raw = append(raw, h.Sum(nil)[:macSize]...)
	return strings.ToLower(encoding.EncodeToString(raw)), nil
}

// Normalize forgives how people retype a key or a secret: case,
// spaces, dashes, line breaks.
func Normalize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '-' || r == '\t' || r == '\n' || r == '\r':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// Group writes a key in fours: "abcd-efgh-...".
func Group(key string) string {
	key = Normalize(key)
	var b strings.Builder
	for i := 0; i < len(key); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(key[i:min(i+4, len(key))])
	}
	return b.String()
}
