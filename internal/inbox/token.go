package inbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"strings"
)

// sealer turns a message's place in the mailbox (folder, UID) into an
// opaque link token, sealed with AES-GCM and bound to one address.
// A visitor can only follow links the server printed for them: UIDs
// cannot be walked, counted, or replayed under another address's key.
// The key lives in memory, so links die with the process; the inbox
// page mints fresh ones.
type sealer struct{ aead cipher.AEAD }

func newSealer() *sealer {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("inbox: no randomness for link tokens: " + err.Error())
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &sealer{aead: aead}
}

func (s *sealer) seal(address, folder string, uid uint32) string {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("inbox: no randomness for link tokens: " + err.Error())
	}
	plain := binary.BigEndian.AppendUint32(nil, uid)
	plain = append(plain, folder...)
	out := s.aead.Seal(nonce, nonce, plain, []byte(strings.ToLower(address)))
	return base64.RawURLEncoding.EncodeToString(out)
}

func (s *sealer) open(address, token string) (folder string, uid uint32, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	n := s.aead.NonceSize()
	if err != nil || len(raw) < n+4+s.aead.Overhead() {
		return "", 0, false
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], []byte(strings.ToLower(address)))
	if err != nil || len(plain) < 4 {
		return "", 0, false
	}
	return string(plain[4:]), binary.BigEndian.Uint32(plain[:4]), true
}
