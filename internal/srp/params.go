package srp

import (
	"crypto"
	_ "crypto/sha256"
	"math/big"
)

type Params struct {
	G             *big.Int
	N             *big.Int
	Hash          crypto.Hash
	NLengthBits   int
	NoUserNameInX bool
}

var ng2048 = &Params{
	G:           big.NewInt(2),
	NLengthBits: 2048,
	Hash:        crypto.SHA256,
}

func init() {
	n := new(big.Int)
	n.SetString(
		"AC6BDB41324A9A9BF166DE5E1389582FAF72B6651987EE07FC3192943DB56050"+
			"A37329CBB4A099ED8193E0757767A13DD52312AB4B03310DCD7F48A9DA04FD50"+
			"E8083969EDB767B0CF6095179A163AB3661A05FBD5FAAAE82918A9962F0B93B8"+
			"55F97993EC975EEAA80D740ADBF4FF747359D041D5C33EA71D281E446B14773B"+
			"CA97B43A23FB801676BD207A436C6481F1D2B9078717461A5B9D32E688F87748"+
			"544523B524B0D57D5EA77A2775D2ECFA032CFBDBF52FB3786160279004E57AE6"+
			"AF874E7303CE53299CCC041C7BC308D82A5698F3A8D0C38271AE35F8E9DBFBB6"+
			"94B5C803D89F7AE435DE236D525F54759B65E372FCD68EF20FA7111F9E4AFF73",
		16,
	)
	ng2048.N = n
}

func Apple2048Params() *Params {
	return &Params{
		G:             ng2048.G,
		N:             ng2048.N,
		Hash:          ng2048.Hash,
		NLengthBits:   ng2048.NLengthBits,
		NoUserNameInX: true,
	}
}

func (p *Params) hashBytes(data []byte) []byte {
	h := p.Hash.New()
	h.Write(data)
	return h.Sum(nil)
}

func (p *Params) padToN(n *big.Int) []byte {
	b := n.Bytes()
	padLen := p.NLengthBits/8 - len(b)
	if padLen <= 0 {
		return b
	}
	padded := make([]byte, p.NLengthBits/8)
	copy(padded[padLen:], b)
	return padded
}

func (p *Params) multiplier() *big.Int {
	h := p.Hash.New()
	nBytes := p.N.Bytes()
	gBytes := p.padToN(p.G)
	h.Write(nBytes)
	h.Write(gBytes)
	k := new(big.Int)
	k.SetBytes(h.Sum(nil))
	return k
}

func (p *Params) computeU(A, B *big.Int) *big.Int {
	h := p.Hash.New()
	h.Write(p.padToN(A))
	h.Write(p.padToN(B))
	u := new(big.Int)
	u.SetBytes(h.Sum(nil))
	return u
}

func (p *Params) computeX(salt, username, password []byte) *big.Int {
	inner := p.Hash.New()
	if !p.NoUserNameInX {
		inner.Write(username)
	}
	inner.Write([]byte(":"))
	inner.Write(password)
	digest := inner.Sum(nil)

	outer := p.Hash.New()
	outer.Write(salt)
	outer.Write(digest)

	x := new(big.Int)
	x.SetBytes(outer.Sum(nil))
	return x
}

func (p *Params) computeClientS(k, x, a *big.Int, B *big.Int, u *big.Int) []byte {
	// S = (B - k * g^x) ^ (a + u * x) mod N
	gx := new(big.Int).Exp(p.G, x, p.N)
	kgx := new(big.Int).Mul(k, gx)
	diff := new(big.Int).Sub(B, kgx)
	diff.Mod(diff, p.N)

	ux := new(big.Int).Mul(u, x)
	exp := new(big.Int).Add(a, ux)

	S := new(big.Int).Exp(diff, exp, p.N)
	S.Mod(S, p.N)
	return p.padToN(S)
}

func (p *Params) computeK(S []byte) []byte {
	return p.hashBytes(S)
}

func (p *Params) computeM1(username, salt, A, B, K []byte) []byte {
	hN := p.hashBytes(p.N.Bytes())
	hg := p.hashBytes(p.padToN(p.G))
	hxor := make([]byte, len(hN))
	for i := range hN {
		hxor[i] = hN[i] ^ hg[i]
	}
	hI := p.hashBytes(username)

	h := p.Hash.New()
	h.Write(hxor)
	h.Write(hI)
	h.Write(salt)
	h.Write(A)
	h.Write(B)
	h.Write(K)
	return h.Sum(nil)
}

func (p *Params) computeM2(A, M1, K []byte) []byte {
	h := p.Hash.New()
	h.Write(A)
	h.Write(M1)
	h.Write(K)
	return h.Sum(nil)
}
