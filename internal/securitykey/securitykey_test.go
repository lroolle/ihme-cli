package securitykey

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/lroolle/ihme-cli/api"
	"github.com/telesma-app/ctap/authenticator"
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
		{&transport.CTAPError{StatusCode: transport.CTAP2_ERR_OPERATION_DENIED}, "not touched in time"},
		{authenticator.ErrPinUvAuthTokenRequired, "requires its PIN on every use"},
		{fmt.Errorf("open /dev/hidraw0: %w", fs.ErrPermission), "udev rules"},
	}
	for _, tc := range cases {
		if got := explain(tc.err).Error(); !strings.Contains(got, tc.want) {
			t.Errorf("explain(%v) = %q, want it to contain %q", tc.err, got, tc.want)
		}
	}
}
