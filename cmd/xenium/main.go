package main

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"sort"
	"strings"

	"xenium/adapters"
	"xenium/app"
	"xenium/consensus"
	"xenium/core"
	"xenium/domain"
)

const (
	colorReset  = "\x1b[0m"
	colorGreen  = "\x1b[32m"
	colorRed    = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorCyan   = "\x1b[36m"
)

func main() {
	cfg := app.DefaultConfig()
	cfg.Chain.DeterministicPoH = true
	cfg.Chain.PoHSeed = 1
	node := app.NewNode(cfg, adapters.SystemClock{}, adapters.StdLogger{})

	xenium := node.Chain

	alice, err := domain.NewWallet()
	if err != nil {
		panic(err)
	}
	bob, err := domain.NewWallet()
	if err != nil {
		panic(err)
	}
	charlie, err := domain.NewWallet()
	if err != nil {
		panic(err)
	}

	if err := xenium.AddValidator("Alice", 100, alice.PublicKey, alice.PrivateKey); err != nil {
		panic(err)
	}
	if err := xenium.AddValidator("Bob", 60, bob.PublicKey, bob.PrivateKey); err != nil {
		panic(err)
	}
	if err := xenium.AddValidator("Charlie", 40, charlie.PublicKey, charlie.PrivateKey); err != nil {
		panic(err)
	}

	xenium.SetBalance(alice.Address, 200)
	xenium.SetBalance(bob.Address, 100)
	xenium.SetBalance(charlie.Address, 80)

	printStakeSummary("Stake (initial)", xenium)

	tx1 := domain.Transaction{To: bob.Address, Amount: 50}
	if err := consensus.SignTransaction(alice.PrivateKey, &tx1); err != nil {
		panic(err)
	}
	if err := xenium.AddBlock([]domain.Transaction{tx1}); err != nil {
		panic(err)
	}

	tx2 := domain.Transaction{To: charlie.Address, Amount: 20}
	if err := consensus.SignTransaction(bob.PrivateKey, &tx2); err != nil {
		panic(err)
	}
	if err := xenium.AddBlock([]domain.Transaction{tx2}); err != nil {
		panic(err)
	}

	// Fork simulation: build on block 1 instead of tip.
	if len(xenium.Chain) < 3 {
		panic("chain too short for fork simulation")
	}
	beforeTip := xenium.CanonicalTipHash()
	beforeScore := xenium.ScoreTip(beforeTip)
	oldChain := append([]domain.Block(nil), xenium.Chain...)

	parentHash := xenium.Chain[1].Hash
	tx3 := domain.Transaction{To: alice.Address, Amount: 10}
	if err := consensus.SignTransaction(charlie.PrivateKey, &tx3); err != nil {
		panic(err)
	}
	forkHash, err := xenium.AddBlockExternal(parentHash, []domain.Transaction{tx3})
	if err != nil {
		panic(err)
	}

	// Extend the fork to ensure higher cumulative weight.
	tx4 := domain.Transaction{To: bob.Address, Amount: 5}
	if err := consensus.SignTransaction(alice.PrivateKey, &tx4); err != nil {
		panic(err)
	}
	forkHash, err = xenium.AddBlockExternal(forkHash, []domain.Transaction{tx4})
	if err != nil {
		panic(err)
	}

	// Additional forks for multi-candidate evaluation.
	_, err = xenium.AddBlockExternal(parentHash, []domain.Transaction{makeTx(bob.PrivateKey, alice.Address, 2)})
	if err != nil {
		panic(err)
	}
	forkB, err := xenium.AddBlockExternal(parentHash, []domain.Transaction{makeTx(alice.PrivateKey, bob.Address, 3)})
	if err != nil {
		panic(err)
	}
	_, err = xenium.AddBlockExternal(forkB, []domain.Transaction{makeTx(bob.PrivateKey, charlie.Address, 1)})
	if err != nil {
		panic(err)
	}

	forkScore := xenium.ScoreTip(forkHash)
	afterTip := xenium.CanonicalTipHash()
	afterScore := xenium.ScoreTip(afterTip)
	newChain := xenium.Chain
	reorgDepth := computeReorgDepth(oldChain, newChain)
	reorged := beforeTip != afterTip
	requiredDelta, actualDelta := xenium.WeightDeltaRequired(beforeScore.CumulativeWeight, forkScore.CumulativeWeight)
	deltaPass := actualDelta >= requiredDelta

	beforeSnap := xenium.GetEpochSnapshot(beforeScore.Slot)
	forkSnap := xenium.GetEpochSnapshot(forkScore.Slot)

	fmt.Println("[Before]")
	fmt.Printf("Tip Hash: %s\n", beforeTip)
	fmt.Printf("Slot: %d\n", beforeScore.Slot)
	fmt.Printf("Weight: %d\n", beforeScore.CumulativeWeight)
	fmt.Printf("Epoch: %d  ActiveStake: %d  Validators: %d\n", beforeSnap.Epoch, beforeSnap.TotalStake, len(beforeSnap.Validators))
	fmt.Println()

	fmt.Println("[Insert]")
	fmt.Printf("Fork Block: %s\n", forkHash)
	fmt.Printf("Parent: %s\n", parentHash)
	fmt.Printf("Slot: %d\n", forkScore.Slot)
	fmt.Printf("Weight: %d\n", forkScore.CumulativeWeight)
	fmt.Printf("Epoch: %d  ActiveStake: %d  Validators: %d\n", forkSnap.Epoch, forkSnap.TotalStake, len(forkSnap.Validators))
	fmt.Println()

	fmt.Println("[Reorg]")
	if reorged {
		fmt.Printf("Depth: %d\n", reorgDepth)
		fmt.Printf("Old Tip: %s\n", beforeTip)
		fmt.Printf("New Tip: %s\n", afterTip)
	} else {
		fmt.Println("No reorg (canonical unchanged)")
		fmt.Printf("Tip: %s\n", afterTip)
		fmt.Printf("Rejected Fork: %s\n", forkHash)
	}
	fmt.Println()

	fmt.Println("[After]")
	fmt.Printf("Tip Hash: %s\n", afterTip)
	fmt.Printf("Slot: %d\n", afterScore.Slot)
	fmt.Printf("Weight: %d\n", afterScore.CumulativeWeight)
	fmt.Println()

	fmt.Println("Canonical Chain:")
	for _, block := range newChain {
		fmt.Printf("Slot %d -> %s\n", block.Slot, block.Hash)
	}
	fmt.Println()
	fmt.Println("=== Fork Candidate Ranking ===")
	fmt.Printf("Canonical: slot=%d weight=%d hash=%s\n", beforeScore.Slot, beforeScore.CumulativeWeight, beforeTip)
	fmt.Printf("Fork:      slot=%d weight=%d hash=%s\n", forkScore.Slot, forkScore.CumulativeWeight, forkHash)
	fmt.Printf("Delta:     required=%d actual=%d pass=%t\n", requiredDelta, actualDelta, deltaPass)
	fmt.Println("==============================")
	fmt.Println("=== Fork Candidates (All Tips) ===")
	candidates := xenium.GetForkCandidates()
	for _, c := range candidates {
		req, act := xenium.WeightDeltaRequired(beforeScore.CumulativeWeight, c.CumulativeWeight)
		pass := act >= req
		cue := "FAIL"
		if pass {
			cue = "PASS"
		}
		snap := xenium.GetEpochSnapshot(c.Slot)
		cueColor := colorRed
		if pass {
			cueColor = colorGreen
		}
		markColor := " "
		if c.Hash == xenium.CanonicalTipHash() {
			markColor = colorCyan + "*" + colorReset
		}
		fmt.Printf("[%s] tip=%s slot=%d weight=%d parent=%s epoch=%d activeStake=%d validators=%d deltaRequired=%d deltaActual=%d %s%s%s\n",
			markColor, c.Hash, c.Slot, c.CumulativeWeight, c.Parent, snap.Epoch, snap.TotalStake, len(snap.Validators), req, act, cueColor, cue, colorReset)
	}
	fmt.Println("==================================")
	fmt.Println("=== Reorg Metrics ===")
	stats := xenium.GetReorgStats()
	fmt.Printf("INFO: %d\n", stats.Info)
	fmt.Printf("WARN: %d\n", stats.Warn)
	fmt.Printf("ERROR: %d\n", stats.Error)
	fmt.Printf("CRITICAL: %d\n", stats.Critical)
	fmt.Println("=====================")
	fmt.Println()
	fmt.Println("=== Missed Slot Stats ===")
	for _, s := range xenium.GetValidatorSummaries() {
		fmt.Printf("%-8s -> Produced: %-3d Missed: %-3d MissRate: %-5.1f%% Slashed: %-5v JailedUntil: %-3d\n",
			s.Name, s.Produced, s.Missed, s.MissRate*100, s.Slashed, s.JailedUntil)
	}
	fmt.Println("=========================")

	for _, block := range xenium.Chain {
		fmt.Printf("Index: %d\n", block.Index)
		fmt.Printf("Hash: %s\n", block.Hash)
		fmt.Printf("PrevHash: %s\n", block.PrevHash)
		fmt.Printf("Validator: %s\n", block.Validator)
		fmt.Printf("Slot: %d\n", block.Slot)
		fmt.Printf("Tick: %d\n", block.Tick)
		fmt.Printf("PoHHash: %s\n", block.PoHHash)
		fmt.Printf("TxRoot: %s\n", block.TxRoot)
		fmt.Printf("StateRoot: %s\n", block.StateRoot)
		fmt.Println("-----")
	}

	printStakeSummary("Stake (final)", xenium)
	writeEpochSnapshotCSV("epoch_snapshots.csv", xenium)
	printForkTimeline(xenium)
}

func computeReorgDepth(oldChain []domain.Block, newChain []domain.Block) int {
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
	return len(oldChain) - diverge
}

func printStakeSummary(label string, chain *core.Blockchain) {
	fmt.Println(label)
	names := make([]string, 0, len(chain.Validators))
	for name := range chain.Validators {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("%s: %d\n", name, chain.Validators[name].Stake)
	}
	fmt.Println()
}

func writeEpochSnapshotCSV(path string, chain *core.Blockchain) {
	snaps := chain.GetAllEpochSnapshots()
	var b strings.Builder
	b.WriteString("epoch,total_stake,validator,stake\n")
	for _, s := range snaps {
		keys := make([]string, 0, len(s.Validators))
		for k := range s.Validators {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			b.WriteString(fmt.Sprintf("%d,%d,,\n", s.Epoch, s.TotalStake))
			continue
		}
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("%d,%d,%s,%d\n", s.Epoch, s.TotalStake, k, s.Validators[k]))
		}
	}
	_ = os.WriteFile(path, []byte(b.String()), 0644)
	fmt.Printf("Wrote epoch snapshot CSV: %s\n", path)
}

func printForkTimeline(chain *core.Blockchain) {
	fmt.Println("Fork timeline (canonical + tips)")
	candidates := chain.GetForkCandidates()
	tipsBySlot := make(map[uint64][]string)
	for _, c := range candidates {
		tipsBySlot[c.Slot] = append(tipsBySlot[c.Slot], c.Hash)
	}
	for _, tips := range tipsBySlot {
		sort.Strings(tips)
	}
	for _, block := range chain.Chain {
		line := fmt.Sprintf("slot %d canonical=%s", block.Slot, block.Hash)
		if tips, ok := tipsBySlot[block.Slot]; ok && len(tips) > 0 {
			line += " tips=" + strings.Join(tips, ",")
		}
		fmt.Println(line)
	}
	fmt.Println()
}

func makeTx(priv *ecdsa.PrivateKey, to string, amount int) domain.Transaction {
	tx := domain.Transaction{To: to, Amount: amount}
	if err := consensus.SignTransaction(priv, &tx); err != nil {
		panic(err)
	}
	return tx
}
