package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"
)

type Client struct {
	http      *http.Client
	session   *SessionData
	clientID  string
	frameID   string
	authAttr  string
	userAgent string
	// setupBase overrides the setup.icloud.com origin. Empty in
	// production (the region constants decide); set by tests.
	setupBase string
	Verbose   bool
	// OnSessionUpdate, when set, is called whenever the session
	// changes mid-command: Apple rotated or expired a cookie on a
	// service response, or the client re-minted the session. The
	// session lives longer than the process, so whoever owns the
	// file gets a chance to persist the fresh cookies instead of
	// losing them at exit.
	OnSessionUpdate func(*SessionData)
}

func NewClient() (*Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}

	clientID := uuid.New().String()
	return &Client{
		http: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		clientID:  clientID,
		session:   &SessionData{ClientID: clientID},
		userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.3.1 Safari/605.1.15",
	}, nil
}

func NewClientWithSession(sess *SessionData) (*Client, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	c.session = sess
	// A client id identifies an installation, not a request. Apple
	// sees one stable id per machine instead of a new one every
	// invocation, and -v output correlates across commands.
	if sess.ClientID == "" {
		sess.ClientID = c.clientID
	}
	c.clientID = sess.ClientID
	return c, nil
}

func (c *Client) Session() *SessionData {
	return c.session
}

// cookieString builds the Cookie header value from stored session cookies.
// Bypasses Go's cookie jar domain matching — cookies go to every service
// request regardless of subdomain. This is how rclone handles it.
// A name stored twice (legacy sessions accumulated copies across
// domains) is sent once, with its LAST value: mergeCookies updates
// the last copy, so the first one is the stale one.
//
// Values Apple set in double quotes go back in double quotes, the
// way a browser replays them. Go's cookie parser strips the quotes
// into Cookie.Quoted; dropping that bit sends a different byte
// string than Apple issued.
func (c *Client) cookieString() string {
	last := make(map[string]int, len(c.session.Cookies))
	for i, ck := range c.session.Cookies {
		last[ck.Name] = i
	}
	var b strings.Builder
	for i, ck := range c.session.Cookies {
		if ck.Value == "" || last[ck.Name] != i {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(ck.Name)
		b.WriteByte('=')
		if ck.Quoted {
			b.WriteByte('"')
			b.WriteString(ck.Value)
			b.WriteByte('"')
		} else {
			b.WriteString(ck.Value)
		}
	}
	return b.String()
}

// mergeCookies merges response cookies into the session cookie list
// and reports whether anything changed. A cookie Apple expires (empty
// value, Max-Age<=0, or an Expires in the past) is removed, not kept:
// replaying a cookie the server deleted is a request a browser would
// never send. Re-sending the same value (a fresh expiry) is not a
// change, so steady-state responses never cost a session write.
//
// Deletions apply only when ok (a 2xx). Cookies are stored by name,
// not by host, so one partition host clearing a name while it answers
// 421 or 5xx would otherwise drop the copy every other host relies on.
func (c *Client) mergeCookies(cookies []*http.Cookie, ok bool) bool {
	now := time.Now()
	changed := false
	for _, ck := range cookies {
		expired := ck.Value == "" || ck.MaxAge < 0 ||
			(!ck.Expires.IsZero() && ck.Expires.Before(now))
		// The last copy is the live one (see cookieString).
		i := -1
		for j, have := range c.session.Cookies {
			if have.Name == ck.Name {
				i = j
			}
		}
		switch {
		case expired && !ok:
			// keep what we have; see above
		case expired:
			if i >= 0 {
				kept := c.session.Cookies[:0]
				for _, have := range c.session.Cookies {
					if have.Name != ck.Name {
						kept = append(kept, have)
					}
				}
				c.session.Cookies = kept
				changed = true
			}
		case i < 0:
			c.session.Cookies = append(c.session.Cookies, SavedCookie{
				Name: ck.Name, Value: ck.Value, Quoted: ck.Quoted, Domain: ck.Domain, Path: ck.Path,
			})
			changed = true
		case c.session.Cookies[i].Value != ck.Value || c.session.Cookies[i].Quoted != ck.Quoted:
			c.session.Cookies[i] = SavedCookie{
				Name: ck.Name, Value: ck.Value, Quoted: ck.Quoted, Domain: ck.Domain, Path: ck.Path,
			}
			changed = true
		}
	}
	return changed
}

func (c *Client) doAuthRequest(method, url string, body any) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, nil, err
	}

	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", c.userAgent)

	if c.frameID != "" {
		req.Header.Set("X-Apple-OAuth-State", "auth-"+c.frameID)
		req.Header.Set("X-Apple-Frame-Id", "auth-"+c.frameID)
	}
	if c.session.Scnt != "" {
		req.Header.Set("scnt", c.session.Scnt)
	}
	if c.session.SessionID != "" {
		req.Header.Set("X-Apple-ID-Session-Id", c.session.SessionID)
	}
	if c.authAttr != "" {
		req.Header.Set("X-Apple-Auth-Attributes", c.authAttr)
	}
	req.Header.Set("X-Apple-I-FD-Client-Info", fmt.Sprintf(
		`{"U":"%s","L":"en-US","Z":"GMT-05:00","V":"1.1","F":""}`, c.userAgent))

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[auth] %s %s\n", method, url)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, fmt.Errorf("reading response: %w", err)
	}

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[auth] -> %d (%d bytes)\n", resp.StatusCode, len(respBody))
	}

	c.captureAuthHeaders(resp)
	c.mergeCookies(resp.Cookies(), resp.StatusCode < 300)
	return resp, respBody, nil
}

func (c *Client) captureAuthHeaders(resp *http.Response) {
	if v := resp.Header.Get("scnt"); v != "" {
		c.session.Scnt = v
	}
	if v := resp.Header.Get("X-Apple-ID-Session-Id"); v != "" {
		c.session.SessionID = v
	}
	if v := resp.Header.Get("X-Apple-Session-Token"); v != "" {
		c.session.SessionToken = v
	}
	if v := resp.Header.Get("X-Apple-TwoSV-Trust-Token"); v != "" {
		c.session.TrustToken = v
	}
	if v := resp.Header.Get("X-Apple-ID-Account-Country"); v != "" {
		c.session.AccountCountry = v
	}
	if v := resp.Header.Get("X-Apple-Auth-Attributes"); v != "" {
		c.authAttr = v
	}
}

func (c *Client) doServiceRequest(method, url string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	for k, v := range serviceHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", c.userAgent)

	if cs := c.cookieString(); cs != "" {
		req.Header.Set("Cookie", cs)
	}

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[svc] %s %s\n", method, redactURL(url))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[svc] -> %d (%d bytes)\n", resp.StatusCode, len(respBody))
	}

	if c.mergeCookies(resp.Cookies(), resp.StatusCode < 300) && c.OnSessionUpdate != nil {
		c.OnSessionUpdate(c.session)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return respBody, &HTTPError{Status: resp.StatusCode, URL: url, Body: truncate(string(respBody), 200)}
	}

	return respBody, nil
}

func (c *Client) setupURL() string {
	if c.setupBase != "" {
		return c.setupBase
	}
	if c.session.AccountCountry == "CN" {
		return SetupEndpointCN
	}
	return SetupEndpoint
}

func (c *Client) hmeBaseURL() (string, error) {
	ws, ok := c.session.Webservices["premiummailsettings"]
	if !ok {
		return "", fmt.Errorf("premiummailsettings service not found (is iCloud+ active?)")
	}
	// Apple sometimes lists a service with no url (pyicloud#337 saw
	// `schoolwork: {}` on real accounts). Building "/v1/hme/..." from
	// it fails later as a baffling transport error; say what it is.
	if strings.TrimSpace(ws.URL) == "" {
		return "", fmt.Errorf("iCloud listed Hide My Email (premiummailsettings) without a service URL — try again; if it persists, run ihme auth login")
	}
	return ws.URL, nil
}

func (c *Client) hmeURL(version int, path string) (string, error) {
	base, err := c.hmeBaseURL()
	if err != nil {
		return "", err
	}

	params := fmt.Sprintf("?clientBuildNumber=%s&clientMasteringNumber=%s&clientId=%s&dsid=%s",
		ClientBuildNumber, ClientMasteringNumber, c.clientID, c.session.Dsid)

	return fmt.Sprintf("%s/v%d/hme/%s%s", base, version, path, params), nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
