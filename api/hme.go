package api

import (
	"encoding/json"
	"fmt"
)

func (c *Client) ListHme() (*ListHmeResult, error) {
	url, err := c.hmeURL(2, "list")
	if err != nil {
		return nil, err
	}

	body, err := c.doServiceRequest("GET", url, nil)
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
	url, err := c.hmeURL(1, "generate")
	if err != nil {
		return "", err
	}

	body, err := c.doServiceRequest("POST", url, map[string]string{"langCode": "en-us"})
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
	url, err := c.hmeURL(1, "reserve")
	if err != nil {
		return nil, err
	}

	reqBody := map[string]string{"hme": hme, "label": label}
	if note != "" {
		reqBody["note"] = note
	}

	body, err := c.doServiceRequest("POST", url, reqBody)
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
	url, err := c.hmeURL(1, "updateMetaData")
	if err != nil {
		return err
	}

	reqBody := map[string]string{
		"anonymousId": anonymousID,
		"label":       label,
	}
	if note != "" {
		reqBody["note"] = note
	}

	body, err := c.doServiceRequest("POST", url, reqBody)
	if err != nil {
		return fmt.Errorf("updating HME: %w", err)
	}
	return checkSuccess("updateMetaData", body)
}

func (c *Client) DeactivateHme(anonymousID string) error {
	url, err := c.hmeURL(1, "deactivate")
	if err != nil {
		return err
	}

	body, err := c.doServiceRequest("POST", url, map[string]string{"anonymousId": anonymousID})
	if err != nil {
		return fmt.Errorf("deactivating HME: %w", err)
	}
	return checkSuccess("deactivate", body)
}

func (c *Client) ReactivateHme(anonymousID string) error {
	url, err := c.hmeURL(1, "reactivate")
	if err != nil {
		return err
	}

	body, err := c.doServiceRequest("POST", url, map[string]string{"anonymousId": anonymousID})
	if err != nil {
		return fmt.Errorf("reactivating HME: %w", err)
	}
	return checkSuccess("reactivate", body)
}

func (c *Client) DeleteHme(anonymousID string) error {
	url, err := c.hmeURL(1, "delete")
	if err != nil {
		return err
	}

	body, err := c.doServiceRequest("POST", url, map[string]string{"anonymousId": anonymousID})
	if err != nil {
		return fmt.Errorf("deleting HME: %w", err)
	}
	return checkSuccess("delete", body)
}

func (c *Client) UpdateForwardTo(email string) error {
	url, err := c.hmeURL(1, "updateForwardTo")
	if err != nil {
		return err
	}

	body, err := c.doServiceRequest("POST", url, map[string]string{"forwardToEmail": email})
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
	if e != nil && e.ErrorMessage != "" {
		return fmt.Errorf("%s failed: %s", operation, e.ErrorMessage)
	}
	return fmt.Errorf("%s failed", operation)
}
