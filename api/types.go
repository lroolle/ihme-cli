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

// HmeError is the error member of an HME response envelope. Apple is
// inconsistent about its shape: usually an object, but errorCode may be
// a number or a quoted string (reserve sends "-41015"-style strings),
// and some responses carry a bare scalar with no object at all
// ({"success":false,"error":1}). Accept all of them — a parse failure
// here hides the real error from the user.
type HmeError struct {
	ErrorMessage string `json:"errorMessage"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

func (e *HmeError) UnmarshalJSON(data []byte) error {
	var obj struct {
		ErrorMessage string          `json:"errorMessage"`
		ErrorCode    json.RawMessage `json:"errorCode"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		e.ErrorMessage = obj.ErrorMessage
		e.ErrorCode = scalarText(obj.ErrorCode)
		return nil
	}
	e.ErrorMessage = ""
	e.ErrorCode = scalarText(data)
	return nil
}

// scalarText renders a JSON scalar (quoted string or number) as its
// bare text, "" for empty/null.
func scalarText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
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

type AccountInfoResponse struct {
	DsInfo      DsInfo                        `json:"dsInfo"`
	Webservices map[string]WebserviceEndpoint `json:"webservices"`
	Success     *bool                         `json:"success,omitempty"`
	// Apple's verdict on the session's second factor. Services
	// refuse a session that still owes 2FA with 401 "Invalid global
	// session".
	HsaChallengeRequired *bool `json:"hsaChallengeRequired,omitempty"`
	HsaTrustedBrowser    *bool `json:"hsaTrustedBrowser,omitempty"`
}

type AccountLoginResponse = AccountInfoResponse

type DsInfo struct {
	Dsid           string `json:"dsid"`
	LastName       string `json:"lastName"`
	FirstName      string `json:"firstName"`
	PrimaryEmail   string `json:"appleId"`
	CountryCode    string `json:"countryCode"`
	HsaVersion     int    `json:"hsaVersion"`
	StatusCode     int    `json:"statusCode"`
	ManagedAppleID bool   `json:"isManagedAppleID"`
	// Pointers: absent and false mean different things, and Apple
	// omits some of these on some accounts.
	HideMyEmailActive    *bool `json:"isHideMyEmailSubscriptionActive,omitempty"`
	HideMyEmailAvailable *bool `json:"isHideMyEmailFeatureAvailable,omitempty"`
	// WebAccessAllowed is the "Access iCloud Data on the Web" setting.
	WebAccessAllowed *bool `json:"isWebAccessAllowed,omitempty"`
}

type WebserviceEndpoint struct {
	URL    string `json:"url"`
	Status string `json:"status"`
	// PcsRequired marks a service behind Advanced Data Protection:
	// web access needs approval on a trusted device (requestPCS).
	PcsRequired bool `json:"pcsRequired,omitempty"`
}

type AuthOptionsResponse struct {
	PhoneNumberVerification *PhoneNumberVerification `json:"phoneNumberVerification,omitempty"`
	TrustedPhoneNumbers     []TrustedPhoneNumber     `json:"trustedPhoneNumbers,omitempty"`
	TrustedDevices          []TrustedDevice          `json:"trustedDevices,omitempty"`
	NoTrustedDevices        bool                     `json:"noTrustedDevices,omitempty"`
	// Set when the account's second factor is a hardware security
	// key. Apple then sends no code anywhere; it wants a WebAuthn
	// assertion instead. The challenge comes either at the top level
	// or nested under fsaVerification (seen by rclone#9734 testers).
	SecurityKeyVerification
	FsaVerification *SecurityKeyVerification `json:"fsaVerification,omitempty"`
	KeyNames        []string                 `json:"keyNames,omitempty"`
}

// SecurityKeyVerification is the security-key part of Apple's 2FA
// options.
type SecurityKeyVerification struct {
	FsaChallenge *SecurityKeyChallenge `json:"fsaChallenge,omitempty"`
	// RequirePrf asks for the WebAuthn PRF extension, which no
	// client of this flow supports.
	RequirePrf bool `json:"requirePrf,omitempty"`
}

// securityKey returns the security-key verification wherever Apple put
// it, or nil for an account that uses codes.
func (o *AuthOptionsResponse) securityKey() *SecurityKeyVerification {
	if o.FsaChallenge != nil {
		return &o.SecurityKeyVerification
	}
	if o.FsaVerification != nil && o.FsaVerification.FsaChallenge != nil {
		return o.FsaVerification
	}
	return nil
}

// SecurityKeyChallenge is Apple's WebAuthn request (fsaChallenge).
// Challenge and KeyHandles are base64url, as Apple sends them.
type SecurityKeyChallenge struct {
	Challenge  string   `json:"challenge"`
	KeyHandles []string `json:"keyHandles"`
	// AllowedCredentials carries the same credential IDs, comma
	// separated; XcodesLoginKit builds its allow list from it.
	AllowedCredentials string `json:"allowedCredentials,omitempty"`
	RpID               string `json:"rpId"`
	// KeyNames are the names the user gave the keys, for the prompt.
	KeyNames []string `json:"-"`
}

// SecurityKeyAssertion is a security key's signed answer to a
// SecurityKeyChallenge, as raw bytes.
type SecurityKeyAssertion struct {
	ClientData        []byte
	AuthenticatorData []byte
	Signature         []byte
	UserHandle        []byte
	CredentialID      []byte
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
	AppleID        string                        `json:"appleId"`
	SessionToken   string                        `json:"sessionToken"`
	TrustToken     string                        `json:"trustToken"`
	Scnt           string                        `json:"scnt"`
	SessionID      string                        `json:"sessionId"`
	AccountCountry string                        `json:"accountCountry"`
	Dsid           string                        `json:"dsid"`
	Webservices    map[string]WebserviceEndpoint `json:"webservices"`
	Cookies        []SavedCookie                 `json:"cookies,omitempty"`
	// ClientID identifies this installation to Apple. Generated once
	// and kept: a new id per invocation is a stranger every time.
	ClientID string    `json:"clientId,omitempty"`
	SavedAt  time.Time `json:"savedAt"`
	// Source is SourceBrowser for a session imported from a browser,
	// empty for a password sign-in.
	Source string `json:"source,omitempty"`
	// ValidatedAt is the last time Apple confirmed this session
	// (successful validate or accountLogin). Fresh sessions skip
	// the per-command validate round trip.
	ValidatedAt time.Time `json:"validatedAt,omitempty"`
}

type SavedCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Quoted records that Apple set the value in double quotes;
	// cookieString puts them back. Older sessions lack it and replay
	// bare values, as before.
	Quoted bool   `json:"quoted,omitempty"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
}
