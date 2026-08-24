package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func DefaultSessionPath() string {
	if p := os.Getenv("IHME_SESSION_PATH"); p != "" {
		return p
	}
	newPath := filepath.Join(configDir(), "ihme", "session.json")

	// Migrate from pre-v0.2 macOS path (~/Library/Application Support/ihme/)
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			oldDir, _ := os.UserConfigDir()
			if oldDir != "" {
				oldPath := filepath.Join(oldDir, "ihme", "session.json")
				if _, err := os.Stat(oldPath); err == nil {
					os.MkdirAll(filepath.Dir(newPath), 0700)
					if os.Rename(oldPath, newPath) == nil {
						os.Remove(filepath.Dir(oldPath))
					}
				}
			}
		}
	}
	return newPath
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserConfigDir(); err == nil {
			return d
		}
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".config")
}

func LoadSession(path string) (*SessionData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading session: %w", err)
	}

	var sess SessionData
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("parsing session: %w", err)
	}
	return &sess, nil
}

func SaveSession(path string, sess *SessionData) error {
	sess.SavedAt = time.Now()

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling session: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// Write-then-rename: a session is now also saved mid-command
	// (recovery re-mints cookies), so a crash or a second ihme
	// process must never leave a half-written session behind. This
	// makes the file whole, not the read-modify-write serialized —
	// concurrent writers still need the lock file on the roadmap.
	tmp, err := os.CreateTemp(dir, "session-*.tmp")
	if err != nil {
		return fmt.Errorf("writing session: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("writing session: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing session: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing session: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing session: %w", err)
	}
	return nil
}

func DeleteSession(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *SessionData) IsExpired() bool {
	if s.SavedAt.IsZero() {
		return true
	}
	return time.Since(s.SavedAt) > 30*24*time.Hour
}
