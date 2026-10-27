package inboxkey

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// BaseURL resolves a flag, the current/legacy environment, the saved URL,
// then the local development server. It never contacts the inbox.
func BaseURL(explicit string) (string, error) {
	for _, value := range []string{explicit, os.Getenv("IHME_INBOX_URL"), os.Getenv("IHME_SERVE_URL")} {
		if value != "" {
			return validateURL(value)
		}
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "url"))
	if errors.Is(err, fs.ErrNotExist) {
		return defaultURL, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the inbox URL: %w", err)
	}
	return validateURL(string(b))
}

func validateURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err == nil && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(value, "?#") {
		ip := net.ParseIP(u.Hostname())
		local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
		if u.Scheme == "https" || (u.Scheme == "http" && local) {
			return strings.TrimRight(value, "/"), nil
		}
	}
	return "", errors.New("invalid inbox URL: use HTTPS (HTTP is allowed for localhost), without credentials, a query, or a fragment\n\n  Fix: ihme share config --url https://mail.example.com")
}

// SaveURL changes only the local link destination, never the shared secret.
func SaveURL(value string) (string, error) {
	value, err := validateURL(value)
	if err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "url-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(value + "\n"); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, "url")); err != nil {
		return "", err
	}
	return value, nil
}
