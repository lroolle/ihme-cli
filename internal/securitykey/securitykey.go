// Package securitykey signs Apple's sign-in challenge with a FIDO2
// security key over USB: the step a browser performs when an Apple
// Account uses security keys instead of six-digit codes.
//
// Only user presence (a touch) is requested, as Apple's own web flow
// does. A key configured to demand its PIN on every use (alwaysUv)
// is refused with a clear error rather than prompted for a PIN.
// U2F-only (CTAP1) keys are not supported.
package securitykey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/telesma-app/ctap/authenticator"
	"github.com/telesma-app/ctap/backend"
	directhid "github.com/telesma-app/ctap/backend/hid"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/protocol"
	"github.com/telesma-app/ctap/transport"
)

// origin is the WebAuthn origin written into clientData and rpID the
// only relying party a key is asked to sign for. pyicloud signs with
// the same pair (timlaing/pyicloud#364, live with a YubiKey).
const (
	origin = "https://apple.com"
	rpID   = "apple.com"
)

const (
	// pollInterval is how often the plugged-in keys are looked at
	// again while waiting.
	pollInterval = 500 * time.Millisecond
	// deniedPolls is how many polls in a row a key may refuse to
	// open before that is the answer: right after plug-in, Linux
	// creates the device node before udev grants access to it.
	deniedPolls = 4
)

// Assert waits for a security key registered to the account, has the
// user touch it, and returns its signature over Apple's challenge.
// ctx bounds the whole wait. With several keys plugged in, only the
// registered one is asked for a touch.
func Assert(ctx context.Context, challenge api.SecurityKeyChallenge) (*api.SecurityKeyAssertion, error) {
	// The key signs for whatever rpId it is handed; a browser would
	// refuse one that does not match the origin, and so do we.
	if challenge.RpID != rpID {
		return nil, fmt.Errorf("refusing to sign for rpId %q; Apple's is %q", challenge.RpID, rpID)
	}
	clientData, allowList, err := prepare(challenge)
	if err != nil {
		return nil, err
	}

	device, allowList, err := waitForKey(ctx, directhid.Enumerate, clientData, allowList)
	if err != nil {
		return nil, err
	}
	defer device.Close()

	resp, err := firstAssertion(ctx, device, clientData, allowList, nil)
	if err != nil {
		return nil, explain(err)
	}
	return toAssertion(clientData, allowList, resp)
}

// toAssertion maps a key's answer to what Apple is sent.
func toAssertion(clientData []byte, allowList []credential.PublicKeyCredentialDescriptor,
	resp protocol.AuthenticatorGetAssertionResponse,
) (*api.SecurityKeyAssertion, error) {
	credentialID := resp.Credential.ID
	if len(credentialID) == 0 && len(allowList) == 1 {
		// CTAP lets the key omit the credential when the allow list
		// named exactly one.
		credentialID = allowList[0].ID
	}
	if len(credentialID) == 0 {
		return nil, errors.New("the security key did not say which credential it used")
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

// silent asks a key to check for a credential without a touch or a
// blink (CTAP "up": false).
var silent = map[protocol.Option]bool{protocol.OptionUserPresence: false}

// firstAssertion returns the first assertion the key yields. With an
// allow list the key returns one, so stopping there loses nothing.
func firstAssertion(ctx context.Context, device *authenticator.Device, clientData []byte,
	allowList []credential.PublicKeyCredentialDescriptor, options map[protocol.Option]bool,
) (protocol.AuthenticatorGetAssertionResponse, error) {
	for resp, err := range device.GetAssertion(ctx, nil, rpID, clientData, allowList, nil, options) {
		return resp, err
	}
	return protocol.AuthenticatorGetAssertionResponse{}, errors.New("the security key returned no signature")
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

// waitForKey returns a plugged-in key that holds one of the account's
// credentials, and the allow list narrowed to that credential, polling
// until one appears or ctx ends.
//
// It stops early in two cases, because waiting will not change them:
// every key that answered lacks the credential (the wrong key), or a
// key has refused access for deniedPolls polls in a row. Any other
// failure (a key still booting, an unrelated HID device) is "not yet",
// and the last one is reported if the wait runs out.
func waitForKey(ctx context.Context, enumerate backend.Enumerator, clientData []byte,
	allowList []credential.PublicKeyCredentialDescriptor,
) (*authenticator.Device, []credential.PublicKeyCredentialDescriptor, error) {
	var lastErr error
	denied := 0
	for {
		device, narrowed, err := findKey(ctx, enumerate, clientData, allowList)
		switch {
		case device != nil:
			return device, narrowed, nil
		case isNoCredentials(err):
			return nil, nil, explain(err)
		case errors.Is(err, fs.ErrPermission):
			if denied++; denied >= deniedPolls {
				return nil, nil, explain(err)
			}
		default:
			denied = 0
		}
		if err != nil && ctx.Err() == nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, nil, fmt.Errorf("no usable security key found: %w", explain(lastErr))
			}
			return nil, nil, errors.New("no security key found; plug one in and run ihme auth login again")
		case <-time.After(pollInterval):
		}
	}
}

// findKey picks among the plugged-in keys the way python-fido2 and
// browsers do: each is asked silently whether it holds one of the
// account's credentials, and the first that does is returned with the
// allow list narrowed to that credential; the rest are closed. One
// touch, even with both of a user's keys plugged in, and no CTAP 2.1
// selection command. A key that cannot answer the silent check is kept
// as a fallback and asked for a touch with the full allow list.
//
// Without a key it returns an open failure if there was one, else the
// no-credentials error if a key answered, else (nil, nil, nil) when
// nothing is plugged in.
func findKey(ctx context.Context, enumerate backend.Enumerator, clientData []byte,
	allowList []credential.PublicKeyCredentialDescriptor,
) (*authenticator.Device, []credential.PublicKeyCredentialDescriptor, error) {
	var (
		openErr, wrongKey error
		fallback          *authenticator.Device
	)
	for t, err := range enumerate(ctx) {
		if err != nil {
			openErr = errors.Join(openErr, err)
			continue
		}
		device, err := authenticator.New(ctx, t)
		if err != nil {
			openErr = errors.Join(openErr, err, t.Close())
			continue
		}
		resp, err := firstAssertion(ctx, device, clientData, allowList, silent)
		switch {
		case err == nil:
			if fallback != nil {
				fallback.Close()
			}
			if len(resp.Credential.ID) > 0 {
				return device, []credential.PublicKeyCredentialDescriptor{resp.Credential}, nil
			}
			return device, allowList, nil
		case isNoCredentials(err):
			wrongKey = err
			device.Close()
		case fallback == nil:
			fallback = device
		default:
			device.Close()
		}
	}
	if fallback != nil {
		return fallback, allowList, nil
	}
	if openErr != nil {
		return nil, nil, openErr
	}
	return nil, nil, wrongKey
}

func isNoCredentials(err error) bool {
	var ctapErr *transport.CTAPError
	return errors.As(err, &ctapErr) && ctapErr.StatusCode == transport.CTAP2_ERR_NO_CREDENTIALS
}

var errNotTouched = errors.New("the security key was not touched in time; run ihme auth login again")

// explain turns a CTAP or HID failure into what the user should do.
func explain(err error) error {
	var ctapErr *transport.CTAPError
	if errors.As(err, &ctapErr) {
		switch ctapErr.StatusCode {
		case transport.CTAP2_ERR_NO_CREDENTIALS:
			return errors.New("this security key is not registered to this Apple Account; try your other key")
		case transport.CTAP2_ERR_OPERATION_DENIED:
			return errors.New("the security key declined; run ihme auth login again")
		case transport.CTAP2_ERR_USER_ACTION_TIMEOUT,
			transport.CTAP2_ERR_ACTION_TIMEOUT,
			transport.CTAP2_ERR_KEEPALIVE_CANCEL:
			return errNotTouched
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errNotTouched
	case errors.Is(err, authenticator.ErrPinUvAuthTokenRequired),
		errors.Is(err, authenticator.ErrBuiltInUVRequired):
		return errors.New("this security key requires its PIN on every use (alwaysUv), which ihme does not support yet")
	case errors.Is(err, authenticator.ErrUvNotConfigured):
		return errors.New("this security key requires fingerprint verification but has none enrolled; enroll one with the key's own app")
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("no permission to open the security key (%s): %w", permissionHint(), err)
	}
	return err
}

func permissionHint() string {
	switch runtime.GOOS {
	case "windows":
		// Windows 10 1903+ only lets elevated processes talk to
		// FIDO keys directly; everything else must go through
		// webauthn.dll, which ihme does not use.
		return "on Windows, run ihme from an Administrator terminal"
	case "linux":
		return "on Linux, install your key's udev rules"
	default:
		return "check that no other app holds the key"
	}
}
