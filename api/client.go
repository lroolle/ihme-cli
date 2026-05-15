package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/publicsuffix"
)

type Client struct {
	http       *http.Client
	session    *SessionData
	clientID   string
	frameID    string
	authAttr   string
	userAgent  string
	Verbose    bool
}

func NewClient() (*Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}

	return &Client{
		http: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		clientID:  uuid.New().String(),
		session:   &SessionData{},
		userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
	}, nil
}

func NewClientWithSession(sess *SessionData) (*Client, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	c.session = sess
	c.restoreCookies()
	c.broadcastCookies()
	return c, nil
}

func (c *Client) Session() *SessionData {
	return c.session
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
	req.Header.Set("X-Apple-I-FD-Client-Info", `{"U":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15","L":"en-US","Z":"GMT-05:00","V":"1.1","F":""}`)

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

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[svc] %s %s\n", method, url)
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

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return respBody, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, truncate(string(respBody), 200))
	}

	return respBody, nil
}

func (c *Client) setupURL() string {
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

func (c *Client) broadcastCookies() {
	// Collect cookies from all known iCloud domains
	sources := []string{
		"https://setup.icloud.com",
		"https://setup.icloud.com.cn",
		"https://www.icloud.com",
		"https://idmsa.apple.com",
	}
	var all []*http.Cookie
	seen := make(map[string]bool)
	for _, s := range sources {
		u, _ := url.Parse(s)
		for _, ck := range c.http.Jar.Cookies(u) {
			if !seen[ck.Name] {
				seen[ck.Name] = true
				all = append(all, ck)
			}
		}
	}

	// Push to all webservice domains
	targets := []string{"https://www.icloud.com", "https://icloud.com"}
	for _, ws := range c.session.Webservices {
		if ws.URL != "" {
			targets = append(targets, ws.URL)
		}
	}
	for _, t := range targets {
		u, _ := url.Parse(t)
		if u != nil {
			c.http.Jar.SetCookies(u, all)
		}
	}
}

func (c *Client) saveCookies() {
	domains := []string{
		"https://www.icloud.com",
		"https://icloud.com",
		"https://setup.icloud.com",
		"https://setup.icloud.com.cn",
		"https://idmsa.apple.com",
	}
	for _, ws := range c.session.Webservices {
		if ws.URL != "" {
			domains = append(domains, ws.URL)
		}
	}
	var saved []SavedCookie
	for _, d := range domains {
		u, _ := url.Parse(d)
		if u == nil {
			continue
		}
		for _, ck := range c.http.Jar.Cookies(u) {
			saved = append(saved, SavedCookie{
				Name:   ck.Name,
				Value:  ck.Value,
				Domain: u.Host,
				Path:   "/",
			})
		}
	}
	c.session.Cookies = saved
}

func (c *Client) restoreCookies() {
	for _, d := range []string{
		"https://www.icloud.com",
		"https://icloud.com",
		"https://setup.icloud.com",
		"https://setup.icloud.com.cn",
		"https://idmsa.apple.com",
	} {
		u, _ := url.Parse(d)
		if u == nil {
			continue
		}
		var cookies []*http.Cookie
		for _, sc := range c.session.Cookies {
			if sc.Domain == u.Host {
				cookies = append(cookies, &http.Cookie{
					Name:  sc.Name,
					Value: sc.Value,
					Path:  sc.Path,
				})
			}
		}
		if len(cookies) > 0 {
			c.http.Jar.SetCookies(u, cookies)
		}
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
