package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func DefaultSessionPath() string {
	if p := os.Getenv("IHME_SESSION_PATH"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "ihme", "session.json")
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

	if err := os.WriteFile(path, data, 0600); err != nil {
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
