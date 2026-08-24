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

func TestHmeErrorUnmarshalShapes(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantMsg  string
		wantCode string
	}{
		{"string code (reserve)", `{"errorMessage":"limit reached","errorCode":"-41015"}`, "limit reached", "-41015"},
		{"int code", `{"errorMessage":"nope","errorCode":403}`, "nope", "403"},
		{"message only", `{"errorMessage":"nope"}`, "nope", ""},
		{"null code", `{"errorMessage":"nope","errorCode":null}`, "nope", ""},
		{"bare int (421 body)", `1`, "", "1"},
		{"bare string", `"-41015"`, "", "-41015"},
	}
	for _, tc := range cases {
		var e HmeError
		if err := json.Unmarshal([]byte(tc.body), &e); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.name, err)
		}
		if e.ErrorMessage != tc.wantMsg {
			t.Errorf("%s: ErrorMessage = %q, want %q", tc.name, e.ErrorMessage, tc.wantMsg)
		}
		if e.ErrorCode != tc.wantCode {
			t.Errorf("%s: ErrorCode = %q, want %q", tc.name, e.ErrorCode, tc.wantCode)
		}
	}
}

func TestReserveErrorEnvelope(t *testing.T) {
	// The exact shape that used to kill the parse: errorCode as a
	// quoted string inside a success=false reserve response.
	body := `{"success":false,"error":{"errorMessage":"You have reached the limit","errorCode":"-41015"}}`
	var resp struct {
		Success bool      `json:"success"`
		Error   *HmeError `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Success {
		t.Error("Success should be false")
	}
	if resp.Error == nil {
		t.Fatal("Error should be present")
	}
	if resp.Error.ErrorMessage != "You have reached the limit" {
		t.Errorf("ErrorMessage = %q", resp.Error.ErrorMessage)
	}
	if resp.Error.ErrorCode != "-41015" {
		t.Errorf("ErrorCode = %q", resp.Error.ErrorCode)
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
