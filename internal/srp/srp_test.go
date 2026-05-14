package srp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func TestApple2048Params(t *testing.T) {
	p := Apple2048Params()

	if p.NLengthBits != 2048 {
		t.Errorf("NLengthBits = %d, want 2048", p.NLengthBits)
	}
	if p.G.Int64() != 2 {
		t.Errorf("G = %d, want 2", p.G.Int64())
	}
	if !p.NoUserNameInX {
		t.Error("NoUserNameInX should be true for Apple params")
	}
	if p.N.BitLen() < 2047 || p.N.BitLen() > 2048 {
		t.Errorf("N bit length = %d, want 2048", p.N.BitLen())
	}
}

func TestPadToN(t *testing.T) {
	p := Apple2048Params()
	small := big.NewInt(42)
	padded := p.padToN(small)
	if len(padded) != 256 {
		t.Errorf("padToN length = %d, want 256", len(padded))
	}
	if padded[255] != 42 {
		t.Errorf("last byte = %d, want 42", padded[255])
	}
	for i := 0; i < 255; i++ {
		if padded[i] != 0 {
			t.Errorf("padding byte %d = %d, want 0", i, padded[i])
		}
	}
}

func TestMultiplier(t *testing.T) {
	p := Apple2048Params()
	k := p.multiplier()
	if k.Sign() == 0 {
		t.Error("multiplier k is zero")
	}

	k2 := p.multiplier()
	if k.Cmp(k2) != 0 {
		t.Error("multiplier should be deterministic")
	}
}

func TestComputeXNoUsername(t *testing.T) {
	p := Apple2048Params()
	salt := []byte("test-salt-value!")
	password := []byte("derived-key-bytes")

	x := p.computeX(salt, []byte("user@example.com"), password)
	xDiff := p.computeX(salt, []byte("different@user.com"), password)

	// With NoUserNameInX=true, username should not affect x
	if x.Cmp(xDiff) != 0 {
		t.Error("NoUserNameInX=true but username affected x computation")
	}

	p2 := &Params{
		G: p.G, N: p.N, Hash: p.Hash, NLengthBits: p.NLengthBits,
		NoUserNameInX: false,
	}
	xWithUser := p2.computeX(salt, []byte("user@example.com"), password)
	if x.Cmp(xWithUser) == 0 {
		t.Error("NoUserNameInX=false should produce different x")
	}
}

func TestDerivePasswordS2K(t *testing.T) {
	password := "testpassword123"
	salt := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	iterations := 1000

	passHash := sha256.Sum256([]byte(password))

	// s2k: raw bytes
	s2kKey := pbkdf2.Key(passHash[:], salt, iterations, 32, sha256.New)
	if len(s2kKey) != 32 {
		t.Errorf("s2k key length = %d, want 32", len(s2kKey))
	}

	// s2k_fo: hex-encoded
	hexInput := []byte(fmt.Sprintf("%x", passHash))
	s2kfoKey := pbkdf2.Key(hexInput, salt, iterations, 32, sha256.New)
	if len(s2kfoKey) != 32 {
		t.Errorf("s2k_fo key length = %d, want 32", len(s2kfoKey))
	}

	// They must differ (different input to PBKDF2)
	if hex.EncodeToString(s2kKey) == hex.EncodeToString(s2kfoKey) {
		t.Error("s2k and s2k_fo should produce different keys")
	}
}

func TestClientCreation(t *testing.T) {
	p := Apple2048Params()
	client, err := NewClient(p)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	pubKey := client.PublicKey()
	if len(pubKey) != 256 {
		t.Errorf("public key length = %d, want 256", len(pubKey))
	}

	// Public key must not be zero
	A := new(big.Int).SetBytes(pubKey)
	if A.Sign() == 0 {
		t.Error("public key A is zero")
	}

	// A must be in range [1, N-1]
	if A.Cmp(p.N) >= 0 {
		t.Error("A >= N")
	}
}

func TestClientDeterminismDiffers(t *testing.T) {
	p := Apple2048Params()
	c1, _ := NewClient(p)
	c2, _ := NewClient(p)

	if hex.EncodeToString(c1.PublicKey()) == hex.EncodeToString(c2.PublicKey()) {
		t.Error("two clients should produce different public keys (random secret)")
	}
}

func TestProcessChallengeRejectsInvalidB(t *testing.T) {
	p := Apple2048Params()
	client, _ := NewClient(p)

	err := client.ProcessChallenge(
		[]byte("user"), []byte("pass"), []byte("salt"),
		[]byte{0},
	)
	if err == nil {
		t.Error("ProcessChallenge should reject B=0")
	}

	bigB := make([]byte, 257)
	bigB[0] = 0xff
	for i := 1; i < 257; i++ {
		bigB[i] = 0xff
	}
	err = client.ProcessChallenge(
		[]byte("user"), []byte("pass"), []byte("salt"),
		bigB,
	)
	if err == nil {
		t.Error("ProcessChallenge should reject B >= N")
	}
}

func TestM1M2Determinism(t *testing.T) {
	p := Apple2048Params()

	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i)
	}

	username := []byte("test@example.com")
	A := p.padToN(big.NewInt(12345))
	B := p.padToN(big.NewInt(67890))
	K := make([]byte, 32)
	for i := range K {
		K[i] = byte(i + 100)
	}

	m1a := p.computeM1(username, salt, A, B, K)
	m1b := p.computeM1(username, salt, A, B, K)
	if hex.EncodeToString(m1a) != hex.EncodeToString(m1b) {
		t.Error("M1 not deterministic")
	}

	m2a := p.computeM2(A, m1a, K)
	m2b := p.computeM2(A, m1b, K)
	if hex.EncodeToString(m2a) != hex.EncodeToString(m2b) {
		t.Error("M2 not deterministic")
	}

	if len(m1a) != 32 {
		t.Errorf("M1 length = %d, want 32 (SHA-256)", len(m1a))
	}
	if len(m2a) != 32 {
		t.Errorf("M2 length = %d, want 32 (SHA-256)", len(m2a))
	}
}

func TestFullSRPRoundTrip(t *testing.T) {
	p := Apple2048Params()

	username := []byte("test@example.com")
	password := []byte("derived-key-32-bytes-of-data!!")
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i + 1)
	}

	// Simulate server: compute verifier v = g^x mod N
	x := p.computeX(salt, username, password)
	v := new(big.Int).Exp(p.G, x, p.N)

	// Server generates B = k*v + g^b mod N
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i + 100)
	}
	bInt := new(big.Int).SetBytes(b)
	k := p.multiplier()
	gB := new(big.Int).Exp(p.G, bInt, p.N)
	kv := new(big.Int).Mul(k, v)
	B := new(big.Int).Add(kv, gB)
	B.Mod(B, p.N)

	// Client processes challenge
	client, err := NewClient(p)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.ProcessChallenge(username, password, salt, p.padToN(B))
	if err != nil {
		t.Fatalf("ProcessChallenge: %v", err)
	}

	// Verify outputs are non-nil and correct length
	if len(client.Proof()) != 32 {
		t.Errorf("M1 length = %d, want 32", len(client.Proof()))
	}
	if len(client.ServerProof()) != 32 {
		t.Errorf("M2 length = %d, want 32", len(client.ServerProof()))
	}
	if len(client.SessionKey()) != 32 {
		t.Errorf("K length = %d, want 32", len(client.SessionKey()))
	}

	// Compute server-side S to verify session key agreement
	A := new(big.Int).SetBytes(client.PublicKey())
	u := p.computeU(A, B)
	// Server S = (A * v^u) ^ b mod N
	vu := new(big.Int).Exp(v, u, p.N)
	Avu := new(big.Int).Mul(A, vu)
	Avu.Mod(Avu, p.N)
	serverS := new(big.Int).Exp(Avu, bInt, p.N)
	serverS.Mod(serverS, p.N)
	serverK := p.computeK(p.padToN(serverS))

	if hex.EncodeToString(client.SessionKey()) != hex.EncodeToString(serverK) {
		t.Error("client and server session keys do not match")
	}

	// Compute server M2 and verify
	ABytes := p.padToN(A)
	serverM1 := p.computeM1(username, salt, ABytes, p.padToN(B), serverK)
	if hex.EncodeToString(client.Proof()) != hex.EncodeToString(serverM1) {
		t.Error("client M1 does not match server-computed M1")
	}

	serverM2 := p.computeM2(ABytes, serverM1, serverK)
	if err := client.VerifyServer(serverM2); err != nil {
		t.Errorf("VerifyServer: %v", err)
	}
}

func TestVerifyServerRejectsBadM2(t *testing.T) {
	p := Apple2048Params()
	client, _ := NewClient(p)

	username := []byte("user")
	password := []byte("pass")
	salt := []byte("salt1234salt1234")

	x := p.computeX(salt, username, password)
	v := new(big.Int).Exp(p.G, x, p.N)
	bBytes := []byte{42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73}
	bInt := new(big.Int).SetBytes(bBytes)
	k := p.multiplier()
	gB := new(big.Int).Exp(p.G, bInt, p.N)
	kv := new(big.Int).Mul(k, v)
	B := new(big.Int).Add(kv, gB)
	B.Mod(B, p.N)

	client.ProcessChallenge(username, password, salt, p.padToN(B))

	badM2 := make([]byte, 32)
	if err := client.VerifyServer(badM2); err == nil {
		t.Error("VerifyServer should reject wrong M2")
	}
}

func TestComputeM1XORProperty(t *testing.T) {
	p := Apple2048Params()

	// The XOR of H(N) and H(g) should be commutative
	hN := p.hashBytes(p.N.Bytes())
	hg := p.hashBytes(p.padToN(p.G))

	xor1 := make([]byte, len(hN))
	xor2 := make([]byte, len(hN))
	for i := range hN {
		xor1[i] = hN[i] ^ hg[i]
		xor2[i] = hg[i] ^ hN[i]
	}

	if hex.EncodeToString(xor1) != hex.EncodeToString(xor2) {
		t.Error("XOR should be commutative")
	}

	// H(N) and H(g) should differ
	if hex.EncodeToString(hN) == hex.EncodeToString(hg) {
		t.Error("H(N) and H(g) should differ")
	}
}
