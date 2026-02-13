package main

import (
	"fmt"

	"xenium/adapters"
	"xenium/app"
	"xenium/consensus"
	"xenium/domain"
)

func main() {
	node := app.NewNode(app.DefaultConfig(), adapters.SystemClock{}, adapters.StdLogger{})

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

	forkScore := xenium.ScoreTip(forkHash)
	afterTip := xenium.CanonicalTipHash()
	afterScore := xenium.ScoreTip(afterTip)
	newChain := xenium.Chain
	reorgDepth := computeReorgDepth(oldChain, newChain)
	reorged := beforeTip != afterTip

	fmt.Println("[Before]")
	fmt.Printf("Tip Hash: %s\n", beforeTip)
	fmt.Printf("Slot: %d\n", beforeScore.Slot)
	fmt.Printf("Weight: %d\n", beforeScore.CumulativeWeight)
	fmt.Println()

	fmt.Println("[Insert]")
	fmt.Printf("Fork Block: %s\n", forkHash)
	fmt.Printf("Parent: %s\n", parentHash)
	fmt.Printf("Slot: %d\n", forkScore.Slot)
	fmt.Printf("Weight: %d\n", forkScore.CumulativeWeight)
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
