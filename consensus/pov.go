package consensus

import (
	"errors"

	"xenium/domain"
)

func VerifyTransactions(txs []domain.Transaction) error {
	for i := range txs {
		if err := VerifyTransactionSignature(txs[i]); err != nil {
			return errors.New("invalid transaction signature at index " + itoa(i))
		}
	}
	return nil
}

func ApplyTransactions(state map[string]int, txs []domain.Transaction) (map[string]int, error) {
	next := make(map[string]int, len(state))
	for k, v := range state {
		next[k] = v
	}
	for i := range txs {
		tx := txs[i]
		if tx.Amount <= 0 {
			return nil, errors.New("invalid amount at index " + itoa(i))
		}
		if tx.From == "" {
			return nil, errors.New("missing sender at index " + itoa(i))
		}
		if next[tx.From] < tx.Amount {
			return nil, errors.New("insufficient balance at index " + itoa(i))
		}
		next[tx.From] -= tx.Amount
		next[tx.To] += tx.Amount
	}
	return next, nil
}

func VerifyBlockLink(prev domain.Block, cur domain.Block) error {
	if cur.PrevHash != prev.Hash {
		return errors.New("invalid prev hash at index " + itoa(int(cur.Index)))
	}
	if cur.Slot < prev.Slot {
		return errors.New("slot regressed at index " + itoa(int(cur.Index)))
	}
	return nil
}

func VerifyPoH(expectedHash [32]byte, expectedTick uint64, cur domain.Block) ([32]byte, uint64, error) {
	if cur.Tick <= expectedTick {
		return expectedHash, expectedTick, errors.New("tick not increasing at index " + itoa(int(cur.Index)))
	}
	if cur.Tick/TicksPerSlot != cur.Slot {
		return expectedHash, expectedTick, errors.New("slot mismatch at index " + itoa(int(cur.Index)))
	}
	for t := expectedTick + 1; t <= cur.Tick; t++ {
		expectedHash = HashPoH(expectedHash, t)
	}
	pohHash, err := ParsePoHHashHex(cur.PoHHash)
	if err != nil {
		return expectedHash, expectedTick, err
	}
	if pohHash != expectedHash {
		return expectedHash, expectedTick, errors.New("invalid poh hash at index " + itoa(int(cur.Index)))
	}
	return expectedHash, cur.Tick, nil
}

func VerifyBlockHash(cur domain.Block) error {
	expected := HashBlock(cur.Index, cur.PrevHash, cur.Slot, cur.Tick, cur.Validator, cur.TxRoot, cur.StateRoot, cur.PoHHash)
	if cur.Hash != expected {
		return errors.New("invalid hash at index " + itoa(int(cur.Index)))
	}
	return nil
}

func VerifyValidator(name string, validators map[string]*domain.Validator, index int) (*domain.Validator, error) {
	v, ok := validators[name]
	if !ok || v.Stake < MinStake {
		return nil, errors.New("unknown validator at index " + itoa(index))
	}
	return v, nil
}

func VerifyLeader(slot uint64, validator string, validators map[string]*domain.Validator, stats map[string]*domain.ValidatorStats) error {
	leader := DeterministicLeader(slot, validators, stats)
	if leader != validator {
		return errors.New("wrong leader at slot " + itoa(int(slot)))
	}
	return nil
}

func VerifyLeaderSnapshot(slot uint64, validator string, stakes map[string]uint64) error {
	leader := LeaderFromSnapshot(slot, stakes)
	if leader != validator {
		return errors.New("wrong leader at slot " + itoa(int(slot)))
	}
	return nil
}

func VerifyBlockSig(cur domain.Block, v *domain.Validator) error {
	return VerifyBlockSignature(cur, v.PubKey)
}

func itoa(v int) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
