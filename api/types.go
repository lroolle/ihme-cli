package api

import (
	"encoding/json"
	"time"
)

type HmeEmail struct {
	Origin          string `json:"origin"`
	AnonymousID     string `json:"anonymousId"`
	Domain          string `json:"domain"`
	ForwardToEmail  string `json:"forwardToEmail"`
	Hme             string `json:"hme"`
	IsActive        bool   `json:"isActive"`
	Label           string `json:"label"`
	Note            string `json:"note"`
	CreateTimestamp int64  `json:"createTimestamp"`
	RecipientMailID string `json:"recipientMailId"`
}

func (h *HmeEmail) CreatedAt() time.Time {
	return time.UnixMilli(h.CreateTimestamp)
}

type ListHmeResult struct {
	HmeEmails       []HmeEmail `json:"hmeEmails"`
	SelectedFwdTo   string     `json:"selectedForwardTo"`
	ForwardToEmails []string   `json:"forwardToEmails"`
}

type HmeResponse struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Error   *HmeError       `json:"error,omitempty"`
}

type HmeError struct {
	ErrorMessage string `json:"errorMessage"`
	ErrorCode    int    `json:"errorCode,omitempty"`
}

type AuthInitRequest struct {
	A           string   `json:"a"`
	AccountName string   `json:"accountName"`
	Protocols   []string `json:"protocols"`
}

type AuthInitResponse struct {
	Iteration int    `json:"iteration"`
	Salt      string `json:"salt"`
	Protocol  string `json:"protocol"`
	B         string `json:"b"`
	C         string `json:"c"`
}

type AuthCompleteRequest struct {
	AccountName string   `json:"accountName"`
	C           string   `json:"c"`
	M1          string   `json:"m1"`
	M2          string   `json:"m2"`
	RememberMe  bool     `json:"rememberMe"`
	TrustTokens []string `json:"trustTokens"`
}

type AccountLoginRequest struct {
	AccountCountryCode string `json:"accountCountryCode"`
	DsWebAuthToken     string `json:"dsWebAuthToken"`
	ExtendedLogin      bool   `json:"extended_login"`
	TrustToken         string `json:"trustToken"`
}

type AccountLoginResponse struct {
	DsInfo      DsInfo                       `json:"dsInfo"`
	Webservices map[string]WebserviceEndpoint `json:"webservices"`
	Success     bool                         `json:"success"`
}

type DsInfo struct {
	Dsid       string `json:"dsid"`
	LastName   string `json:"lastName"`
	FirstName  string `json:"firstName"`
	PrimaryEmail string `json:"appleId"`
}

type WebserviceEndpoint struct {
	URL    string `json:"url"`
	Status string `json:"status"`
}

type AuthOptionsResponse struct {
	PhoneNumberVerification *PhoneNumberVerification `json:"phoneNumberVerification,omitempty"`
	TrustedPhoneNumbers     []TrustedPhoneNumber     `json:"trustedPhoneNumbers,omitempty"`
	TrustedDevices          []TrustedDevice          `json:"trustedDevices,omitempty"`
	NoTrustedDevices        bool                     `json:"noTrustedDevices,omitempty"`
}

type PhoneNumberVerification struct {
	TrustedPhoneNumbers []TrustedPhoneNumber `json:"trustedPhoneNumbers"`
}

type TrustedPhoneNumber struct {
	ID                 int    `json:"id"`
	NumberWithDialCode string `json:"numberWithDialCode"`
	ObfuscatedNumber   string `json:"obfuscatedNumber"`
	LastTwoDigits      string `json:"lastTwoDigits"`
	PushMode           string `json:"pushMode"`
}

type TrustedDevice struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type SessionData struct {
	AppleID        string                       `json:"appleId"`
	SessionToken   string                       `json:"sessionToken"`
	TrustToken     string                       `json:"trustToken"`
	Scnt           string                       `json:"scnt"`
	SessionID      string                       `json:"sessionId"`
	AccountCountry string                       `json:"accountCountry"`
	Dsid           string                       `json:"dsid"`
	Webservices    map[string]WebserviceEndpoint `json:"webservices"`
	Cookies        []SavedCookie                `json:"cookies,omitempty"`
	SavedAt        time.Time                    `json:"savedAt"`
}

type SavedCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}
