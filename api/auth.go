package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lroolle/ihme-cli/internal/srp"
	"golang.org/x/crypto/pbkdf2"
)

type TwoFactorCallback func() (string, error)

func (c *Client) Login(appleID, password string, otpCallback TwoFactorCallback) error {
	c.session.AppleID = strings.ToLower(appleID)
	c.frameID = "auth-" + uuid.New().String()

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

func (c *Client) ValidateSession() error {
	url := SetupEndpoint + "/validate"
	body, err := c.doServiceRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("validate session: %w", err)
	}

	var resp AccountLoginResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing validate response: %w", err)
	}

	if resp.DsInfo.Dsid != "" {
		c.session.Dsid = resp.DsInfo.Dsid
	}
	if len(resp.Webservices) > 0 {
		c.session.Webservices = resp.Webservices
	}
	return nil
}

func (c *Client) authStart() error {
	params := fmt.Sprintf("?frame_id=%s&language=en_US&skVersion=7&iframeId=%s&client_id=%s&redirect_uri=%s&response_type=code&response_mode=web_message&state=%s&authVersion=latest",
		c.frameID, c.frameID, WidgetKey, "https://www.icloud.com", c.frameID)
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

	code, err := otpCallback()
	if err != nil {
		return fmt.Errorf("getting 2FA code: %w", err)
	}

	url := AuthEndpoint + "/verify/trusteddevice/securitycode"
	body := map[string]any{
		"securityCode": map[string]string{"code": code},
	}

	resp, respBody, err := c.doAuthRequest("POST", url, body)
	if err != nil {
		return fmt.Errorf("submitting 2FA code: %w", err)
	}

	switch resp.StatusCode {
	case 200, 204:
		return nil
	case 401:
		return fmt.Errorf("incorrect verification code")
	default:
		return fmt.Errorf("2FA verification returned %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}
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
	url := SetupEndpoint + "/accountLogin"
	req := AccountLoginRequest{
		AccountCountryCode: c.session.AccountCountry,
		DsWebAuthToken:     c.session.SessionToken,
		ExtendedLogin:      true,
		TrustToken:         c.session.TrustToken,
	}

	body, err := c.doServiceRequest("POST", url, req)
	if err != nil {
		return fmt.Errorf("account login: %w", err)
	}

	var resp AccountLoginResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing login response: %w", err)
	}

	c.session.Dsid = resp.DsInfo.Dsid
	c.session.Webservices = resp.Webservices
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
