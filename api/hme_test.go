package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hmeTestServer serves the two endpoints a recovery round touches:
// the HME call itself (rejecting the first n attempts with 401) and
// accountLogin, which hands back a fresh webservices map pointing at
// the same server.
type hmeTestServer struct {
	*httptest.Server
	rejectFirst int
	hmeCalls    int
	loginCalls  int
	lastDsid    string
}

func newHmeTestServer(rejectFirst int) *hmeTestServer {
	s := &hmeTestServer{rejectFirst: rejectFirst}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/accountLogin"):
			s.loginCalls++
			fmt.Fprintf(w, `{"dsInfo":{"dsid":"999"},"webservices":{"premiummailsettings":{"url":%q,"status":"active"}}}`, s.URL)
		case strings.HasSuffix(r.URL.Path, "/v2/hme/list"):
			s.hmeCalls++
			s.lastDsid = r.URL.Query().Get("dsid")
			if s.hmeCalls <= s.rejectFirst {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"success":true,"result":{"hmeEmails":[{"anonymousId":"abc"}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return s
}

func (s *hmeTestServer) client() *Client {
	c, _ := NewClientWithSession(&SessionData{
		SessionToken: "session-token",
		TrustToken:   "trust-token",
		Dsid:         "111",
		Webservices:  map[string]WebserviceEndpoint{"premiummailsettings": {URL: s.URL, Status: "active"}},
		Cookies:      []SavedCookie{{Name: "X-APPLE-WEBAUTH-TOKEN", Value: "stale"}},
	})
	c.setupBase = s.URL
	return c
}

// A service host can reject a session the pre-flight validate just
// cleared. One silent re-auth beats handing the user a 401.
func TestHmeRequestRecoversFromRejection(t *testing.T) {
	srv := newHmeTestServer(1)
	defer srv.Close()

	c := srv.client()
	saved := 0
	c.OnSessionUpdate = func(*SessionData) { saved++ }

	result, err := c.ListHme()
	if err != nil {
		t.Fatalf("ListHme after recovery: %v", err)
	}
	if len(result.HmeEmails) != 1 {
		t.Fatalf("expected 1 address, got %d", len(result.HmeEmails))
	}
	if srv.loginCalls != 1 {
		t.Errorf("expected 1 accountLogin, got %d", srv.loginCalls)
	}
	if srv.hmeCalls != 2 {
		t.Errorf("expected 2 HME attempts, got %d", srv.hmeCalls)
	}
	// The retry must be rebuilt, not replayed: accountLogin can move
	// the dsid and the mail-domain host.
	if srv.lastDsid != "999" {
		t.Errorf("retry used stale dsid %q, want the one accountLogin returned", srv.lastDsid)
	}
	if saved != 1 {
		t.Errorf("expected the refreshed session to be handed back once, got %d", saved)
	}
}

// Recovery is one shot. A session Apple keeps rejecting is expired,
// and a retry loop against Apple's auth endpoints is how accounts
// get rate limited.
func TestHmeRequestRecoversOnlyOnce(t *testing.T) {
	srv := newHmeTestServer(99)
	defer srv.Close()

	_, err := srv.client().ListHme()
	if err == nil {
		t.Fatal("expected an error from a session Apple keeps rejecting")
	}
	if !IsAuthRejection(err) {
		t.Errorf("persistent 401 should read as an auth rejection, got %v", err)
	}
	// The service's verdict is the diagnosis; recovery's failure is
	// only why it stands.
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error dropped the status that explains it: %v", err)
	}
	if srv.hmeCalls != 2 || srv.loginCalls != 1 {
		t.Errorf("expected 2 HME attempts and 1 accountLogin, got %d and %d", srv.hmeCalls, srv.loginCalls)
	}
}

// Without tokens there is nothing to re-mint; fail as a rejection so
// the caller sends the user to `ihme auth login`, not to a retry.
func TestHmeRequestWithoutTokensFailsAsRejection(t *testing.T) {
	srv := newHmeTestServer(99)
	defer srv.Close()

	c := srv.client()
	c.session.SessionToken = ""
	c.session.TrustToken = ""

	_, err := c.ListHme()
	if !IsAuthRejection(err) {
		t.Fatalf("expected an auth rejection, got %v", err)
	}
	if srv.loginCalls != 0 {
		t.Errorf("expected no accountLogin attempt, got %d", srv.loginCalls)
	}
}

// A 401 body is often empty; the error must still name the status
// and the host, and must never carry the account's dsid.
func TestHTTPErrorRedactsQuery(t *testing.T) {
	e := &HTTPError{
		Status: 401,
		URL:    "https://p137-maildomainws.icloud.com/v1/hme/generate?clientId=abc&dsid=16941737673",
	}
	msg := e.Error()
	if strings.Contains(msg, "dsid") || strings.Contains(msg, "clientId") {
		t.Errorf("error message leaks account identifiers: %s", msg)
	}
	if !strings.Contains(msg, "401") || !strings.Contains(msg, "/v1/hme/generate") {
		t.Errorf("error message lost the diagnosis: %s", msg)
	}
	if strings.HasSuffix(msg, ": ") {
		t.Errorf("empty body should not leave a dangling colon: %q", msg)
	}
}

func TestHmeErrorScalarBody(t *testing.T) {
	var e HmeError
	if err := json.Unmarshal([]byte(`1`), &e); err != nil {
		t.Fatalf("scalar error member: %v", err)
	}
	if e.ErrorCode != "1" {
		t.Errorf("expected code 1, got %q", e.ErrorCode)
	}
}
