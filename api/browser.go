package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SourceBrowser marks a session signed in at icloud.com in a browser
// and handed to ihme as cookies. The browser did the password, the
// second factor (code, security key) and any device approval, so
// such a session carries no session or trust token: nothing can mint
// new cookies once Apple retires these, and recovery means copying
// them again, not accountLogin.
const SourceBrowser = "browser"

// ErrBrowserSessionExpired is a rejected browser session. It is an
// ErrSessionInvalid, with a different fix: sign in at icloud.com and
// copy the cookies again.
var ErrBrowserSessionExpired = fmt.Errorf("browser session expired: %w", ErrSessionInvalid)

// ParseBrowserCookies reads the cookies a browser sends to iCloud.
// It accepts a Cookie request header as DevTools shows it (with or
// without the "Cookie:" name, or wrapped in single quotes the way a
// copied curl -H carries it) or a cookie editor's JSON export, of
// which only icloud.com cookies are kept. A name given twice keeps
// its first value: a browser lists the cookie with the most specific
// path first.
func ParseBrowserCookies(text string) ([]SavedCookie, error) {
	text = strings.TrimSpace(text)
	var cookies []SavedCookie
	var err error
	if strings.HasPrefix(text, "[") {
		cookies, err = parseCookieJSON(text)
	} else {
		cookies = parseCookieHeader(text)
	}
	if err != nil {
		return nil, err
	}

	signedIn := false
	for _, ck := range cookies {
		if strings.HasPrefix(ck.Name, "X-APPLE-WEBAUTH-") {
			signedIn = true
		}
	}
	if !signedIn {
		return nil, errors.New("no iCloud sign-in cookies (X-APPLE-WEBAUTH-*) in the input; copy the Cookie header of a request to setup.icloud.com while signed in at icloud.com")
	}
	return cookies, nil
}

func parseCookieHeader(text string) []SavedCookie {
	text = strings.Trim(text, "'")
	if name, rest, ok := strings.Cut(text, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "cookie") {
		text = rest
	}
	var cookies []SavedCookie
	for _, pair := range strings.Split(text, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		cookies = addBrowserCookie(cookies, strings.TrimSpace(name), strings.TrimSpace(value))
	}
	return cookies
}

func parseCookieJSON(text string) ([]SavedCookie, error) {
	var exported []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(text), &exported); err != nil {
		return nil, fmt.Errorf("reading cookie export: %w", err)
	}
	var cookies []SavedCookie
	for _, e := range exported {
		domain := strings.TrimPrefix(e.Domain, ".")
		if domain != "" && !isICloudDomain(domain) {
			continue
		}
		cookies = addBrowserCookie(cookies, e.Name, e.Value)
	}
	return cookies, nil
}

func isICloudDomain(domain string) bool {
	for _, d := range []string{"icloud.com", "icloud.com.cn"} {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// addBrowserCookie appends one cookie unless its name is already
// there. A value in double quotes is stored bare with Quoted set, as
// mergeCookies stores the quoted values Apple sets.
func addBrowserCookie(cookies []SavedCookie, name, value string) []SavedCookie {
	if name == "" || value == "" {
		return cookies
	}
	for _, have := range cookies {
		if have.Name == name {
			return cookies
		}
	}
	quoted := len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"'
	if quoted {
		value = value[1 : len(value)-1]
	}
	return append(cookies, SavedCookie{
		Name: name, Value: value, Quoted: quoted, Domain: ".icloud.com", Path: "/",
	})
}

// ImportBrowserSession turns the session's browser cookies into a
// working session: /validate confirms them and returns the account
// and its service map, and Apple's verdict on the account is checked
// as after a password sign-in.
func (c *Client) ImportBrowserSession() error {
	c.session.Source = SourceBrowser
	info, _, err := c.ValidateSessionInfo()
	if err != nil {
		if IsAuthRejection(err) {
			return fmt.Errorf("iCloud did not accept these cookies (signed out, expired, or copied from another site): %w", err)
		}
		return err
	}
	if info.DsInfo.PrimaryEmail != "" {
		c.session.AppleID = strings.ToLower(info.DsInfo.PrimaryEmail)
	}
	return c.checkAccount(info)
}
