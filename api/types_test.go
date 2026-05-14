package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHmeEmailCreatedAt(t *testing.T) {
	hme := HmeEmail{CreateTimestamp: 1705276800000}
	created := hme.CreatedAt()

	if created.Year() != 2024 {
		t.Errorf("year = %d, want 2024", created.Year())
	}
	if created.Month() != time.January {
		t.Errorf("month = %v, want January", created.Month())
	}
}

func TestHmeEmailJSON(t *testing.T) {
	hme := HmeEmail{
		AnonymousID:    "abc123",
		Label:          "test.com",
		Hme:            "test@relay.com",
		IsActive:       true,
		ForwardToEmail: "user@icloud.com",
	}

	data, err := json.Marshal(hme)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed HmeEmail
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.AnonymousID != "abc123" {
		t.Errorf("AnonymousID = %q", parsed.AnonymousID)
	}
	if parsed.Label != "test.com" {
		t.Errorf("Label = %q", parsed.Label)
	}
	if !parsed.IsActive {
		t.Error("IsActive should be true")
	}
}

func TestSessionDataJSON(t *testing.T) {
	sess := SessionData{
		AppleID:      "test@icloud.com",
		SessionToken: "token",
		TrustToken:   "trust",
		Webservices: map[string]WebserviceEndpoint{
			"premiummailsettings": {URL: "https://example.com", Status: "active"},
		},
		SavedAt: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed SessionData
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.AppleID != "test@icloud.com" {
		t.Errorf("AppleID = %q", parsed.AppleID)
	}
	ws, ok := parsed.Webservices["premiummailsettings"]
	if !ok {
		t.Fatal("missing premiummailsettings")
	}
	if ws.URL != "https://example.com" {
		t.Errorf("URL = %q", ws.URL)
	}
}
