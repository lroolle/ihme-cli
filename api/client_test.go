package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestCookieStringEmpty(t *testing.T) {
	c, _ := NewClient()
	if s := c.cookieString(); s != "" {
		t.Errorf("empty client should return empty cookie string, got %q", s)
	}
}

func TestCookieStringFromSession(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{
		{Name: "X-APPLE-WEBAUTH-USER", Value: "v=1:s=1:d=123", Domain: "www.icloud.com"},
		{Name: "X-APPLE-DS-WEB-SESSION-TOKEN", Value: "tokenvalue", Domain: "www.icloud.com"},
	}

	s := c.cookieString()
	if !strings.Contains(s, "X-APPLE-WEBAUTH-USER=v=1:s=1:d=123") {
		t.Errorf("cookie string missing WEBAUTH-USER: %q", s)
	}
	if !strings.Contains(s, "X-APPLE-DS-WEB-SESSION-TOKEN=tokenvalue") {
		t.Errorf("cookie string missing SESSION-TOKEN: %q", s)
	}
	if !strings.Contains(s, "; ") {
		t.Errorf("cookies should be separated by '; ': %q", s)
	}
}

func TestCookieStringSkipsEmpty(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{
		{Name: "good", Value: "yes"},
		{Name: "empty", Value: ""},
		{Name: "also-good", Value: "yes"},
	}

	s := c.cookieString()
	if strings.Contains(s, "empty") {
		t.Errorf("should skip empty-value cookies: %q", s)
	}
	parts := strings.Split(s, "; ")
	if len(parts) != 2 {
		t.Errorf("expected 2 cookies, got %d: %q", len(parts), s)
	}
}

func TestMergeCookiesNew(t *testing.T) {
	c, _ := NewClient()
	c.mergeCookies([]*http.Cookie{
		{Name: "A", Value: "1"},
		{Name: "B", Value: "2"},
	})
	if len(c.session.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(c.session.Cookies))
	}
}

func TestMergeCookiesUpdate(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{
		{Name: "A", Value: "old"},
	}
	c.mergeCookies([]*http.Cookie{
		{Name: "A", Value: "new"},
	})
	if len(c.session.Cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(c.session.Cookies))
	}
	if c.session.Cookies[0].Value != "new" {
		t.Errorf("expected updated value 'new', got %q", c.session.Cookies[0].Value)
	}
}

func TestMergeCookiesSkipsEmpty(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{
		{Name: "A", Value: "keep"},
	}
	c.mergeCookies([]*http.Cookie{
		{Name: "B", Value: ""},
	})
	if len(c.session.Cookies) != 1 {
		t.Fatalf("should not add empty cookie, got %d", len(c.session.Cookies))
	}
}

func TestMergeCookiesMixed(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{
		{Name: "existing", Value: "old"},
	}
	c.mergeCookies([]*http.Cookie{
		{Name: "existing", Value: "updated"},
		{Name: "new-cookie", Value: "fresh"},
	})
	if len(c.session.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(c.session.Cookies))
	}
	if c.session.Cookies[0].Value != "updated" {
		t.Errorf("existing cookie not updated: %q", c.session.Cookies[0].Value)
	}
	if c.session.Cookies[1].Name != "new-cookie" {
		t.Errorf("new cookie not appended: %q", c.session.Cookies[1].Name)
	}
}

func TestNewClientWithSession(t *testing.T) {
	sess := &SessionData{
		AppleID:      "test@icloud.com",
		SessionToken: "token",
		Cookies: []SavedCookie{
			{Name: "A", Value: "1", Domain: "icloud.com"},
		},
	}
	c, err := NewClientWithSession(sess)
	if err != nil {
		t.Fatalf("NewClientWithSession: %v", err)
	}
	if c.session.AppleID != "test@icloud.com" {
		t.Errorf("AppleID = %q", c.session.AppleID)
	}
	if c.cookieString() != "A=1" {
		t.Errorf("cookies not restored: %q", c.cookieString())
	}
}

func TestSetupURLDefault(t *testing.T) {
	c, _ := NewClient()
	if c.setupURL() != SetupEndpoint {
		t.Errorf("default should be %s, got %s", SetupEndpoint, c.setupURL())
	}
}

func TestSetupURLCN(t *testing.T) {
	c, _ := NewClient()
	c.session.AccountCountry = "CN"
	if c.setupURL() != SetupEndpointCN {
		t.Errorf("CN should be %s, got %s", SetupEndpointCN, c.setupURL())
	}
}
