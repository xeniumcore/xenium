package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
)

type PoH struct {
	CurrentTick uint64
	Hash        [32]byte
}

const TicksPerSecond = 50
const TicksPerSlot = 20

func NewPoH(seed [32]byte) *PoH {
	return &PoH{CurrentTick: 0, Hash: seed}
}

func (p *PoH) Tick(n uint64) ([32]byte, uint64) {
	for i := uint64(0); i < n; i++ {
		p.CurrentTick++
		p.Hash = HashPoH(p.Hash, p.CurrentTick)
	}
	return p.Hash, n
}

func (p *PoH) Slot() uint64 {
	return p.CurrentTick / TicksPerSlot
}

func HashPoH(prev [32]byte, tick uint64) [32]byte {
	raw := make([]byte, 0, len(prev)+16)
	raw = append(raw, prev[:]...)
	raw = append(raw, []byte(strconv.FormatUint(tick, 10))...)
	return sha256.Sum256(raw)
}

func HashPoHSeed(nonce int64) [32]byte {
	raw := "poh|" + strconv.FormatInt(nonce, 10)
	return sha256.Sum256([]byte(raw))
}

func PoHHashHex(hash [32]byte) string {
	return hex.EncodeToString(hash[:])
}

func ParsePoHHashHex(s string) ([32]byte, error) {
	raw, err := hex.DecodeString(s)
	if err != nil {
		return [32]byte{}, err
	}
	if len(raw) != 32 {
		return [32]byte{}, errors.New("invalid poh hash length")
	}
	var out [32]byte
	copy(out[:], raw)
	return out, nil
}
