package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
