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
	// OnSessionUpdate, when set, is called after the client re-mints
	// a session mid-command. The session lives longer than the
	// process, so whoever owns the file gets a chance to persist the
	// fresh cookies instead of losing them at exit.
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
// Deduplicates by name (last value wins) to handle legacy sessions that
// accumulated copies across domains.
func (c *Client) cookieString() string {
	seen := make(map[string]struct{}, len(c.session.Cookies))
	var b strings.Builder
	first := true
	for _, ck := range c.session.Cookies {
		if ck.Value == "" {
			continue
		}
		if _, dup := seen[ck.Name]; dup {
			continue
		}
		seen[ck.Name] = struct{}{}
		if !first {
			b.WriteString("; ")
		}
		first = false
		b.WriteString(ck.Name)
		b.WriteByte('=')
		b.WriteString(ck.Value)
	}
	return b.String()
}

// mergeCookies merges response cookies into the session cookie list.
// Updates existing cookies by name, appends new ones.
func (c *Client) mergeCookies(cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	idx := make(map[string]int, len(c.session.Cookies))
	for i, ck := range c.session.Cookies {
		idx[ck.Name] = i
	}
	for _, ck := range cookies {
		if ck.Value == "" {
			continue
		}
		sc := SavedCookie{
			Name:   ck.Name,
			Value:  ck.Value,
			Domain: ck.Domain,
			Path:   ck.Path,
		}
		if i, ok := idx[ck.Name]; ok {
			c.session.Cookies[i] = sc
		} else {
			c.session.Cookies = append(c.session.Cookies, sc)
			idx[ck.Name] = len(c.session.Cookies) - 1
		}
	}
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
	c.mergeCookies(resp.Cookies())
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

	c.mergeCookies(resp.Cookies())

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
