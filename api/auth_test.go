package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func TestDerivePasswordS2K(t *testing.T) {
	salt := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	password := "testpassword"
	iterations := 1000

	key := derivePassword(password, salt, iterations, "s2k")
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}

	// Verify manually: s2k uses raw SHA256 bytes
	passHash := sha256.Sum256([]byte(password))
	expected := pbkdf2.Key(passHash[:], salt, iterations, 32, sha256.New)
	if hex.EncodeToString(key) != hex.EncodeToString(expected) {
		t.Errorf("s2k key mismatch")
	}
}

func TestDerivePasswordS2KFO(t *testing.T) {
	salt := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	password := "testpassword"
	iterations := 1000

	key := derivePassword(password, salt, iterations, "s2k_fo")
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}

	// Verify manually: s2k_fo uses uppercase hex of SHA256
	passHash := sha256.Sum256([]byte(password))
	hexInput := []byte(fmt.Sprintf("%X", passHash))
	expected := pbkdf2.Key(hexInput, salt, iterations, 32, sha256.New)
	if hex.EncodeToString(key) != hex.EncodeToString(expected) {
		t.Errorf("s2k_fo key mismatch")
	}
}

func TestDerivePasswordS2KAndS2KFODiffer(t *testing.T) {
	salt := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	password := "password123"

	s2k := derivePassword(password, salt, 1000, "s2k")
	s2kfo := derivePassword(password, salt, 1000, "s2k_fo")

	if hex.EncodeToString(s2k) == hex.EncodeToString(s2kfo) {
		t.Error("s2k and s2k_fo should produce different keys")
	}
}

func TestDerivePasswordS2KFOUppercase(t *testing.T) {
	salt := []byte{0x01, 0x02}
	password := "test"

	key := derivePassword(password, salt, 1, "s2k_fo")

	// Verify the hex input is uppercase by computing manually
	passHash := sha256.Sum256([]byte(password))
	upperHex := fmt.Sprintf("%X", passHash)
	lowerHex := fmt.Sprintf("%x", passHash)

	if upperHex == lowerHex {
		t.Skip("hash has no alpha chars, can't distinguish case")
	}

	upperKey := pbkdf2.Key([]byte(upperHex), salt, 1, 32, sha256.New)
	lowerKey := pbkdf2.Key([]byte(lowerHex), salt, 1, 32, sha256.New)

	if hex.EncodeToString(key) == hex.EncodeToString(lowerKey) {
		t.Error("s2k_fo should use uppercase hex, not lowercase")
	}
	if hex.EncodeToString(key) != hex.EncodeToString(upperKey) {
		t.Error("s2k_fo key doesn't match uppercase hex derivation")
	}
}

func TestDerivePasswordDeterministic(t *testing.T) {
	salt := []byte{0x01, 0x02, 0x03, 0x04}
	k1 := derivePassword("pass", salt, 100, "s2k")
	k2 := derivePassword("pass", salt, 100, "s2k")
	if hex.EncodeToString(k1) != hex.EncodeToString(k2) {
		t.Error("derivePassword should be deterministic")
	}
}

func TestDerivePasswordDefaultProtocol(t *testing.T) {
	salt := []byte{0x01}

	// Unknown protocol should fall through to s2k (raw bytes)
	keyUnknown := derivePassword("pass", salt, 1, "unknown_protocol")
	keyS2K := derivePassword("pass", salt, 1, "s2k")

	if hex.EncodeToString(keyUnknown) != hex.EncodeToString(keyS2K) {
		t.Error("unknown protocol should default to s2k behavior")
	}
}

// Apple started answering 409 to a VALID security code around
// mid-2026 (rclone#9488). The tells that it was accepted are a fresh
// session token, or the body's own securityCode.valid=true when the
// 409 carries no token (rclone#9730); a bare 409 is still a failure.
func TestCodeAcceptedDespiteConflict(t *testing.T) {
	withToken := &http.Response{StatusCode: 409, Header: http.Header{"X-Apple-Session-Token": []string{"fresh"}}}
	if !codeAcceptedDespiteConflict(withToken, nil) {
		t.Error("409 carrying a session token means the code was accepted")
	}

	bare := &http.Response{StatusCode: 409, Header: http.Header{}}
	if codeAcceptedDespiteConflict(bare, nil) {
		t.Error("409 without a session token or a valid verdict is not an acceptance")
	}

	validBody := []byte(`{"securityCode":{"code":"******","tooManyCodesSent":false,"valid":true},"authenticationType":"hsa2"}`)
	if !codeAcceptedDespiteConflict(bare, validBody) {
		t.Error("409 whose body says securityCode.valid=true means the code was accepted (rclone#9730)")
	}
	if codeAcceptedDespiteConflict(bare, []byte(`{"securityCode":{"code":"******","valid":false}}`)) {
		t.Error("valid=false must not read as acceptance")
	}
	if codeAcceptedDespiteConflict(bare, []byte(`not json`)) {
		t.Error("garbage body must not read as acceptance")
	}

	ok := &http.Response{StatusCode: 200, Header: http.Header{"X-Apple-Session-Token": []string{"fresh"}}}
	if codeAcceptedDespiteConflict(ok, validBody) {
		t.Error("only 409 takes this path")
	}

	if codeAcceptedDespiteConflict(nil, validBody) {
		t.Error("nil response must not read as acceptance")
	}
}

func TestSigninError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
		locked bool
	}{
		{"locked, numeric code", 403, `{"serviceErrors":[{"code":-20209,"message":"This Apple Account has been locked for security reasons."}]}`, "locked", true},
		{"locked, string code", 400, `{"serviceErrors":[{"code":"-20209","message":"locked"}]}`, "locked", true},
		{"wrong password", 401, `{"serviceErrors":[{"code":"-20101","message":"Your Apple Account or password was incorrect."}]}`, "incorrect Apple ID or password (Apple: Your Apple Account or password was incorrect.)", false},
		{"bare 403", 403, ``, "incorrect Apple ID or password", false},
		{"init refusal is not about the password", 401, ``, "Apple refused sign-in for this Apple ID", false},
		{"other, with message", 503, `{"serviceErrors":[{"code":"-1","message":"Service unavailable"}]}`, "signin complete returned 503: Service unavailable", false},
		{"other, opaque", 500, `oops`, "signin complete returned 500: oops", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := "signin complete"
			if strings.HasPrefix(tc.name, "init") {
				step = "signin init"
			}
			err := signinError(step, tc.status, []byte(tc.body))
			if errors.Is(err, ErrAccountLocked) != tc.locked {
				t.Fatalf("locked = %v, want %v (%v)", !tc.locked, tc.locked, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

// toServer sends every request to srv, whatever host it names, so
// the real AuthEndpoint URLs can be exercised against a fake Apple.
type toServer struct{ srv *httptest.Server }

func (t toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.srv.URL)
	r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(r)
}

// A security-key account gets no code anywhere: login must answer
// fsaChallenge with the key's signature at /verify/security/key and
// never prompt for a code.
func TestHandle2FASecurityKey(t *testing.T) {
	var submitted map[string]any
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/appleauth/auth":
			fmt.Fprint(w, `{"fsaChallenge":{"challenge":"Y2hhbGxlbmdl","keyHandles":["a2V5MQ","a2V5Mg"],"rpId":"apple.com"},"keyNames":["Blue","Black"],"trustedPhoneNumbers":[{"id":1,"pushMode":"sms"}]}`)
		case r.Method == "POST" && r.URL.Path == "/appleauth/auth/verify/security/key":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Errorf("decoding submitted body: %v", err)
			}
			w.WriteHeader(status)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	newClient := func() *Client {
		c, _ := NewClient()
		c.http.Transport = toServer{srv}
		return c
	}
	otp := func() (string, error) {
		t.Error("a security-key account must not be asked for a code")
		return "", errors.New("unexpected")
	}
	var got SecurityKeyChallenge
	key := func(ch SecurityKeyChallenge) (*SecurityKeyAssertion, error) {
		got = ch
		return &SecurityKeyAssertion{
			ClientData:        []byte("cd"),
			AuthenticatorData: []byte("ad"),
			Signature:         []byte("sig"),
			CredentialID:      []byte("key1"),
		}, nil
	}

	if err := newClient().handle2FA(nil, otp, key); err != nil {
		t.Fatalf("handle2FA: %v", err)
	}
	if got.Challenge != "Y2hhbGxlbmdl" || got.RpID != "apple.com" || len(got.KeyHandles) != 2 {
		t.Errorf("challenge passed to the key = %+v", got)
	}
	if strings.Join(got.KeyNames, ",") != "Blue,Black" {
		t.Errorf("key names = %v, want Blue,Black", got.KeyNames)
	}
	want := map[string]any{
		"challenge":         "Y2hhbGxlbmdl",
		"clientData":        "Y2Q=",
		"authenticatorData": "YWQ=",
		"signatureData":     "c2ln",
		"credentialID":      "a2V5MQ==",
		"userHandle":        nil,
		"rpId":              "apple.com",
	}
	for k, v := range want {
		if got, ok := submitted[k]; !ok || got != v {
			t.Errorf("submitted %s = %v (present %v), want %v", k, got, ok, v)
		}
	}

	status = 400
	if err := newClient().handle2FA(nil, otp, key); err == nil || !strings.Contains(err.Error(), "security key not accepted by Apple") {
		t.Errorf("400 from Apple: got %v", err)
	}

	if err := newClient().handle2FA(nil, otp, nil); err == nil || !strings.Contains(err.Error(), "security key") {
		t.Errorf("no key callback: got %v", err)
	}

	keyErr := func(SecurityKeyChallenge) (*SecurityKeyAssertion, error) { return nil, errors.New("not touched") }
	if err := newClient().handle2FA(nil, otp, keyErr); err == nil || !strings.Contains(err.Error(), "not touched") {
		t.Errorf("key failure must surface: got %v", err)
	}
}

// keyNames alone still means a security-key account: never fall
// through to a code prompt that cannot be answered.
func TestHandle2FAKeyNamesWithoutChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"keyNames":["Blue"],"trustedPhoneNumbers":[{"id":1,"pushMode":"sms"}]}`)
	}))
	defer srv.Close()
	c, _ := NewClient()
	c.http.Transport = toServer{srv}
	otp := func() (string, error) {
		t.Error("must not prompt for a code")
		return "", errors.New("unexpected")
	}
	if err := c.handle2FA(nil, otp, nil); err == nil || !strings.Contains(err.Error(), "no security key challenge") {
		t.Errorf("got %v", err)
	}
}

// fakeApple serves the 2FA options GET with options and answers the
// security-key POST with status and body.
func fakeApple(t *testing.T, options string, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/appleauth/auth":
			fmt.Fprint(w, options)
		case r.Method == "POST" && r.URL.Path == "/appleauth/auth/verify/security/key":
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	c, _ := NewClient()
	c.http.Transport = toServer{srv}
	return c
}

// Every shape Apple (or a man in the middle of our reading of it) has
// been seen to use for the challenge must reach the key, and the
// answer statuses must be read the way Apple's own page reads them.
func TestHandle2FASecurityKeyShapes(t *testing.T) {
	const challenge = `{"challenge":"Y2g","keyHandles":["a2V5MQ"],"rpId":"apple.com"}`
	noCode := func() (string, error) {
		t.Error("must not prompt for a code")
		return "", errors.New("unexpected")
	}
	cases := []struct {
		name       string
		options    string
		signin     string
		status     int
		body       string
		wantErr    string // "" means success
		wantHandle string
	}{
		{"top level", `{"fsaChallenge":` + challenge + `}`, "", 200, "", "", "a2V5MQ"},
		{"nested under fsaVerification", `{"fsaVerification":{"fsaChallenge":` + challenge + `}}`, "", 200, "", "", "a2V5MQ"},
		{"only in the signin 409", `{"trustedPhoneNumbers":[{"id":1,"pushMode":"sms"}]}`, `{"fsaChallenge":` + challenge + `}`, 200, "", "", "a2V5MQ"},
		{"allowedCredentials instead of keyHandles", `{"fsaChallenge":{"challenge":"Y2g","allowedCredentials":"a2V5Mg, a2V5Mw","rpId":"apple.com"}}`, "", 200, "", "", "a2V5Mg"},
		{"no key handles at all", `{"fsaChallenge":{"challenge":"Y2g","rpId":"apple.com"}}`, "", 200, "", "missing challenge, rpId, or key handles", ""},
		{"PRF required", `{"fsaChallenge":` + challenge + `,"requirePrf":true}`, "", 200, "", "PRF extension", ""},
		{"409 without serviceErrors is accepted", `{"fsaChallenge":` + challenge + `}`, "", 409, `{}`, "", "a2V5MQ"},
		{"409 with serviceErrors is refused", `{"fsaChallenge":` + challenge + `}`, "", 409, `{"serviceErrors":[{"code":"-27962"}]}`, "not accepted by Apple (409)", "a2V5MQ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got SecurityKeyChallenge
			key := func(ch SecurityKeyChallenge) (*SecurityKeyAssertion, error) {
				got = ch
				return &SecurityKeyAssertion{CredentialID: []byte("key1")}, nil
			}
			err := fakeApple(t, tc.options, tc.status, tc.body).handle2FA([]byte(tc.signin), noCode, key)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("want success, got %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
			if tc.wantHandle != "" && (len(got.KeyHandles) == 0 || got.KeyHandles[0] != tc.wantHandle) {
				t.Errorf("key handles = %v, want first %q", got.KeyHandles, tc.wantHandle)
			}
		})
	}
}

// The regression guard for everyone else: an account that uses codes
// still gets the push, the prompt, and the code POST, and the key
// callback is never called.
func TestHandle2FACodeAccountUnchanged(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == "GET" {
			fmt.Fprint(w, `{"trustedDevices":[{"name":"iPhone","id":"1"}]}`)
		}
	}))
	defer srv.Close()
	c, _ := NewClient()
	c.http.Transport = toServer{srv}
	prompted := false
	otp := func() (string, error) { prompted = true; return "123456", nil }
	key := func(SecurityKeyChallenge) (*SecurityKeyAssertion, error) {
		t.Error("a code account must not reach the security key")
		return nil, errors.New("unexpected")
	}
	if err := c.handle2FA([]byte(`{}`), otp, key); err != nil {
		t.Fatal(err)
	}
	want := "GET /appleauth/auth,PUT /appleauth/auth/verify/trusteddevice/securitycode,POST /appleauth/auth/verify/trusteddevice/securitycode"
	if got := strings.Join(calls, ","); got != want || !prompted {
		t.Errorf("calls = %s (prompted %v)\nwant    %s", got, prompted, want)
	}
}

// A login Apple still holds 2FA against must not report success:
// Robert's security-key login said "Logged in successfully" and the
// next command got 401 "Invalid global session". Only an explicit
// hsaChallengeRequired=true fails, so accounts whose response lacks
// the field behave as before.
func TestCheckAccount(t *testing.T) {
	cases := []struct {
		name, body string
		wantErr    bool
		wantNote   string // substring; "" means no note
	}{
		{"2FA still owed", `{"dsInfo":{"dsid":"1","hsaVersion":2},"hsaChallengeRequired":true,"hsaTrustedBrowser":false}`, true, ""},
		{"2FA done, everything on", `{"dsInfo":{"dsid":"1","hsaVersion":2,"isWebAccessAllowed":true,"isHideMyEmailFeatureAvailable":true},"hsaChallengeRequired":false,"hsaTrustedBrowser":true,"webservices":{"premiummailsettings":{"url":"u","status":"active"}}}`, false, ""},
		{"fields absent", `{"dsInfo":{"dsid":"1"},"webservices":{"premiummailsettings":{"url":"u"}}}`, false, ""},
		{"pcsRequired HME is a note, not a failure", `{"dsInfo":{"dsid":"1"},"hsaChallengeRequired":false,"webservices":{"premiummailsettings":{"url":"u","status":"active","pcsRequired":true}}}`, false, "pcsRequired"},
		{"web access off is a note, not a failure", `{"dsInfo":{"dsid":"1","isWebAccessAllowed":false},"hsaChallengeRequired":false,"webservices":{"premiummailsettings":{"url":"u"}}}`, false, "Access iCloud Data on the Web"},
		{"data access off outranks a missing service", `{"dsInfo":{"dsid":"1","isWebAccessAllowed":false},"hsaChallengeRequired":false,"webservices":{}}`, false, "Allow Data Access"},
		{"no Hide My Email is a note, not a failure", `{"dsInfo":{"dsid":"1","isHideMyEmailFeatureAvailable":false},"hsaChallengeRequired":false,"webservices":{"premiummailsettings":{"url":"u"}}}`, false, "iCloud+"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c, _ := NewClient()
			c.setupBase = srv.URL
			if err := c.accountLogin(); err != nil {
				t.Fatal(err)
			}
			err := c.checkAccount(c.lastLogin)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkAccount = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "hsaChallengeRequired") {
				t.Errorf("error should name Apple's flag: %v", err)
			}
			if tc.wantErr {
				return // login fails before any note
			}
			note := accountNote(c.lastLogin)
			if (tc.wantNote == "") != (note == "") || !strings.Contains(note, tc.wantNote) {
				t.Errorf("note = %q, want one containing %q", note, tc.wantNote)
			}
		})
	}
}

// An account Apple lists without the Hide My Email service at all
// gets the iCloud+ note: nothing else ihme does can work there.
func TestAccountNoteNoService(t *testing.T) {
	if note := accountNote(&AccountLoginResponse{}); !strings.Contains(note, "iCloud+") {
		t.Errorf("note = %q", note)
	}
}
