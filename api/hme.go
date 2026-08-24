package api

import (
	"encoding/json"
	"fmt"
	"os"
)

// hmeRequest performs one HME call, recovering once from a session
// rejection. Apple hands the mail-domain host its own cookies, so it
// can answer 401 minutes after /validate said the session was fine —
// the pre-flight check in cmdutil cannot see that coming. A rejected
// call changed nothing on Apple's side, so replaying it after
// re-auth is safe even for the mutating paths.
//
// The URL is rebuilt after recovery on purpose: accountLogin returns
// a fresh webservices map, and the mail-domain host (p137-, p52-, …)
// and dsid can both move.
func (c *Client) hmeRequest(version int, path, method string, body any) ([]byte, error) {
	url, err := c.hmeURL(version, path)
	if err != nil {
		return nil, err
	}

	respBody, err := c.doServiceRequest(method, url, body)
	if err == nil || !IsAuthRejection(err) {
		return respBody, err
	}

	if c.Verbose {
		fmt.Fprintf(os.Stderr, "[svc] %s rejected the session, re-authenticating once\n", path)
	}
	if rerr := c.recoverSession(); rerr != nil {
		// Transport trouble during recovery proves nothing about the
		// session — say "try again", not "sign in again". A refused
		// recovery confirms the rejection, and the service's own
		// verdict stays the diagnosis.
		if IsTransient(rerr) {
			return nil, rerr
		}
		return nil, fmt.Errorf("%w (re-auth: %v)", err, rerr)
	}

	url, err = c.hmeURL(version, path)
	if err != nil {
		return nil, err
	}
	return c.doServiceRequest(method, url, body)
}

func (c *Client) ListHme() (*ListHmeResult, error) {
	body, err := c.hmeRequest(2, "list", "GET", nil)
	if err != nil {
		return nil, fmt.Errorf("listing HME: %w", err)
	}

	var resp struct {
		Success bool          `json:"success"`
		Result  ListHmeResult `json:"result"`
		Error   *HmeError     `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing list response: %w", err)
	}
	if !resp.Success {
		return nil, hmeErr("list", resp.Error)
	}
	return &resp.Result, nil
}

func (c *Client) GenerateHme() (string, error) {
	body, err := c.hmeRequest(1, "generate", "POST", map[string]string{"langCode": "en-us"})
	if err != nil {
		return "", fmt.Errorf("generating HME: %w", err)
	}

	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Hme string `json:"hme"`
		} `json:"result"`
		Error *HmeError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing generate response: %w", err)
	}
	if !resp.Success {
		return "", hmeErr("generate", resp.Error)
	}
	return resp.Result.Hme, nil
}

func (c *Client) ReserveHme(hme, label, note string) (*HmeEmail, error) {
	reqBody := map[string]string{"hme": hme, "label": label}
	if note != "" {
		reqBody["note"] = note
	}

	body, err := c.hmeRequest(1, "reserve", "POST", reqBody)
	if err != nil {
		return nil, fmt.Errorf("reserving HME: %w", err)
	}

	var resp struct {
		Success bool `json:"success"`
		Result  struct {
			Hme HmeEmail `json:"hme"`
		} `json:"result"`
		Error *HmeError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing reserve response: %w", err)
	}
	if !resp.Success {
		return nil, hmeErr("reserve", resp.Error)
	}
	return &resp.Result.Hme, nil
}

func (c *Client) UpdateHmeMetadata(anonymousID, label, note string) error {
	reqBody := map[string]string{
		"anonymousId": anonymousID,
		"label":       label,
	}
	if note != "" {
		reqBody["note"] = note
	}

	body, err := c.hmeRequest(1, "updateMetaData", "POST", reqBody)
	if err != nil {
		return fmt.Errorf("updating HME: %w", err)
	}
	return checkSuccess("updateMetaData", body)
}

func (c *Client) DeactivateHme(anonymousID string) error {
	body, err := c.hmeRequest(1, "deactivate", "POST", map[string]string{"anonymousId": anonymousID})
	if err != nil {
		return fmt.Errorf("deactivating HME: %w", err)
	}
	return checkSuccess("deactivate", body)
}

func (c *Client) ReactivateHme(anonymousID string) error {
	body, err := c.hmeRequest(1, "reactivate", "POST", map[string]string{"anonymousId": anonymousID})
	if err != nil {
		return fmt.Errorf("reactivating HME: %w", err)
	}
	return checkSuccess("reactivate", body)
}

func (c *Client) DeleteHme(anonymousID string) error {
	body, err := c.hmeRequest(1, "delete", "POST", map[string]string{"anonymousId": anonymousID})
	if err != nil {
		return fmt.Errorf("deleting HME: %w", err)
	}
	return checkSuccess("delete", body)
}

func (c *Client) UpdateForwardTo(email string) error {
	body, err := c.hmeRequest(1, "updateForwardTo", "POST", map[string]string{"forwardToEmail": email})
	if err != nil {
		return fmt.Errorf("updating forward-to: %w", err)
	}
	return checkSuccess("updateForwardTo", body)
}

func checkSuccess(operation string, body []byte) error {
	var resp struct {
		Success bool      `json:"success"`
		Error   *HmeError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing %s response: %w", operation, err)
	}
	if !resp.Success {
		return hmeErr(operation, resp.Error)
	}
	return nil
}

func hmeErr(operation string, e *HmeError) error {
	switch {
	case e == nil:
		return fmt.Errorf("%s failed", operation)
	case e.ErrorMessage != "" && e.ErrorCode != "":
		return fmt.Errorf("%s failed: %s (code %s)", operation, e.ErrorMessage, e.ErrorCode)
	case e.ErrorMessage != "":
		return fmt.Errorf("%s failed: %s", operation, e.ErrorMessage)
	case e.ErrorCode != "":
		return fmt.Errorf("%s failed (code %s)", operation, e.ErrorCode)
	default:
		return fmt.Errorf("%s failed", operation)
	}
}
