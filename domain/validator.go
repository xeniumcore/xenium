package domain

import "crypto/ecdsa"

type Validator struct {
	Name     string
	Stake    int
	PubKey   string
	PrivKey  *ecdsa.PrivateKey
	LastSlot uint64
}

type ValidatorStats struct {
	MissedSlots      uint64
	JailedUntilEpoch uint64
	Slashed          bool
}
