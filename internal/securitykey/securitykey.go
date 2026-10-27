// Package securitykey signs Apple's sign-in challenge with a FIDO2
// security key over USB: the step a browser performs when an Apple
// Account uses security keys instead of six-digit codes.
//
// Only user presence (a touch) is requested, as Apple's own web flow
// does. A key configured to demand its PIN on every use (alwaysUv)
// is refused with a clear error rather than prompted for a PIN.
package securitykey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"strings"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/telesma-app/ctap/authenticator"
	directhid "github.com/telesma-app/ctap/backend/hid"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/transport"
)

// origin is the WebAuthn origin written into clientData. Apple's
// rpId is apple.com; pyicloud signs with this same origin and
// icloudpd users sign in with it.
const origin = "https://apple.com"

// pollInterval is how often Assert looks for a key that is not yet
// plugged in.
const pollInterval = 500 * time.Millisecond

// Assert waits for a security key to appear, has the user touch it,
// and returns its signature over Apple's challenge. ctx bounds the
// whole wait. With several keys plugged in, the first touch picks
// the key and a second touch signs.
func Assert(ctx context.Context, challenge api.SecurityKeyChallenge) (*api.SecurityKeyAssertion, error) {
	clientData, allowList, err := prepare(challenge)
	if err != nil {
		return nil, err
	}

	device, err := waitForKey(ctx)
	if err != nil {
		return nil, err
	}
	defer device.Close()

	for resp, err := range device.GetAssertion(ctx, nil, challenge.RpID, clientData, allowList, nil, nil) {
		if err != nil {
			return nil, explain(err)
		}
		credentialID := resp.Credential.ID
		if len(credentialID) == 0 && len(allowList) == 1 {
			// CTAP lets the key omit the credential when the
			// allow list named exactly one.
			credentialID = allowList[0].ID
		}
		assertion := &api.SecurityKeyAssertion{
			ClientData:        clientData,
			AuthenticatorData: resp.AuthDataRaw,
			Signature:         resp.Signature,
			CredentialID:      credentialID,
		}
		if resp.User != nil {
			assertion.UserHandle = resp.User.ID
		}
		return assertion, nil
	}
	return nil, errors.New("the security key returned no signature")
}

// collectedClientData is WebAuthn's CollectedClientData. Field order
// matches what python-fido2 (pyicloud) sends.
type collectedClientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

// prepare builds the clientData JSON and the allow list from Apple's
// challenge. The key signs SHA-256(clientData), so these bytes are
// exactly what Apple receives back.
func prepare(challenge api.SecurityKeyChallenge) ([]byte, []credential.PublicKeyCredentialDescriptor, error) {
	raw, err := decodeBase64(challenge.Challenge)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding Apple's challenge: %w", err)
	}
	clientData, err := json.Marshal(collectedClientData{
		Type:      "webauthn.get",
		Challenge: base64.RawURLEncoding.EncodeToString(raw),
		Origin:    origin,
	})
	if err != nil {
		return nil, nil, err
	}

	allowList := make([]credential.PublicKeyCredentialDescriptor, 0, len(challenge.KeyHandles))
	for _, handle := range challenge.KeyHandles {
		id, err := decodeBase64(handle)
		if err != nil {
			return nil, nil, fmt.Errorf("decoding key handle: %w", err)
		}
		allowList = append(allowList, credential.PublicKeyCredentialDescriptor{
			Type: credential.PublicKeyCredentialTypePublicKey,
			ID:   id,
		})
	}
	return clientData, allowList, nil
}

// decodeBase64 accepts base64url or standard base64, padded or not:
// Apple documents neither, and pyicloud tolerates both.
func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	s = strings.NewReplacer("+", "-", "/", "_").Replace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

// waitForKey returns the first security key found, polling until one
// is plugged in or ctx ends.
func waitForKey(ctx context.Context) (*authenticator.Device, error) {
	for {
		seen := 0
		enumerate := func(ctx context.Context) iter.Seq2[transport.Device, error] {
			return func(yield func(transport.Device, error) bool) {
				for d, err := range directhid.Enumerate(ctx) {
					seen++
					if !yield(d, err) {
						return
					}
				}
			}
		}
		device, err := authenticator.Select(ctx, enumerate)
		if err == nil {
			return device, nil
		}
		if seen > 0 {
			// A key is there but could not be opened or picked.
			return nil, explain(err)
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("no security key found; plug one in and run ihme auth login again")
		case <-time.After(pollInterval):
		}
	}
}

// explain turns a CTAP or HID failure into what the user should do.
func explain(err error) error {
	var ctapErr *transport.CTAPError
	if errors.As(err, &ctapErr) {
		switch ctapErr.StatusCode {
		case transport.CTAP2_ERR_NO_CREDENTIALS:
			return errors.New("this security key is not registered to this Apple Account; try your other key")
		case transport.CTAP2_ERR_OPERATION_DENIED,
			transport.CTAP2_ERR_USER_ACTION_TIMEOUT,
			transport.CTAP2_ERR_ACTION_TIMEOUT,
			transport.CTAP2_ERR_KEEPALIVE_CANCEL:
			return errors.New("the security key was not touched in time; run ihme auth login again")
		}
	}
	if errors.Is(err, authenticator.ErrPinUvAuthTokenRequired) || errors.Is(err, authenticator.ErrBuiltInUVRequired) {
		return errors.New("this security key requires its PIN on every use (alwaysUv), which ihme does not support yet")
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("no permission to open the security key (on Linux, install your key's udev rules): %w", err)
	}
	return err
}
