package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	sess := &SessionData{
		AppleID:        "test@icloud.com",
		SessionToken:   "token123",
		TrustToken:     "trust456",
		AccountCountry: "USA",
		Dsid:           "12345",
		Webservices: map[string]WebserviceEndpoint{
			"premiummailsettings": {URL: "https://example.com", Status: "active"},
		},
	}

	if err := SaveSession(path, sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	if loaded.AppleID != "test@icloud.com" {
		t.Errorf("AppleID = %q, want test@icloud.com", loaded.AppleID)
	}
	if loaded.SessionToken != "token123" {
		t.Errorf("SessionToken = %q, want token123", loaded.SessionToken)
	}
	if loaded.TrustToken != "trust456" {
		t.Errorf("TrustToken = %q, want trust456", loaded.TrustToken)
	}
	if loaded.Dsid != "12345" {
		t.Errorf("Dsid = %q, want 12345", loaded.Dsid)
	}
	if loaded.SavedAt.IsZero() {
		t.Error("SavedAt should be set")
	}

	ws, ok := loaded.Webservices["premiummailsettings"]
	if !ok {
		t.Fatal("premiummailsettings not in webservices")
	}
	if ws.URL != "https://example.com" {
		t.Errorf("URL = %q, want https://example.com", ws.URL)
	}
}

func TestLoadSessionNotFound(t *testing.T) {
	sess, err := LoadSession("/nonexistent/path/session.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != nil {
		t.Error("expected nil for missing file")
	}
}

func TestLoadSessionCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	os.WriteFile(path, []byte("not json"), 0600)

	_, err := LoadSession(path)
	if err == nil {
		t.Error("expected error for corrupt JSON")
	}
}

func TestDeleteSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	SaveSession(path, &SessionData{AppleID: "test"})

	if err := DeleteSession(path); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	sess, _ := LoadSession(path)
	if sess != nil {
		t.Error("session should be deleted")
	}
}

func TestDeleteSessionNotFound(t *testing.T) {
	if err := DeleteSession("/nonexistent/session.json"); err != nil {
		t.Errorf("deleting nonexistent should not error: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	fresh := &SessionData{SavedAt: time.Now()}
	if fresh.IsExpired() {
		t.Error("fresh session should not be expired")
	}

	old := &SessionData{SavedAt: time.Now().Add(-31 * 24 * time.Hour)}
	if !old.IsExpired() {
		t.Error("31-day-old session should be expired")
	}

	empty := &SessionData{}
	if !empty.IsExpired() {
		t.Error("session with zero SavedAt should be expired")
	}
}

func TestConfigDir(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
		want string
	}{
		{
			name: "XDG_CONFIG_HOME set",
			envs: map[string]string{"XDG_CONFIG_HOME": "/custom/config", "HOME": "/home/test"},
			want: "/custom/config",
		},
		{
			name: "HOME fallback",
			envs: map[string]string{"XDG_CONFIG_HOME": "", "HOME": "/home/test"},
			want: "/home/test/.config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}
			got := configDir()
			if got != tt.want {
				t.Errorf("configDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultSessionPath(t *testing.T) {
	t.Run("IHME_SESSION_PATH override", func(t *testing.T) {
		t.Setenv("IHME_SESSION_PATH", "/override/session.json")
		got := DefaultSessionPath()
		if got != "/override/session.json" {
			t.Errorf("got %q, want /override/session.json", got)
		}
	})

	t.Run("default uses configDir", func(t *testing.T) {
		t.Setenv("IHME_SESSION_PATH", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/test")
		got := DefaultSessionPath()
		want := "/home/test/.config/ihme/session.json"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestSessionFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "session.json")

	SaveSession(path, &SessionData{AppleID: "test"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	dirPerm := dirInfo.Mode().Perm()
	if dirPerm != 0700 {
		t.Errorf("dir permissions = %o, want 0700", dirPerm)
	}
}
