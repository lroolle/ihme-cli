package srp

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

type Client struct {
	params     *Params
	secret     *big.Int
	multiplier *big.Int
	A          *big.Int
	M1         []byte
	M2         []byte
	K          []byte
}

func NewClient(params *Params) (*Client, error) {
	a := make([]byte, 32)
	if _, err := rand.Read(a); err != nil {
		return nil, fmt.Errorf("generating SRP secret: %w", err)
	}

	secret := new(big.Int).SetBytes(a)
	A := new(big.Int).Exp(params.G, secret, params.N)
	k := params.multiplier()

	return &Client{
		params:     params,
		secret:     secret,
		multiplier: k,
		A:          A,
	}, nil
}

func (c *Client) PublicKey() []byte {
	return c.params.padToN(c.A)
}

func (c *Client) ProcessChallenge(username, password, salt, serverB []byte) error {
	B := new(big.Int).SetBytes(serverB)
	if B.Cmp(big.NewInt(0)) <= 0 || c.params.N.Cmp(B) <= 0 {
		return errors.New("invalid server public key B")
	}

	x := c.params.computeX(salt, username, password)
	u := c.params.computeU(c.A, B)
	if u.Sign() == 0 {
		return errors.New("invalid SRP parameter u (zero)")
	}

	S := c.params.computeClientS(c.multiplier, x, c.secret, B, u)
	c.K = c.params.computeK(S)

	ABytes := c.params.padToN(c.A)
	BBytes := c.params.padToN(B)
	c.M1 = c.params.computeM1(username, salt, ABytes, BBytes, c.K)
	c.M2 = c.params.computeM2(ABytes, c.M1, c.K)
	return nil
}

func (c *Client) Proof() []byte {
	return c.M1
}

func (c *Client) ServerProof() []byte {
	return c.M2
}

func (c *Client) VerifyServer(serverM2 []byte) error {
	if !bytes.Equal(c.M2, serverM2) {
		return errors.New("server proof verification failed")
	}
	return nil
}

func (c *Client) SessionKey() []byte {
	return c.K
}
