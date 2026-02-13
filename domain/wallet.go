package domain

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
)

type Wallet struct {
	PrivateKey *ecdsa.PrivateKey
	PublicKey  string
	Address    string
}

func NewWallet() (*Wallet, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	pubBytes := elliptic.Marshal(priv.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	pubHex := hex.EncodeToString(pubBytes)
	addr, err := AddressFromPubKey(pubHex)
	if err != nil {
		return nil, err
	}
	return &Wallet{
		PrivateKey: priv,
		PublicKey:  pubHex,
		Address:    addr,
	}, nil
}
