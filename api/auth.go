package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lroolle/ihme-cli/internal/srp"
	"golang.org/x/crypto/pbkdf2"
)

type TwoFactorCallback func() (string, error)

func (c *Client) Login(appleID, password string, otpCallback TwoFactorCallback) error {
	c.session.AppleID = strings.ToLower(appleID)
	c.frameID = strings.ToLower(uuid.New().String())

	if err := c.authStart(); err != nil {
		return fmt.Errorf("auth start: %w", err)
	}

	if err := c.authFederate(c.session.AppleID); err != nil {
		return fmt.Errorf("federate: %w", err)
	}

	if err := c.srpAuthenticate(c.session.AppleID, password, otpCallback); err != nil {
		return err
	}

	if err := c.getTrust(); err != nil {
		return fmt.Errorf("get trust: %w", err)
	}

	if err := c.accountLogin(); err != nil {
		return fmt.Errorf("account login: %w", err)
	}

	return nil
}

func (c *Client) LoginWithSession() error {
	if c.session.SessionToken == "" {
		return fmt.Errorf("no session token")
	}
	return c.accountLogin()
}

func (c *Client) ResumeSession() error {
	if c.session.SessionToken == "" && c.session.TrustToken == "" {
		return fmt.Errorf("no session data")
	}

	// Try validate first (uses persisted cookies, no sign-in alert).
	// Only a definitive rejection justifies the accountLogin
	// fallback: transport trouble (timeouts, 5xx, proxy flaps, 421
	// routing hiccups) gets one quiet retry and then surfaces as
	// transient — the session is very likely still valid, and
	// accountLogin would spam sign-in alerts and mislabel the
	// failure as expiry.
	if len(c.session.Cookies) > 0 {
		err := c.ValidateSession()
		if err == nil {
			return nil
		}
		if !IsAuthRejection(err) {
			if c.Verbose {
				fmt.Fprintf(os.Stderr, "[svc] validate failed transiently, retrying once: %v\n", err)
			}
			time.Sleep(1500 * time.Millisecond)
			err = c.ValidateSession()
			if err == nil {
				return nil
			}
			if !IsAuthRejection(err) {
				return &TransientError{Err: fmt.Errorf("validating session: %w", err)}
			}
		}
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "[svc] session rejected, falling back to accountLogin\n")
		}
	}

	// Fall back to accountLogin (triggers Apple sign-in email).
	if err := c.accountLogin(); err != nil {
		if !IsAuthRejection(err) {
			return &TransientError{Err: fmt.Errorf("account login: %w", err)}
		}
		return err
	}
	return nil
}

// recoverSession re-mints service cookies after Apple rejected a
// service call mid-command. It skips the validate step ResumeSession
// starts with, on purpose: a service host can answer 401 while
// /validate still answers 200 (the cookies are scoped per service),
// so revalidating would only confirm a session the service already
// refused. accountLogin trades the stored session and trust tokens
// for fresh cookies without a password or a 2FA prompt.
func (c *Client) recoverSession() error {
	if c.session.SessionToken == "" && c.session.TrustToken == "" {
		return fmt.Errorf("recovering session: %w", ErrSessionInvalid)
	}
	if err := c.accountLogin(); err != nil {
		if !IsAuthRejection(err) {
			return &TransientError{Err: fmt.Errorf("recovering session: %w", err)}
		}
		return err
	}
	if c.OnSessionUpdate != nil {
		c.OnSessionUpdate(c.session)
	}
	return nil
}

func (c *Client) ValidateSession() error {
	_, _, err := c.ValidateSessionInfo()
	return err
}

func (c *Client) ValidateSessionInfo() (*AccountInfoResponse, json.RawMessage, error) {
	url := c.setupURL() + "/validate"
	body, err := c.doServiceRequest("POST", url, nil)

	// 421 = wrong region. Apple's response tells us the right one.
	if err != nil && len(body) > 0 {
		var errResp struct {
			RequestInfo []struct {
				Country string `json:"country"`
			} `json:"requestInfo"`
		}
		if json.Unmarshal(body, &errResp) == nil && len(errResp.RequestInfo) > 0 {
			country := errResp.RequestInfo[0].Country
			if country == "CN" && c.session.AccountCountry != "CN" {
				c.session.AccountCountry = "CN"
				if c.Verbose {
					fmt.Fprintf(os.Stderr, "[svc] 421 redirect: switching to CN endpoint\n")
				}
				return c.ValidateSessionInfo()
			}
		}
		return nil, nil, fmt.Errorf("validate session: %w", err)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("validate session: %w", err)
	}

	var resp AccountInfoResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("parsing validate response: %w", err)
	}
	if resp.Success != nil && !*resp.Success && resp.DsInfo.Dsid == "" && len(resp.Webservices) == 0 {
		return nil, json.RawMessage(body), fmt.Errorf("validate session: %w", ErrSessionInvalid)
	}

	if resp.DsInfo.Dsid != "" {
		c.session.Dsid = resp.DsInfo.Dsid
	}
	if len(resp.Webservices) > 0 {
		c.session.Webservices = resp.Webservices
	}
	c.session.ValidatedAt = time.Now()
	return &resp, json.RawMessage(body), nil
}

func (c *Client) authStart() error {
	fid := "auth-" + c.frameID
	params := fmt.Sprintf("?frame_id=%s&language=en_US&skVersion=7&iframeId=%s&client_id=%s&redirect_uri=%s&response_type=code&response_mode=web_message&state=%s&authVersion=latest",
		fid, fid, WidgetKey, "https://www.icloud.com", fid)
	url := AuthEndpoint + "/authorize/signin" + params

	resp, _, err := c.doAuthRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("auth start returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) authFederate(email string) error {
	url := AuthEndpoint + "/federate?isRememberMeEnabled=true"
	body := map[string]any{"accountName": email, "rememberMe": true}

	resp, _, err := c.doAuthRequest("POST", url, body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("federate returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) srpAuthenticate(email, password string, otpCallback TwoFactorCallback) error {
	params := srp.Apple2048Params()
	client, err := srp.NewClient(params)
	if err != nil {
		return fmt.Errorf("creating SRP client: %w", err)
	}

	initReq := AuthInitRequest{
		A:           base64.StdEncoding.EncodeToString(client.PublicKey()),
		AccountName: email,
		Protocols:   []string{"s2k", "s2k_fo"},
	}

	resp, body, err := c.doAuthRequest("POST", AuthEndpoint+"/signin/init", initReq)
	if err != nil {
		return fmt.Errorf("signin init: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("signin init returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var initResp AuthInitResponse
	if err := json.Unmarshal(body, &initResp); err != nil {
		return fmt.Errorf("parsing init response: %w", err)
	}

	salt, err := base64.StdEncoding.DecodeString(initResp.Salt)
	if err != nil {
		return fmt.Errorf("decoding salt: %w", err)
	}
	serverB, err := base64.StdEncoding.DecodeString(initResp.B)
	if err != nil {
		return fmt.Errorf("decoding B: %w", err)
	}

	passKey := derivePassword(password, salt, initResp.Iteration, initResp.Protocol)

	if err := client.ProcessChallenge([]byte(email), passKey, salt, serverB); err != nil {
		return fmt.Errorf("SRP challenge: %w", err)
	}

	trustTokens := []string{}
	if c.session.TrustToken != "" {
		trustTokens = append(trustTokens, c.session.TrustToken)
	}

	// Apple's protocol requires both M1 and M2 in the complete request.
	// Confirmed by pyicloud, Go-iClient, rclone, and icloud.js.
	completeReq := AuthCompleteRequest{
		AccountName: email,
		C:           initResp.C,
		M1:          base64.StdEncoding.EncodeToString(client.Proof()),
		M2:          base64.StdEncoding.EncodeToString(client.ServerProof()),
		RememberMe:  true,
		TrustTokens: trustTokens,
	}

	resp, body, err = c.doAuthRequest("POST", AuthEndpoint+"/signin/complete?isRememberMeEnabled=true", completeReq)
	if err != nil {
		return fmt.Errorf("signin complete: %w", err)
	}

	switch resp.StatusCode {
	case 200:
		return nil
	case 409:
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "[auth] 409 body: %s\n", string(body))
		}
		return c.handle2FA(otpCallback)
	case 403:
		return fmt.Errorf("invalid credentials")
	case 412:
		return fmt.Errorf("account requires acknowledgment at appleid.apple.com")
	default:
		return fmt.Errorf("signin complete returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
}

func (c *Client) handle2FA(otpCallback TwoFactorCallback) error {
	if otpCallback == nil {
		return fmt.Errorf("two-factor authentication required but no callback provided")
	}

	_, authBody, err := c.doAuthRequest("GET", AuthEndpoint, nil)
	if err != nil {
		return fmt.Errorf("fetching auth options: %w", err)
	}
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[auth] auth options: %s\n", truncate(string(authBody), 2000))
	}

	var opts AuthOptionsResponse
	if err := json.Unmarshal(authBody, &opts); err != nil {
		return fmt.Errorf("parsing auth options: %w", err)
	}

	// Some accounts nest auth state under phoneNumberVerification
	if opts.PhoneNumberVerification != nil {
		if len(opts.TrustedPhoneNumbers) == 0 {
			opts.TrustedPhoneNumbers = opts.PhoneNumberVerification.TrustedPhoneNumbers
		}
	}

	if len(opts.TrustedPhoneNumbers) > 0 {
		// Check if any phone supports push (vs SMS-only)
		hasPush := false
		for _, p := range opts.TrustedPhoneNumbers {
			if p.PushMode != "sms" {
				hasPush = true
				break
			}
		}
		if !hasPush {
			return c.handle2FASMS(opts.TrustedPhoneNumbers, otpCallback)
		}
	}

	return c.handle2FADevice(otpCallback)
}

func (c *Client) handle2FASMS(phones []TrustedPhoneNumber, otpCallback TwoFactorCallback) error {
	// Use first phone number by default
	phone := phones[0]
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[auth] requesting SMS to %s (id=%d)\n", phone.NumberWithDialCode, phone.ID)
	}

	// Request SMS: PUT /verify/phone
	smsReq := map[string]any{
		"phoneNumber": map[string]any{"id": phone.ID},
		"mode":        "sms",
	}
	resp, _, err := c.doAuthRequest("PUT", AuthEndpoint+"/verify/phone", smsReq)
	if err != nil {
		return fmt.Errorf("requesting SMS: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("SMS request returned %d", resp.StatusCode)
	}

	fmt.Fprintf(os.Stderr, "Verification code sent to %s\n", phone.NumberWithDialCode)

	code, err := otpCallback()
	if err != nil {
		return fmt.Errorf("getting 2FA code: %w", err)
	}

	// Submit SMS code: POST /verify/phone/securitycode
	body := map[string]any{
		"phoneNumber":  map[string]any{"id": phone.ID},
		"securityCode": map[string]string{"code": code},
		"mode":         "sms",
	}
	resp, respBody, err := c.doAuthRequest("POST", AuthEndpoint+"/verify/phone/securitycode", body)
	if err != nil {
		return fmt.Errorf("submitting SMS code: %w", err)
	}

	switch {
	case resp.StatusCode == 200 || resp.StatusCode == 204:
		return nil
	case codeAcceptedDespiteConflict(resp):
		return nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return fmt.Errorf("incorrect verification code")
	default:
		return fmt.Errorf("SMS verification returned %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
}

func (c *Client) handle2FADevice(otpCallback TwoFactorCallback) error {
	// Explicitly request push notification to trusted devices.
	// Required for iOS 26.4+ where the SRP 409 no longer auto-pushes.
	// See rclone session.go RequestPushNotification.
	c.doAuthRequest("PUT", AuthEndpoint+"/verify/trusteddevice/securitycode", nil)

	code, err := otpCallback()
	if err != nil {
		return fmt.Errorf("getting 2FA code: %w", err)
	}

	body := map[string]any{
		"securityCode": map[string]string{"code": code},
	}
	resp, respBody, err := c.doAuthRequest("POST", AuthEndpoint+"/verify/trusteddevice/securitycode", body)
	if err != nil {
		return fmt.Errorf("submitting 2FA code: %w", err)
	}

	switch {
	case resp.StatusCode == 200 || resp.StatusCode == 204:
		return nil
	case codeAcceptedDespiteConflict(resp):
		return nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return fmt.Errorf("incorrect verification code")
	default:
		return fmt.Errorf("2FA verification returned %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
}

// codeAcceptedDespiteConflict reports whether Apple accepted a
// security code while answering 409. Since ~mid-2026 the
// securitycode endpoints answer 409 even for a valid code; the body
// says securityCode.valid=true and — the part worth trusting —
// Apple issues a fresh X-Apple-Session-Token, which it only does on
// success. doAuthRequest has already absorbed those headers by the
// time this runs, so accepting here continues straight to the trust
// step. Verified against rclone's fix for the same break
// (backend/iclouddrive/api/session.go, rclone#9488, 2026-07-29).
func codeAcceptedDespiteConflict(resp *http.Response) bool {
	return resp != nil &&
		resp.StatusCode == 409 &&
		resp.Header.Get("X-Apple-Session-Token") != ""
}

func (c *Client) getTrust() error {
	url := AuthEndpoint + "/2sv/trust"
	resp, body, err := c.doAuthRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("get trust returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

func (c *Client) accountLogin() error {
	url := c.setupURL() + "/accountLogin"
	req := AccountLoginRequest{
		AccountCountryCode: c.session.AccountCountry,
		DsWebAuthToken:     c.session.SessionToken,
		ExtendedLogin:      true,
		TrustToken:         c.session.TrustToken,
	}

	body, err := c.doServiceRequest("POST", url, req)

	// 421 = wrong region, retry with CN
	if err != nil && len(body) > 0 {
		var errResp struct {
			RequestInfo []struct {
				Country string `json:"country"`
			} `json:"requestInfo"`
		}
		if json.Unmarshal(body, &errResp) == nil && len(errResp.RequestInfo) > 0 {
			country := errResp.RequestInfo[0].Country
			if country == "CN" && c.session.AccountCountry != "CN" {
				c.session.AccountCountry = "CN"
				if c.Verbose {
					fmt.Fprintf(os.Stderr, "[svc] 421 redirect: switching to CN endpoint\n")
				}
				return c.accountLogin()
			}
		}
		return fmt.Errorf("account login: %w", err)
	}
	if err != nil {
		return fmt.Errorf("account login: %w", err)
	}

	var resp AccountLoginResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}

	c.session.Dsid = resp.DsInfo.Dsid
	c.session.Webservices = resp.Webservices
	c.session.ValidatedAt = time.Now()
	return nil
}

func derivePassword(password string, salt []byte, iterations int, protocol string) []byte {
	passHash := sha256.Sum256([]byte(password))

	var passInput []byte
	switch protocol {
	case "s2k_fo":
		passInput = []byte(fmt.Sprintf("%X", passHash))
	default:
		passInput = passHash[:]
	}

	return pbkdf2.Key(passInput, salt, iterations, 32, sha256.New)
}
