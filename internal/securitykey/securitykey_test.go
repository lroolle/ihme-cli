package securitykey

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/lroolle/ihme-cli/api"
	"github.com/telesma-app/ctap/authenticator"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/protocol"
	"github.com/telesma-app/ctap/transport"
)

// The key signs SHA-256(clientData) and Apple re-derives it from the
// bytes we send, so clientData must be byte-exact WebAuthn: the
// challenge re-encoded as unpadded base64url, the apple.com origin.
func TestPrepareClientData(t *testing.T) {
	clientData, allowList, err := prepare(api.SecurityKeyChallenge{
		Challenge:  "-_8=", // base64url, padded: bytes fb ff
		KeyHandles: []string{"a2V5MQ", "a2V5Mg=="},
		RpID:       "apple.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"webauthn.get","challenge":"-_8","origin":"https://apple.com","crossOrigin":false}`
	if string(clientData) != want {
		t.Errorf("clientData = %s\nwant        %s", clientData, want)
	}
	if len(allowList) != 2 || string(allowList[0].ID) != "key1" || string(allowList[1].ID) != "key2" {
		t.Errorf("allow list = %+v", allowList)
	}
	if allowList[0].Type != "public-key" {
		t.Errorf("credential type = %q", allowList[0].Type)
	}
}

func TestDecodeBase64AcceptsBothAlphabets(t *testing.T) {
	want := []byte{0xfb, 0xff, 0x01}
	for _, in := range []string{"-_8B", "+/8B", "+/8B===="} {
		got, err := decodeBase64(in)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("decodeBase64(%q) = %x, %v; want %x", in, got, err, want)
		}
	}
	if _, err := decodeBase64("not base64!"); err == nil {
		t.Error("garbage must not decode")
	}
}

func TestPrepareRejectsBadChallenge(t *testing.T) {
	if _, _, err := prepare(api.SecurityKeyChallenge{Challenge: "!!", RpID: "apple.com"}); err == nil {
		t.Error("an undecodable challenge must fail before touching the key")
	}
}

func TestExplain(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&transport.CTAPError{StatusCode: transport.CTAP2_ERR_NO_CREDENTIALS}, "not registered to this Apple Account"},
		{fmt.Errorf("wrapped: %w", &transport.CTAPError{StatusCode: transport.CTAP2_ERR_USER_ACTION_TIMEOUT}), "not touched in time"},
		{&transport.CTAPError{StatusCode: transport.CTAP2_ERR_OPERATION_DENIED}, "declined"},
		{fmt.Errorf("select: %w", context.DeadlineExceeded), "not touched in time"},
		{authenticator.ErrPinUvAuthTokenRequired, "requires its PIN on every use"},
		// The hint itself depends on runtime.GOOS.
		{fmt.Errorf("open /dev/hidraw0: %w", fs.ErrPermission), "no permission to open the security key (" + permissionHint() + ")"},
	}
	for _, tc := range cases {
		if got := explain(tc.err).Error(); !strings.Contains(got, tc.want) {
			t.Errorf("explain(%v) = %q, want it to contain %q", tc.err, got, tc.want)
		}
	}
}

// A key must never sign for a relying party other than Apple's, even
// if the server (or something in between) names one.
func TestAssertRefusesForeignRpID(t *testing.T) {
	_, err := Assert(context.Background(), api.SecurityKeyChallenge{Challenge: "Y2g", RpID: "evil.example"})
	if err == nil || !strings.Contains(err.Error(), "refusing to sign") {
		t.Errorf("foreign rpId: got %v", err)
	}
}

// failing is a device list whose every entry fails to open with err;
// nil err means nothing is plugged in.
func failing(err error) func(context.Context) iter.Seq2[transport.Device, error] {
	return func(context.Context) iter.Seq2[transport.Device, error] {
		return func(yield func(transport.Device, error) bool) {
			if err != nil {
				yield(nil, err)
			}
		}
	}
}

func TestWaitForKey(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		want  string
		early bool // gives up before the deadline
	}{
		{"nothing plugged in", nil, "no security key found", false},
		{"access refused for good", fmt.Errorf("open /dev/hidraw3: %w", fs.ErrPermission), "no permission to open the security key", true},
		{"key still booting", errors.New("ctaphid init: timeout"), "no usable security key found: ctaphid init: timeout", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			deadline := 4 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			start := time.Now()
			_, _, err := waitForKey(ctx, failing(tc.err), nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want it to contain %q", err, tc.want)
			}
			if early := time.Since(start) < deadline-time.Second; early != tc.early {
				t.Errorf("gave up after %v; early = %v, want %v", time.Since(start), early, tc.early)
			}
		})
	}
}

func TestToAssertion(t *testing.T) {
	one := []credential.PublicKeyCredentialDescriptor{{Type: credential.PublicKeyCredentialTypePublicKey, ID: []byte("key1")}}
	two := append(one, credential.PublicKeyCredentialDescriptor{Type: credential.PublicKeyCredentialTypePublicKey, ID: []byte("key2")})

	// A key may omit the credential when the allow list named one.
	a, err := toAssertion([]byte("cd"), one, protocol.AuthenticatorGetAssertionResponse{AuthDataRaw: []byte("ad"), Signature: []byte("sig")})
	if err != nil || string(a.CredentialID) != "key1" || a.UserHandle != nil || string(a.AuthenticatorData) != "ad" {
		t.Errorf("single allow list: %+v, %v", a, err)
	}

	resp := protocol.AuthenticatorGetAssertionResponse{
		Credential: two[1],
		User:       &credential.PublicKeyCredentialUserEntity{ID: []byte("user")},
	}
	if a, err := toAssertion(nil, two, resp); err != nil || string(a.CredentialID) != "key2" || string(a.UserHandle) != "user" {
		t.Errorf("named credential: %+v, %v", a, err)
	}

	// Never send Apple an empty credential ID.
	if _, err := toAssertion(nil, two, protocol.AuthenticatorGetAssertionResponse{}); err == nil {
		t.Error("an unnamed credential among two must fail locally")
	}
}
