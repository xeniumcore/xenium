package consensus

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"

	"xenium/domain"
)

const MinStake = 10
const BlockReward = 1
const SlashPenalty = 5
const SlashPercent = 2
const MaxMissedSlots = 3
const SlotsPerEpoch = 50
const JailEpochs = 2

func AddValidator(validators map[string]*domain.Validator, stats map[string]*domain.ValidatorStats, name string, stake int, pubKey string, priv *ecdsa.PrivateKey) error {
	if name == "" {
		return errors.New("validator name is required")
	}
	if stake <= 0 {
		return errors.New("stake must be positive")
	}
	if pubKey == "" {
		return errors.New("validator pubkey is required")
	}
	v, ok := validators[name]
	if !ok {
		if stake < MinStake {
			return errors.New("stake below minimum")
		}
		validators[name] = &domain.Validator{Name: name, Stake: stake, PubKey: pubKey, PrivKey: priv}
		if stats != nil {
			if _, ok := stats[name]; !ok {
				stats[name] = &domain.ValidatorStats{}
			}
		}
		return nil
	}
	v.Stake += stake
	if v.PubKey == "" {
		v.PubKey = pubKey
	}
	if v.PrivKey == nil {
		v.PrivKey = priv
	}
	return nil
}

func AddStake(validators map[string]*domain.Validator, name string, amount int) error {
	if amount <= 0 {
		return errors.New("stake amount must be positive")
	}
	v, ok := validators[name]
	if !ok {
		return errors.New("validator not found")
	}
	v.Stake += amount
	return nil
}

func Unstake(validators map[string]*domain.Validator, name string, amount int) error {
	if amount <= 0 {
		return errors.New("unstake amount must be positive")
	}
	v, ok := validators[name]
	if !ok {
		return errors.New("validator not found")
	}
	if amount > v.Stake {
		return errors.New("unstake exceeds stake")
	}
	newStake := v.Stake - amount
	if newStake > 0 && newStake < MinStake {
		return errors.New("stake below minimum")
	}
	if newStake == 0 {
		delete(validators, name)
		return nil
	}
	v.Stake = newStake
	return nil
}

func RewardValidator(validators map[string]*domain.Validator, name string) {
	if v, ok := validators[name]; ok {
		v.Stake += BlockReward
	}
}

func SlashValidator(validators map[string]*domain.Validator, name string, amount int) {
	if amount <= 0 {
		return
	}
	v, ok := validators[name]
	if !ok {
		return
	}
	if amount >= v.Stake {
		delete(validators, name)
		return
	}
	v.Stake -= amount
	if v.Stake < MinStake {
		delete(validators, name)
	}
}

func SlashValidatorPercent(validators map[string]*domain.Validator, name string, percent int) {
	if percent <= 0 {
		return
	}
	v, ok := validators[name]
	if !ok {
		return
	}
	amount := (v.Stake * percent) / 100
	if amount == 0 {
		amount = 1
	}
	SlashValidator(validators, name, amount)
}

func DeterministicLeader(slot uint64, validators map[string]*domain.Validator, stats map[string]*domain.ValidatorStats) string {
	totalStake := 0
	for _, v := range validators {
		if v.Stake < MinStake {
			continue
		}
		if IsJailed(stats, v.Name, slot) {
			continue
		}
		totalStake += v.Stake
	}
	if totalStake == 0 {
		return "genesis"
	}

	draw := deterministicDraw(slot, totalStake)
	running := 0
	for _, name := range sortedValidatorNames(validators) {
		v := validators[name]
		if v.Stake < MinStake {
			continue
		}
		if IsJailed(stats, v.Name, slot) {
			continue
		}
		running += v.Stake
		if draw < running {
			return name
		}
	}
	return "genesis"
}

func LeaderFromSnapshot(slot uint64, stakes map[string]uint64) string {
	totalStake := uint64(0)
	for _, stake := range stakes {
		totalStake += stake
	}
	if totalStake == 0 {
		return "genesis"
	}

	draw := deterministicDraw(slot, int(totalStake))
	running := uint64(0)
	for _, name := range sortedStakeNames(stakes) {
		running += stakes[name]
		if uint64(draw) < running {
			return name
		}
	}
	return "genesis"
}

func IsJailed(stats map[string]*domain.ValidatorStats, name string, slot uint64) bool {
	if stats == nil {
		return false
	}
	s, ok := stats[name]
	if !ok {
		return false
	}
	epoch := slot / SlotsPerEpoch
	return epoch < s.JailedUntilEpoch
}

func deterministicDraw(slot uint64, max int) int {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	sum := sha256.Sum256(buf[:])
	n := binary.LittleEndian.Uint64(sum[:8])
	return int(n % uint64(max))
}

func sortedValidatorNames(m map[string]*domain.Validator) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedStakeNames(m map[string]uint64) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
