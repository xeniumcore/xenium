package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

func AddressFromPubKey(pubKeyHex string) (string, error) {
	raw, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
