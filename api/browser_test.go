package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseBrowserCookies(t *testing.T) {
	cases := []struct {
		name, in string
		want     string // cookieString of the result; "" means an error
	}{
		{"header value", `X-APPLE-WEBAUTH-TOKEN="v=2:t=abc"; X-APPLE-WEBAUTH-USER="v=1:s=0:d=1"; dslang=US-EN`,
			`X-APPLE-WEBAUTH-TOKEN="v=2:t=abc"; X-APPLE-WEBAUTH-USER="v=1:s=0:d=1"; dslang=US-EN`},
		{"with header name", "Cookie: X-APPLE-WEBAUTH-TOKEN=abc; a=b\n", "X-APPLE-WEBAUTH-TOKEN=abc; a=b"},
		{"as curl -H quotes it", `'cookie: X-APPLE-WEBAUTH-TOKEN=abc'`, "X-APPLE-WEBAUTH-TOKEN=abc"},
		{"values may contain = and :", "X-APPLE-WEBAUTH-TOKEN=v=2:t=AQ==", "X-APPLE-WEBAUTH-TOKEN=v=2:t=AQ=="},
		{"first of a name wins, junk skipped", "X-APPLE-WEBAUTH-TOKEN=new; ; junk; X-APPLE-WEBAUTH-TOKEN=old; empty=", "X-APPLE-WEBAUTH-TOKEN=new"},
		{"cookie editor JSON, other sites dropped",
			`[{"name":"X-APPLE-WEBAUTH-TOKEN","value":"\"v=2:t=abc\"","domain":".icloud.com"},{"name":"sid","value":"x","domain":".example.com"},{"name":"w","value":"1","domain":"www.icloud.com"}]`,
			`X-APPLE-WEBAUTH-TOKEN="v=2:t=abc"; w=1`},
		{"not signed in", "dslang=US-EN; site=USA", ""},
		{"empty", "  ", ""},
		{"broken JSON", `[{"name":`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cookies, err := ParseBrowserCookies(tc.in)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("want an error, got %v", cookies)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c, _ := NewClientWithSession(&SessionData{Cookies: cookies})
			if got := c.cookieString(); got != tc.want {
				t.Errorf("replayed as %q, want %q", got, tc.want)
			}
		})
	}
}

// browserApple answers /validate with status and body (SERVICE_URL
// in it becomes the server's own URL), records the
// Cookie header it was sent, and counts accountLogin calls, which a
// browser session must never make.
type browserApple struct {
	*httptest.Server
	status       int
	body         string
	hmeStatus    int
	gotCookie    string
	accountLogin int
}

func newBrowserApple(t *testing.T, status int, body string) *browserApple {
	a := &browserApple{status: status, body: body, hmeStatus: http.StatusOK}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/validate"):
			a.gotCookie = r.Header.Get("Cookie")
			w.WriteHeader(a.status)
			fmt.Fprint(w, strings.ReplaceAll(a.body, "SERVICE_URL", a.URL))
		case strings.HasSuffix(r.URL.Path, "/accountLogin"):
			a.accountLogin++
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasSuffix(r.URL.Path, "/v2/hme/list"):
			w.WriteHeader(a.hmeStatus)
			fmt.Fprint(w, `{"success":true,"result":{"hmeEmails":[{"anonymousId":"a"}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *browserApple) client() *Client {
	c, _ := NewClientWithSession(&SessionData{
		Cookies: []SavedCookie{{Name: "X-APPLE-WEBAUTH-TOKEN", Value: "v=2:t=abc", Quoted: true}},
	})
	c.setupBase = a.URL
	return c
}

const validateOK = `{"dsInfo":{"dsid":"42","appleId":"Someone@Example.com"},"webservices":{"premiummailsettings":{"url":"SERVICE_URL","status":"active"}}}`

func TestImportBrowserSession(t *testing.T) {
	apple := newBrowserApple(t, http.StatusOK, validateOK)
	c := apple.client()
	if err := c.ImportBrowserSession(); err != nil {
		t.Fatal(err)
	}
	s := c.Session()
	if s.Source != SourceBrowser || s.AppleID != "someone@example.com" || s.Dsid != "42" {
		t.Errorf("session = source %q, appleId %q, dsid %q", s.Source, s.AppleID, s.Dsid)
	}
	if apple.gotCookie != `X-APPLE-WEBAUTH-TOKEN="v=2:t=abc"` {
		t.Errorf("validate got Cookie %q", apple.gotCookie)
	}
	if list, err := c.ListHme(); err != nil || len(list.HmeEmails) != 1 {
		t.Errorf("ListHme = %v, %v", list, err)
	}
}

func TestImportBrowserSessionRefused(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"401", `{}`, http.StatusUnauthorized},
		{"421 refusal", `{"success":false,"requestInfo":[{"country":"US"}]}`, http.StatusMisdirectedRequest},
		{"2FA owed", `{"dsInfo":{"dsid":"42"},"hsaChallengeRequired":true,"webservices":{"premiummailsettings":{"url":"SERVICE_URL"}}}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apple := newBrowserApple(t, tc.status, tc.body)
			err := apple.client().ImportBrowserSession()
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.status != http.StatusOK && !IsAuthRejection(err) {
				t.Errorf("refused cookies should be a rejection: %v", err)
			}
			if apple.accountLogin != 0 {
				t.Errorf("browser import called accountLogin %d times", apple.accountLogin)
			}
		})
	}
}

// An expired browser session has nothing to re-mint cookies with: it
// must fail as a rejection that names the browser fix, without the
// accountLogin fallback a password session gets.
func TestBrowserSessionExpired(t *testing.T) {
	t.Run("resume", func(t *testing.T) {
		apple := newBrowserApple(t, http.StatusUnauthorized, `{}`)
		c := apple.client()
		c.session.Source = SourceBrowser
		err := c.ResumeSession()
		if !errors.Is(err, ErrBrowserSessionExpired) || !IsAuthRejection(err) {
			t.Fatalf("ResumeSession = %v", err)
		}
		if apple.accountLogin != 0 {
			t.Errorf("accountLogin called %d times", apple.accountLogin)
		}
	})
	t.Run("resume on 421 refusal", func(t *testing.T) {
		apple := newBrowserApple(t, http.StatusMisdirectedRequest, `{"success":false}`)
		c := apple.client()
		c.session.Source = SourceBrowser
		if err := c.ResumeSession(); !errors.Is(err, ErrBrowserSessionExpired) {
			t.Fatalf("ResumeSession = %v", err)
		}
	})
	// Moments after import the session is fresh: a refusal is Hide
	// My Email's verdict on the account, not expiry.
	t.Run("service call right after import", func(t *testing.T) {
		apple := newBrowserApple(t, http.StatusOK, validateOK)
		c := apple.client()
		if err := c.ImportBrowserSession(); err != nil {
			t.Fatal(err)
		}
		apple.hmeStatus = http.StatusUnauthorized
		_, err := c.ListHme()
		if !errors.As(err, new(*ServiceRefusedError)) || errors.Is(err, ErrBrowserSessionExpired) {
			t.Fatalf("ListHme = %v", err)
		}
	})
	// In a later command the same refusal means the cookies expired.
	t.Run("service call in a later command", func(t *testing.T) {
		apple := newBrowserApple(t, http.StatusOK, validateOK)
		c := apple.client()
		c.session.Source = SourceBrowser
		c.session.Webservices = map[string]WebserviceEndpoint{"premiummailsettings": {URL: apple.URL}}
		apple.hmeStatus = http.StatusUnauthorized
		_, err := c.ListHme()
		if !errors.Is(err, ErrBrowserSessionExpired) || !IsAuthRejection(err) {
			t.Fatalf("ListHme = %v", err)
		}
		if !strings.Contains(err.Error(), "401") {
			t.Errorf("error lost the service's verdict: %v", err)
		}
		if apple.accountLogin != 0 {
			t.Errorf("accountLogin called %d times", apple.accountLogin)
		}
	})
}
