// Package shares stores the per-address access keys behind
// `ihme serve`. A share lets whoever holds its key read the mail one
// Hide My Email address received, and nothing else.
//
// Only a SHA-256 of each key is stored; the key itself is shown once,
// when `ihme share` mints it. One address has at most one live key:
// sharing it again rotates the key, which is also how a key is reset.
// The file is the whole admin surface: the server only reads it, the
// CLI only writes it, and a revoke takes effect on the next request.
package shares

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Share grants read access to one address's received mail.
type Share struct {
	Address     string    `json:"address"`
	AnonymousID string    `json:"anonymousId,omitempty"`
	Label       string    `json:"label,omitempty"`
	KeyHash     string    `json:"keyHash"`
	CreatedAt   time.Time `json:"createdAt"`
}

type file struct {
	Shares []Share `json:"shares"`
}

// DefaultPath is $IHME_SHARES_PATH, else shares.json beside the
// session file, so IHME_SESSION_PATH isolates both at once.
func DefaultPath(sessionPath string) string {
	if p := os.Getenv("IHME_SHARES_PATH"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(sessionPath), "shares.json")
}

// Load reads the shares file; a missing file is no shares.
func Load(path string) ([]Share, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading shares: %w", err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return f.Shares, nil
}

// Save writes the shares file atomically, readable by the owner only.
func Save(path string, list []Share) error {
	if list == nil {
		list = []Share{}
	}
	data, err := json.MarshalIndent(file{Shares: list}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("writing shares: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "shares-*.tmp")
	if err != nil {
		return fmt.Errorf("writing shares: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("writing shares: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing shares: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing shares: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing shares: %w", err)
	}
	return nil
}

// Grant mints a key for address, replacing any key it had, and
// returns the key. rotated reports that an older key stopped working.
func Grant(path, address, anonymousID, label string, now time.Time) (key string, rotated bool, err error) {
	list, err := Load(path)
	if err != nil {
		return "", false, err
	}
	key, err = newKey()
	if err != nil {
		return "", false, err
	}
	kept := list[:0]
	for _, s := range list {
		if strings.EqualFold(s.Address, address) {
			rotated = true
			continue
		}
		kept = append(kept, s)
	}
	kept = append(kept, Share{
		Address:     strings.ToLower(address),
		AnonymousID: anonymousID,
		Label:       label,
		KeyHash:     Hash(key),
		CreatedAt:   now.UTC(),
	})
	return key, rotated, Save(path, kept)
}

// Revoke removes every share match accepts and returns them.
func Revoke(path string, match func(Share) bool) ([]Share, error) {
	list, err := Load(path)
	if err != nil {
		return nil, err
	}
	var kept, removed []Share
	for _, s := range list {
		if match(s) {
			removed = append(removed, s)
		} else {
			kept = append(kept, s)
		}
	}
	if len(removed) == 0 {
		return nil, nil
	}
	return removed, Save(path, kept)
}

// Lookup returns the share key opens. The comparison is between
// hashes of a 128-bit random key, so a timing side channel has
// nothing to give away.
func Lookup(list []Share, key string) (*Share, bool) {
	if Normalize(key) == "" {
		return nil, false
	}
	h := Hash(key)
	for i := range list {
		if list[i].KeyHash == h {
			return &list[i], true
		}
	}
	return nil, false
}

// Normalize forgives how people retype a key: case, spaces, dashes.
func Normalize(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '-' || r == '\t' || r == '\n' || r == '\r':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return r
	}, key)
}

// Hash is the stored form of a key.
func Hash(key string) string {
	sum := sha256.Sum256([]byte(Normalize(key)))
	return hex.EncodeToString(sum[:])
}

var keyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newKey is 128 random bits as 26 lowercase base32 characters: no
// look-alike punctuation, safe in a URL, and case-insensitive.
func newKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating key: %w", err)
	}
	return strings.ToLower(keyEncoding.EncodeToString(b)), nil
}
