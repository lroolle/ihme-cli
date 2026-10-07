package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	}, true)
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
	}, true)
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
	}, true)
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
	}, true)
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

// Legacy sessions can hold a name twice. mergeCookies updates the
// last copy, so the header must send the last copy, or every
// rotation is silently ignored.
func TestCookieStringLastCopyWins(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{
		{Name: "A", Value: "stale"},
		{Name: "B", Value: "b"},
		{Name: "A", Value: "old"},
	}
	c.mergeCookies([]*http.Cookie{{Name: "A", Value: "fresh"}}, true)
	if got := c.cookieString(); got != "B=b; A=fresh" {
		t.Errorf("cookie header = %q, want %q", got, "B=b; A=fresh")
	}
}

func TestMergeCookiesExpiryRemoves(t *testing.T) {
	cases := map[string]*http.Cookie{
		"empty value":  {Name: "A", Value: ""},
		"max-age<=0":   {Name: "A", Value: "x", MaxAge: -1},
		"expires past": {Name: "A", Value: "x", Expires: time.Now().Add(-time.Hour)},
	}
	for name, tomb := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := NewClient()
			c.session.Cookies = []SavedCookie{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}, {Name: "A", Value: "3"}}
			if !c.mergeCookies([]*http.Cookie{tomb}, true) {
				t.Error("removing a cookie must report a change")
			}
			if got := c.cookieString(); got != "B=2" {
				t.Errorf("cookie header = %q, want only B=2", got)
			}
		})
	}
}

// A non-2xx answer can rotate a cookie but not delete one.
func TestMergeCookiesKeepsOnFailedResponse(t *testing.T) {
	c, _ := NewClient()
	c.session.Cookies = []SavedCookie{{Name: "A", Value: "1"}}
	if c.mergeCookies([]*http.Cookie{{Name: "A", Value: ""}}, false) {
		t.Error("a deletion on a failed response reported a change")
	}
	if c.cookieString() != "A=1" {
		t.Errorf("a failed response deleted a cookie: %q", c.cookieString())
	}
	if !c.mergeCookies([]*http.Cookie{{Name: "A", Value: "2"}}, false) || c.cookieString() != "A=2" {
		t.Error("a rotation on a failed response was dropped")
	}
}

func TestMergeCookiesReportsChange(t *testing.T) {
	c, _ := NewClient()
	if !c.mergeCookies([]*http.Cookie{{Name: "A", Value: "1"}}, true) {
		t.Error("a new cookie is a change")
	}
	if c.mergeCookies([]*http.Cookie{{Name: "A", Value: "1", MaxAge: 3600}}, true) {
		t.Error("the same value with a fresh expiry is not a change")
	}
	if !c.mergeCookies([]*http.Cookie{{Name: "A", Value: "2"}}, true) {
		t.Error("a rotated value is a change")
	}
	if c.mergeCookies(nil, true) {
		t.Error("no cookies is no change")
	}
}

// Apple sets its WEBAUTH cookies in double quotes. A browser replays
// them byte for byte; so must we, across a save and a reload.
func TestQuotedCookieRoundTrip(t *testing.T) {
	var gotCookie string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotCookie = r.Header.Get("Cookie")
		if calls == 1 {
			w.Header().Add("Set-Cookie", `X-APPLE-WEBAUTH-USER="v=1:s=0:d=42"; Path=/; Domain=127.0.0.1`)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c, _ := NewClient()
	if _, err := c.doServiceRequest("POST", srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(c.session)
	var reloaded SessionData
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	c2, _ := NewClientWithSession(&reloaded)
	if _, err := c2.doServiceRequest("POST", srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if want := `X-APPLE-WEBAUTH-USER="v=1:s=0:d=42"`; gotCookie != want {
		t.Errorf("replayed Cookie = %q, want %q", gotCookie, want)
	}
}

// A cookie Apple rotates on an ordinary call must reach disk: inside
// the validate TTL nothing else saves the session, and the next
// command would start on the stale value.
func TestRotatedCookieTriggersSessionUpdate(t *testing.T) {
	value := "1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "X-APPLE-WEBAUTH-TOKEN="+value+"; Path=/")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c, _ := NewClient()
	updates := 0
	c.OnSessionUpdate = func(*SessionData) { updates++ }
	for range 2 {
		if _, err := c.doServiceRequest("GET", srv.URL, nil); err != nil {
			t.Fatal(err)
		}
	}
	if updates != 1 {
		t.Errorf("same cookie twice: %d session updates, want 1", updates)
	}
	value = "2"
	if _, err := c.doServiceRequest("GET", srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if updates != 2 {
		t.Errorf("rotated cookie: %d session updates, want 2", updates)
	}
}

func TestHmeBaseURLAdvertisedWithoutURL(t *testing.T) {
	c, _ := NewClient()
	c.session.Webservices = map[string]WebserviceEndpoint{"premiummailsettings": {Status: "active"}}
	_, err := c.hmeBaseURL()
	if err == nil || !strings.Contains(err.Error(), "without a service URL") {
		t.Errorf("want a named error for an empty service URL, got %v", err)
	}
}
