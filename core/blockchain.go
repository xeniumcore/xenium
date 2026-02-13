package core

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/rand"
	"sort"

	"xenium/consensus"
	"xenium/domain"
	"xenium/ports"
)

type ChainScore struct {
	Slot             uint64
	CumulativeWeight uint64
	Hash             string
}

type ChainConfig struct {
	MaxReorgDepth        int
	FinalitySlots        uint64
	MinReorgWeightDeltaP int
	EpochLength          uint64
	DeterministicPoH     bool
	PoHSeed              int64
}

type ReorgMetrics struct {
	Info     uint64
	Warn     uint64
	Error    uint64
	Critical uint64
}

type EquivocationProof struct {
	Slot      uint64
	Validator string
	BlockA    string
	BlockB    string
}

type EpochSnapshot struct {
	Epoch      uint64
	TotalStake uint64
	Validators map[string]uint64
}

type Blockchain struct {
	Chain            []domain.Block
	Blocks           map[string]domain.Block
	Parents          map[string]string
	CanonicalTip     string
	Validators       map[string]*domain.Validator
	Stats            map[string]*domain.ValidatorStats
	rand             *rand.Rand
	poh              *consensus.PoH
	State            map[string]int
	Genesis          map[string]int
	SlotProduced     map[uint64]string
	SlotProducers    map[uint64]map[string]string
	Equivocations    []EquivocationProof
	LastProcessedSlot uint64
	FinalizedSlot     uint64
	Config           ChainConfig
	ReorgStats       ReorgMetrics
	Clock            ports.Clock
	Logger           ports.Logger
	currentEpoch     uint64
	snapshots        map[uint64]*EpochSnapshot
}

func NewBlockchain(cfg ChainConfig, clock ports.Clock, logger ports.Logger) *Blockchain {
	bc := &Blockchain{
		Blocks:        make(map[string]domain.Block),
		Parents:       make(map[string]string),
		Validators:    make(map[string]*domain.Validator),
		Stats:         make(map[string]*domain.ValidatorStats),
		State:         make(map[string]int),
		Genesis:       make(map[string]int),
		SlotProduced:  make(map[uint64]string),
		SlotProducers: make(map[uint64]map[string]string),
		Config:        cfg,
		Clock:         clock,
		Logger:        ensureLogger(logger),
		snapshots:     make(map[uint64]*EpochSnapshot),
	}
	if bc.Config.MaxReorgDepth == 0 {
		bc.Config.MaxReorgDepth = 2
	}
	if bc.Config.FinalitySlots == 0 {
		bc.Config.FinalitySlots = 2
	}
	if bc.Config.EpochLength == 0 {
		bc.Config.EpochLength = consensus.SlotsPerEpoch
	}
	seed := int64(0)
	if bc.Config.DeterministicPoH {
		seed = bc.Config.PoHSeed
	} else if bc.Clock != nil {
		seed = bc.Clock.UnixNano()
	}
	bc.rand = rand.New(rand.NewSource(seed))
	genesis := bc.createGenesisBlock()
	pohSeed, _ := consensus.ParsePoHHashHex(genesis.PoHHash)
	bc.poh = consensus.NewPoH(pohSeed)

	bc.insertBlock(genesis)
	bc.CanonicalTip = genesis.Hash
	bc.rebuildCanonicalChain()
	bc.updateFinality()
	return bc
}

func (bc *Blockchain) SetBalance(address string, amount int) {
	if amount < 0 {
		return
	}
	bc.State[address] = amount
	if len(bc.Chain) <= 1 {
		bc.Genesis[address] = amount
	}
}

func (bc *Blockchain) AddValidator(name string, stake int, pubKey string, priv *ecdsa.PrivateKey) error {
	return consensus.AddValidator(bc.Validators, bc.Stats, name, stake, pubKey, priv)
}

func (bc *Blockchain) AddBlock(txs []domain.Transaction) error {
	if len(bc.Validators) == 0 {
		return errors.New("no validators available")
	}
	if bc.poh == nil {
		return errors.New("poh not initialized")
	}
	prev := bc.Blocks[bc.CanonicalTip]
	_, _ = bc.poh.Tick(consensus.TicksPerSlot)
	slot := bc.poh.Slot()
	bc.ensureSnapshotForSlot(slot)
	validator := bc.leaderForSlot(slot)

	if err := consensus.VerifyTransactions(txs); err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return err
	}
	nextState, err := consensus.ApplyTransactions(bc.State, txs)
	if err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return err
	}

	txRoot := consensus.TxRoot(txs)
	stateRoot := consensus.StateRoot(nextState)
	pohHash := consensus.PoHHashHex(bc.poh.Hash)

	block := domain.Block{
		Index:        prev.Index + 1,
		PrevHash:     prev.Hash,
		Slot:         slot,
		Tick:         bc.poh.CurrentTick,
		Validator:    validator,
		TxRoot:       txRoot,
		StateRoot:    stateRoot,
		PoHHash:      pohHash,
		Transactions: txs,
	}

	v := bc.Validators[validator]
	if v == nil || v.PrivKey == nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return errors.New("missing validator signing key")
	}
	if err := consensus.SignBlock(v.PrivKey, &block); err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return err
	}

	if err := bc.verifyBlockOnAccept(prev, block, bc.State); err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return err
	}

	eqErr := bc.registerSlotProducer(block)
	bc.insertBlock(block)
	bc.updateCanonical(block.Hash)
	consensus.RewardValidator(bc.Validators, validator)
	bc.processMissedSlots(bc.chainTipSlot())
	return eqErr
}

func (bc *Blockchain) AddBlockExternal(prevHash string, txs []domain.Transaction) (string, error) {
	if len(bc.Validators) == 0 {
		return "", errors.New("no validators available")
	}
	if bc.poh == nil {
		return "", errors.New("poh not initialized")
	}
	parent, ok := bc.Blocks[prevHash]
	if !ok {
		return "", errors.New("unknown parent hash")
	}

	_, _ = bc.poh.Tick(consensus.TicksPerSlot)
	slot := bc.poh.Slot()
	bc.ensureSnapshotForSlot(slot)
	validator := bc.leaderForSlot(slot)

	if err := consensus.VerifyTransactions(txs); err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return "", err
	}
	parentState, err := bc.stateAtTip(prevHash)
	if err != nil {
		return "", err
	}
	nextState, err := consensus.ApplyTransactions(parentState, txs)
	if err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return "", err
	}

	txRoot := consensus.TxRoot(txs)
	stateRoot := consensus.StateRoot(nextState)
	pohHash := consensus.PoHHashHex(bc.poh.Hash)

	block := domain.Block{
		Index:        parent.Index + 1,
		PrevHash:     parent.Hash,
		Slot:         slot,
		Tick:         bc.poh.CurrentTick,
		Validator:    validator,
		TxRoot:       txRoot,
		StateRoot:    stateRoot,
		PoHHash:      pohHash,
		Transactions: txs,
	}

	v := bc.Validators[validator]
	if v == nil || v.PrivKey == nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return "", errors.New("missing validator signing key")
	}
	if err := consensus.SignBlock(v.PrivKey, &block); err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return "", err
	}

	if err := bc.verifyBlockOnAccept(parent, block, parentState); err != nil {
		consensus.SlashValidator(bc.Validators, validator, consensus.SlashPenalty)
		return "", err
	}

	eqErr := bc.registerSlotProducer(block)
	bc.insertBlock(block)
	bc.updateCanonical(block.Hash)
	consensus.RewardValidator(bc.Validators, validator)
	bc.processMissedSlots(bc.chainTipSlot())
	return block.Hash, eqErr
}

func (bc *Blockchain) VerifyChain() error {
	if len(bc.Chain) == 0 {
		return errors.New("empty chain")
	}
	genesis := bc.Chain[0]
	expectedGenesisHash := consensus.HashBlock(genesis.Index, genesis.PrevHash, genesis.Slot, genesis.Tick, genesis.Validator, genesis.TxRoot, genesis.StateRoot, genesis.PoHHash)
	if genesis.Hash != expectedGenesisHash {
		return errors.New("invalid genesis hash")
	}
	expectedHash, err := consensus.ParsePoHHashHex(genesis.PoHHash)
	if err != nil {
		return err
	}
	expectedTick := genesis.Tick
	seenSlots := make(map[uint64]string)
	state := make(map[string]int)
	for k, v := range bc.Genesis {
		state[k] = v
	}
	for i := 1; i < len(bc.Chain); i++ {
		prev := bc.Chain[i-1]
		cur := bc.Chain[i]

		if err := consensus.VerifyBlockLink(prev, cur); err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		nextHash, nextTick, err := consensus.VerifyPoH(expectedHash, expectedTick, cur)
		if err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		expectedHash = nextHash
		expectedTick = nextTick
		if err := consensus.VerifyBlockHash(cur); err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		snap := bc.snapshotForSlot(cur.Slot)
		if snap == nil {
			return errors.New("missing epoch snapshot for slot " + itoa(int(cur.Slot)))
		}
		if err := consensus.VerifyLeaderSnapshot(cur.Slot, cur.Validator, snap.Validators); err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		if prevValidator, ok := seenSlots[cur.Slot]; ok && prevValidator != "" {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return errors.New("double produce at slot " + itoa(int(cur.Slot)))
		}
		seenSlots[cur.Slot] = cur.Validator
		v, err := consensus.VerifyValidator(cur.Validator, bc.Validators, i)
		if err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		if err := consensus.VerifyBlockSig(cur, v); err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		if err := consensus.VerifyTransactions(cur.Transactions); err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		if consensus.TxRoot(cur.Transactions) != cur.TxRoot {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return errors.New("invalid tx root at index " + itoa(i))
		}
		nextState, err := consensus.ApplyTransactions(state, cur.Transactions)
		if err != nil {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return err
		}
		if consensus.StateRoot(nextState) != cur.StateRoot {
			consensus.SlashValidator(bc.Validators, cur.Validator, consensus.SlashPenalty)
			return errors.New("invalid state root at index " + itoa(i))
		}
		state = nextState
	}
	return nil
}

func (bc *Blockchain) verifyBlockOnAccept(prev domain.Block, block domain.Block, state map[string]int) error {
	if block.PrevHash != prev.Hash {
		return errors.New("invalid prev hash for block")
	}
	snap := bc.snapshotForSlot(block.Slot)
	if snap == nil {
		return errors.New("missing epoch snapshot for block")
	}
	if err := consensus.VerifyLeaderSnapshot(block.Slot, block.Validator, snap.Validators); err != nil {
		return err
	}
	if err := consensus.VerifyTransactions(block.Transactions); err != nil {
		return err
	}
	if consensus.TxRoot(block.Transactions) != block.TxRoot {
		return errors.New("invalid tx root for block")
	}
	nextState, err := consensus.ApplyTransactions(state, block.Transactions)
	if err != nil {
		return err
	}
	if consensus.StateRoot(nextState) != block.StateRoot {
		return errors.New("invalid state root for block")
	}
	v := bc.Validators[block.Validator]
	if v == nil {
		return errors.New("unknown validator for block")
	}
	if err := consensus.VerifyBlockSignature(block, v.PubKey); err != nil {
		return err
	}
	return nil
}

func (bc *Blockchain) createGenesisBlock() domain.Block {
	seed := consensus.HashPoHSeed(bc.rand.Int63())
	pohHash := consensus.PoHHashHex(seed)
	genesis := domain.Block{
		Index:     0,
		PrevHash:  "GENESIS",
		Slot:      0,
		Tick:      0,
		Validator: "genesis",
		TxRoot:    consensus.TxRoot(nil),
		StateRoot: consensus.StateRoot(nil),
		PoHHash:   pohHash,
	}
	genesis.Hash = consensus.HashBlock(genesis.Index, genesis.PrevHash, genesis.Slot, genesis.Tick, genesis.Validator, genesis.TxRoot, genesis.StateRoot, genesis.PoHHash)
	return genesis
}

func (bc *Blockchain) insertBlock(block domain.Block) {
	bc.Blocks[block.Hash] = block
	bc.Parents[block.Hash] = block.PrevHash
}

func (bc *Blockchain) updateCanonical(tipHash string) bool {
	if bc.CanonicalTip == "" {
		bc.CanonicalTip = tipHash
		bc.rebuildCanonicalChain()
		bc.updateFinality()
		return true
	}
	currentScore := bc.scoreTip(bc.CanonicalTip)
	newScore := bc.scoreTip(tipHash)
	if betterScore(newScore, currentScore) {
		newChain, err := bc.chainFromTip(tipHash)
		if err != nil {
			return false
		}
		reorgDepth, divergeSlot := computeReorgDepthAndSlot(bc.Chain, newChain)
		if bc.FinalizedSlot > 0 && divergeSlot <= bc.FinalizedSlot {
			bc.ReorgStats.Critical++
			bc.Logger.Criticalf("Reorg attempt touching finalized slot=%d", divergeSlot)
			return false
		}
		if reorgDepth > bc.Config.MaxReorgDepth {
			bc.ReorgStats.Error++
			bc.Logger.Errorf("Reorg rejected depth=%d exceeds max=%d (fromSlot=%d toSlot=%d)",
				reorgDepth, bc.Config.MaxReorgDepth, divergeSlot, newChain[len(newChain)-1].Slot)
			return false
		}
		if !bc.weightDeltaSatisfied(currentScore.CumulativeWeight, newScore.CumulativeWeight) {
			required, actual := bc.weightDeltaRequired(currentScore.CumulativeWeight, newScore.CumulativeWeight)
			bc.ReorgStats.Error++
			bc.Logger.Errorf("Reorg rejected: insufficient weight delta required=%d actual=%d minDeltaPct=%d",
				required, actual, bc.Config.MinReorgWeightDeltaP)
			return false
		}
		if reorgDepth > 0 {
			if reorgDepth > 1 {
				bc.ReorgStats.Warn++
				bc.Logger.Warnf("Reorg detected depth=%d (fromSlot=%d toSlot=%d)",
					reorgDepth, divergeSlot, newChain[len(newChain)-1].Slot)
			} else {
				bc.ReorgStats.Info++
				bc.Logger.Infof("Reorg detected depth=%d (fromSlot=%d toSlot=%d)",
					reorgDepth, divergeSlot, newChain[len(newChain)-1].Slot)
			}
		}
		bc.CanonicalTip = tipHash
		bc.Chain = newChain
		bc.rebuildSlotMap()
		bc.rebuildStateFromCanonical()
		bc.updateFinality()
		return true
	}
	return false
}

func (bc *Blockchain) scoreTip(tipHash string) ChainScore {
	block, ok := bc.Blocks[tipHash]
	if !ok {
		return ChainScore{}
	}
	weight := uint64(0)
	cur := block
	for {
		weight += bc.snapshotStake(cur.Slot, cur.Validator)
		if cur.PrevHash == "GENESIS" {
			break
		}
		parent, ok := bc.Blocks[cur.PrevHash]
		if !ok {
			break
		}
		cur = parent
	}
	return ChainScore{Slot: block.Slot, CumulativeWeight: weight, Hash: block.Hash}
}

func (bc *Blockchain) scoreTipCached(tipHash string, cache map[string]uint64) ChainScore {
	block, ok := bc.Blocks[tipHash]
	if !ok {
		return ChainScore{}
	}
	weight := bc.cumulativeWeightCached(tipHash, cache)
	return ChainScore{Slot: block.Slot, CumulativeWeight: weight, Hash: block.Hash}
}

func (bc *Blockchain) cumulativeWeightCached(hash string, cache map[string]uint64) uint64 {
	if v, ok := cache[hash]; ok {
		return v
	}
	block, ok := bc.Blocks[hash]
	if !ok {
		return 0
	}
	weight := bc.snapshotStake(block.Slot, block.Validator)
	if block.PrevHash != "GENESIS" {
		weight += bc.cumulativeWeightCached(block.PrevHash, cache)
	}
	cache[hash] = weight
	return weight
}

func betterScore(a ChainScore, b ChainScore) bool {
	if a.CumulativeWeight != b.CumulativeWeight {
		return a.CumulativeWeight > b.CumulativeWeight
	}
	if a.Slot != b.Slot {
		return a.Slot > b.Slot
	}
	return a.Hash < b.Hash
}

func (bc *Blockchain) weightDeltaSatisfied(oldWeight uint64, newWeight uint64) bool {
	if bc.Config.MinReorgWeightDeltaP <= 0 {
		return true
	}
	if newWeight <= oldWeight {
		return false
	}
	active := bc.activeStake()
	minDelta := (active * uint64(bc.Config.MinReorgWeightDeltaP)) / 100
	if minDelta == 0 {
		minDelta = 1
	}
	return newWeight >= oldWeight+minDelta
}

func (bc *Blockchain) weightDeltaRequired(oldWeight uint64, newWeight uint64) (uint64, uint64) {
	active := bc.activeStake()
	minDelta := (active * uint64(bc.Config.MinReorgWeightDeltaP)) / 100
	if minDelta == 0 {
		minDelta = 1
	}
	actual := uint64(0)
	if newWeight > oldWeight {
		actual = newWeight - oldWeight
	}
	return minDelta, actual
}

func (bc *Blockchain) WeightDeltaRequired(oldWeight uint64, newWeight uint64) (uint64, uint64) {
	return bc.weightDeltaRequired(oldWeight, newWeight)
}

func (bc *Blockchain) activeStake() uint64 {
	snap := bc.snapshotForSlot(bc.chainTipSlot())
	if snap == nil {
		return 0
	}
	return snap.TotalStake
}

func (bc *Blockchain) rebuildCanonicalChain() {
	if bc.CanonicalTip == "" {
		bc.Chain = nil
		return
	}
	var chain []domain.Block
	curHash := bc.CanonicalTip
	for {
		cur, ok := bc.Blocks[curHash]
		if !ok {
			break
		}
		chain = append(chain, cur)
		if cur.PrevHash == "GENESIS" {
			break
		}
		curHash = cur.PrevHash
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	bc.Chain = chain
	bc.rebuildSlotMap()
	bc.rebuildStateFromCanonical()
}

func (bc *Blockchain) rebuildSlotMap() {
	bc.SlotProduced = make(map[uint64]string)
	for i := 1; i < len(bc.Chain); i++ {
		b := bc.Chain[i]
		bc.SlotProduced[b.Slot] = b.Validator
	}
}

func (bc *Blockchain) rebuildStateFromCanonical() {
	state := make(map[string]int)
	for k, v := range bc.Genesis {
		state[k] = v
	}
	for i := 1; i < len(bc.Chain); i++ {
		next, err := consensus.ApplyTransactions(state, bc.Chain[i].Transactions)
		if err != nil {
			return
		}
		state = next
	}
	bc.State = state
}

func (bc *Blockchain) updateFinality() {
	tipSlot := bc.chainTipSlot()
	if tipSlot < bc.Config.FinalitySlots {
		return
	}
	finalized := tipSlot - bc.Config.FinalitySlots
	if finalized > bc.FinalizedSlot {
		bc.FinalizedSlot = finalized
	}
}

func (bc *Blockchain) chainTipSlot() uint64 {
	if len(bc.Chain) == 0 {
		return 0
	}
	return bc.Chain[len(bc.Chain)-1].Slot
}

func (bc *Blockchain) epochForSlot(slot uint64) uint64 {
	if bc.Config.EpochLength == 0 {
		return 0
	}
	return slot / bc.Config.EpochLength
}

func (bc *Blockchain) ensureSnapshotForSlot(slot uint64) {
	epoch := bc.epochForSlot(slot)
	bc.ensureSnapshot(epoch)
}

func (bc *Blockchain) ensureSnapshot(epoch uint64) {
	if _, ok := bc.snapshots[epoch]; ok {
		return
	}
	snap := &EpochSnapshot{
		Epoch:      epoch,
		Validators: make(map[string]uint64),
	}
	epochSlot := epoch * bc.Config.EpochLength
	for _, v := range bc.Validators {
		if v.Stake < consensus.MinStake {
			continue
		}
		if consensus.IsJailed(bc.Stats, v.Name, epochSlot) {
			continue
		}
		snap.Validators[v.Name] = uint64(v.Stake)
		snap.TotalStake += uint64(v.Stake)
	}
	bc.snapshots[epoch] = snap
	bc.currentEpoch = epoch
}

func (bc *Blockchain) snapshotForSlot(slot uint64) *EpochSnapshot {
	epoch := bc.epochForSlot(slot)
	bc.ensureSnapshot(epoch)
	return bc.snapshots[epoch]
}

func (bc *Blockchain) GetEpochSnapshot(slot uint64) EpochSnapshot {
	snap := bc.snapshotForSlot(slot)
	if snap == nil {
		return EpochSnapshot{}
	}
	out := EpochSnapshot{
		Epoch:      snap.Epoch,
		TotalStake: snap.TotalStake,
		Validators: make(map[string]uint64, len(snap.Validators)),
	}
	for k, v := range snap.Validators {
		out.Validators[k] = v
	}
	return out
}

func (bc *Blockchain) GetAllEpochSnapshots() []EpochSnapshot {
	if len(bc.snapshots) == 0 {
		return nil
	}
	epochs := make([]uint64, 0, len(bc.snapshots))
	for k := range bc.snapshots {
		epochs = append(epochs, k)
	}
	sort.Slice(epochs, func(i, j int) bool { return epochs[i] < epochs[j] })
	out := make([]EpochSnapshot, 0, len(epochs))
	for _, e := range epochs {
		s := bc.snapshots[e]
		if s == nil {
			continue
		}
		cp := EpochSnapshot{
			Epoch:      s.Epoch,
			TotalStake: s.TotalStake,
			Validators: make(map[string]uint64, len(s.Validators)),
		}
		for k, v := range s.Validators {
			cp.Validators[k] = v
		}
		out = append(out, cp)
	}
	return out
}

func (bc *Blockchain) snapshotStake(slot uint64, validator string) uint64 {
	snap := bc.snapshotForSlot(slot)
	if snap == nil {
		return 0
	}
	return snap.Validators[validator]
}

func (bc *Blockchain) leaderForSlot(slot uint64) string {
	snap := bc.snapshotForSlot(slot)
	if snap == nil {
		return "genesis"
	}
	return consensus.LeaderFromSnapshot(slot, snap.Validators)
}

func (bc *Blockchain) processMissedSlots(targetSlot uint64) {
	if targetSlot <= bc.LastProcessedSlot {
		return
	}
	for slot := bc.LastProcessedSlot + 1; slot <= targetSlot; slot++ {
		leader := bc.leaderForSlot(slot)
		if leader == "genesis" {
			continue
		}
		producedBy := bc.SlotProduced[slot]
		if producedBy != leader {
			stats := bc.ensureStats(leader)
			stats.MissedSlots++
			if stats.MissedSlots > consensus.MaxMissedSlots {
				consensus.SlashValidatorPercent(bc.Validators, leader, consensus.SlashPercent)
				stats.MissedSlots = 0
				stats.JailedUntilEpoch = (slot / consensus.SlotsPerEpoch) + consensus.JailEpochs
			}
		}
	}
	bc.LastProcessedSlot = targetSlot
}

func (bc *Blockchain) ensureStats(name string) *domain.ValidatorStats {
	if bc.Stats == nil {
		bc.Stats = make(map[string]*domain.ValidatorStats)
	}
	stats, ok := bc.Stats[name]
	if !ok {
		stats = &domain.ValidatorStats{}
		bc.Stats[name] = stats
	}
	return stats
}

func (bc *Blockchain) registerSlotProducer(block domain.Block) error {
	if bc.SlotProducers == nil {
		bc.SlotProducers = make(map[uint64]map[string]string)
	}
	slotMap := bc.SlotProducers[block.Slot]
	if slotMap == nil {
		slotMap = make(map[string]string)
		bc.SlotProducers[block.Slot] = slotMap
	}
	if existing, ok := slotMap[block.Validator]; ok && existing != block.Hash {
		bc.handleEquivocation(block.Validator, block.Slot, existing, block.Hash)
		return ErrEquivocation
	}
	slotMap[block.Validator] = block.Hash
	return nil
}

func (bc *Blockchain) handleEquivocation(validator string, slot uint64, h1 string, h2 string) {
	proof := EquivocationProof{
		Slot:      slot,
		Validator: validator,
		BlockA:    h1,
		BlockB:    h2,
	}
	bc.Equivocations = append(bc.Equivocations, proof)
	stats := bc.ensureStats(validator)
	stats.Slashed = true
	consensus.SlashValidatorPercent(bc.Validators, validator, consensus.SlashPercent)
	stats.JailedUntilEpoch = (slot / consensus.SlotsPerEpoch) + consensus.JailEpochs
	bc.Logger.Errorf("Equivocation detected validator=%s slot=%d block1=%s block2=%s jailedUntil=%d",
		validator, slot, h1, h2, stats.JailedUntilEpoch)
}

func (bc *Blockchain) stateAtTip(tipHash string) (map[string]int, error) {
	chain, err := bc.chainFromTip(tipHash)
	if err != nil {
		return nil, err
	}
	state := make(map[string]int)
	for k, v := range bc.Genesis {
		state[k] = v
	}
	for i := 1; i < len(chain); i++ {
		next, err := consensus.ApplyTransactions(state, chain[i].Transactions)
		if err != nil {
			return nil, err
		}
		state = next
	}
	return state, nil
}

func (bc *Blockchain) chainFromTip(tipHash string) ([]domain.Block, error) {
	if tipHash == "" {
		return nil, errors.New("empty tip hash")
	}
	var chain []domain.Block
	curHash := tipHash
	for {
		cur, ok := bc.Blocks[curHash]
		if !ok {
			return nil, errors.New("missing block in chain")
		}
		chain = append(chain, cur)
		if cur.PrevHash == "GENESIS" {
			break
		}
		curHash = cur.PrevHash
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func computeReorgDepthAndSlot(oldChain []domain.Block, newChain []domain.Block) (int, uint64) {
	minLen := len(oldChain)
	if len(newChain) < minLen {
		minLen = len(newChain)
	}
	diverge := minLen
	for i := 0; i < minLen; i++ {
		if oldChain[i].Hash != newChain[i].Hash {
			diverge = i
			break
		}
	}
	reorgDepth := len(oldChain) - diverge
	var divergeSlot uint64
	if diverge < len(oldChain) {
		divergeSlot = oldChain[diverge].Slot
	} else if diverge < len(newChain) {
		divergeSlot = newChain[diverge].Slot
	}
	return reorgDepth, divergeSlot
}

func (bc *Blockchain) CanonicalTipHash() string {
	return bc.CanonicalTip
}

func (bc *Blockchain) ScoreTip(tipHash string) ChainScore {
	return bc.scoreTip(tipHash)
}

func (bc *Blockchain) GetReorgStats() ReorgMetrics {
	return bc.ReorgStats
}

func (bc *Blockchain) PrintReorgStats() {
	fmt.Printf("ReorgStats: INFO=%d WARN=%d ERROR=%d CRITICAL=%d\n",
		bc.ReorgStats.Info, bc.ReorgStats.Warn, bc.ReorgStats.Error, bc.ReorgStats.Critical)
}

func (bc *Blockchain) ResetReorgStats() {
	bc.ReorgStats = ReorgMetrics{}
}

type ValidatorSummary struct {
	Name        string
	Produced    uint64
	Missed      uint64
	MissRate    float64
	Slashed     bool
	JailedUntil uint64
}

type ForkCandidate struct {
	Hash             string
	Slot             uint64
	CumulativeWeight uint64
	Parent           string
}

func (bc *Blockchain) GetValidatorSummaries() []ValidatorSummary {
	names := make([]string, 0, len(bc.Validators))
	for name := range bc.Validators {
		names = append(names, name)
	}
	sort.Strings(names)

	produced := make(map[string]uint64, len(names))
	for _, v := range bc.SlotProduced {
		produced[v]++
	}

	out := make([]ValidatorSummary, 0, len(names))
	for _, name := range names {
		stats := bc.Stats[name]
		var missed uint64
		var slashed bool
		var jailed uint64
		if stats != nil {
			missed = stats.MissedSlots
			slashed = stats.Slashed
			jailed = stats.JailedUntilEpoch
		}
		prod := produced[name]
		total := prod + missed
		var rate float64
		if total > 0 {
			rate = float64(missed) / float64(total)
		}
		out = append(out, ValidatorSummary{
			Name:        name,
			Produced:    prod,
			Missed:      missed,
			MissRate:    rate,
			Slashed:     slashed,
			JailedUntil: jailed,
		})
	}
	return out
}

func (bc *Blockchain) GetForkCandidates() []ForkCandidate {
	if len(bc.Blocks) == 0 {
		return nil
	}
	hasChild := make(map[string]bool, len(bc.Blocks))
	for _, parent := range bc.Parents {
		if parent != "" && parent != "GENESIS" {
			hasChild[parent] = true
		}
	}
	weightCache := make(map[string]uint64, len(bc.Blocks))
	candidates := make([]ForkCandidate, 0)
	for hash, block := range bc.Blocks {
		if hash == "" {
			continue
		}
		if hasChild[hash] {
			continue
		}
		score := bc.scoreTipCached(hash, weightCache)
		candidates = append(candidates, ForkCandidate{
			Hash:             hash,
			Slot:             score.Slot,
			CumulativeWeight: score.CumulativeWeight,
			Parent:           block.PrevHash,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CumulativeWeight != candidates[j].CumulativeWeight {
			return candidates[i].CumulativeWeight > candidates[j].CumulativeWeight
		}
		if candidates[i].Slot != candidates[j].Slot {
			return candidates[i].Slot > candidates[j].Slot
		}
		return candidates[i].Hash < candidates[j].Hash
	})
	return candidates
}

func ensureLogger(l ports.Logger) ports.Logger {
	if l == nil {
		return nopLogger{}
	}
	return l
}

type nopLogger struct{}

func (nopLogger) Infof(string, ...any)     {}
func (nopLogger) Warnf(string, ...any)     {}
func (nopLogger) Errorf(string, ...any)    {}
func (nopLogger) Criticalf(string, ...any) {}

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

var ErrEquivocation = errors.New("equivocation detected")
