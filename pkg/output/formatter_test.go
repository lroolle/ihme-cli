package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
)

var testEmails = []api.HmeEmail{
	{
		AnonymousID:     "abc12345-full-id",
		Label:           "github.com",
		Hme:             "test@privaterelay.appleid.com",
		ForwardToEmail:  "user@icloud.com",
		IsActive:        true,
		CreateTimestamp:  1705276800000,
		Note:            "#dev | main account",
	},
	{
		AnonymousID:     "def67890-full-id",
		Label:           "amazon.com",
		Hme:             "test2@privaterelay.appleid.com",
		ForwardToEmail:  "user@icloud.com",
		IsActive:        false,
		CreateTimestamp:  1704067200000,
		Note:            "",
	},
}

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	PrintTable(&buf, testEmails)
	out := buf.String()

	if !strings.Contains(out, "ID") || !strings.Contains(out, "LABEL") {
		t.Error("table missing headers")
	}
	if !strings.Contains(out, "github.com") {
		t.Error("table missing github.com label")
	}
	if !strings.Contains(out, "abc12345") {
		t.Error("table should truncate ID to 8 chars")
	}
	if !strings.Contains(out, "active") {
		t.Error("table missing status")
	}
}

func TestPrintTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	PrintTable(&buf, nil)
	out := buf.String()
	if !strings.Contains(out, "ID") {
		t.Error("empty table should still have headers")
	}
}

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintJSON(&buf, testEmails); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}

	var parsed []api.HmeEmail
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("got %d items, want 2", len(parsed))
	}
	if parsed[0].Label != "github.com" {
		t.Errorf("first label = %q, want github.com", parsed[0].Label)
	}
}

func TestPrintCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintCSV(&buf, testEmails); err != nil {
		t.Fatalf("PrintCSV: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")

	if len(lines) != 3 {
		t.Fatalf("CSV lines = %d, want 3 (header + 2 rows)", len(lines))
	}

	header := lines[0]
	if !strings.Contains(header, "anonymousId") || !strings.Contains(header, "hme") {
		t.Error("CSV missing expected headers")
	}

	row1 := lines[1]
	if !strings.Contains(row1, "github.com") {
		t.Error("first row missing github.com")
	}
	if !strings.Contains(row1, "dev") {
		t.Error("first row missing tag 'dev'")
	}
}

func TestPrintDetail(t *testing.T) {
	var buf bytes.Buffer
	PrintDetail(&buf, &testEmails[0])
	out := buf.String()

	checks := []string{
		"ID:",
		"abc12345-full-id",
		"Label:",
		"github.com",
		"Address:",
		"test@privaterelay.appleid.com",
		"Forward to:",
		"Status:",
		"active",
		"Tags:",
		"dev",
		"Note:",
		"main account",
	}

	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("detail missing %q", check)
		}
	}
}

func TestPrintDetailInactive(t *testing.T) {
	var buf bytes.Buffer
	PrintDetail(&buf, &testEmails[1])
	out := buf.String()

	if !strings.Contains(out, "inactive") {
		t.Error("should show 'inactive' status")
	}
	if strings.Contains(out, "Tags:") {
		t.Error("should not show Tags line when no tags")
	}
}
