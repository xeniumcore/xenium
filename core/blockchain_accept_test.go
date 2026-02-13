package core

import (
	"crypto/ecdsa"
	"strings"
	"testing"

	"xenium/consensus"
	"xenium/domain"
)

func newTestChain(t *testing.T) *Blockchain {
	t.Helper()
	cfg := ChainConfig{
		MaxReorgDepth:        2,
		FinalitySlots:        2,
		MinReorgWeightDeltaP: 10,
		EpochLength:          consensus.SlotsPerEpoch,
		DeterministicPoH:     true,
		PoHSeed:              1,
	}
	return NewBlockchain(cfg, nil, nil)
}

func buildBlock(t *testing.T, bc *Blockchain, validator string, signKey *ecdsa.PrivateKey, txs []domain.Transaction) (domain.Block, domain.Block) {
	t.Helper()
	prev := bc.Blocks[bc.CanonicalTip]
	_, _ = bc.poh.Tick(consensus.TicksPerSlot)
	slot := bc.poh.Slot()
	bc.ensureSnapshotForSlot(slot)
	if validator == "" {
		validator = bc.leaderForSlot(slot)
	}
	nextState, err := consensus.ApplyTransactions(bc.State, txs)
	if err != nil {
		t.Fatalf("apply txs: %v", err)
	}
	block := domain.Block{
		Index:        prev.Index + 1,
		PrevHash:     prev.Hash,
		Slot:         slot,
		Tick:         bc.poh.CurrentTick,
		Validator:    validator,
		TxRoot:       consensus.TxRoot(txs),
		StateRoot:    consensus.StateRoot(nextState),
		PoHHash:      consensus.PoHHashHex(bc.poh.Hash),
		Transactions: txs,
	}
	if err := consensus.SignBlock(signKey, &block); err != nil {
		t.Fatalf("sign block: %v", err)
	}
	return prev, block
}

func TestVerifyBlockOnAcceptRejectsInvalidTx(t *testing.T) {
	bc := newTestChain(t)

	validator, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	receiver, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, validator.PublicKey, validator.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}
	bc.SetBalance(validator.Address, 100)

	tx := domain.Transaction{To: receiver.Address, Amount: 10}
	if err := consensus.SignTransaction(validator.PrivateKey, &tx); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	tx.Signature = "00" // corrupt signature

	prev, block := buildBlock(t, bc, "", validator.PrivateKey, []domain.Transaction{tx})
	if err := bc.verifyBlockOnAccept(prev, block, bc.State); err == nil {
		t.Fatalf("expected invalid tx to be rejected")
	}
}

func TestVerifyBlockOnAcceptRejectsInvalidStateRoot(t *testing.T) {
	bc := newTestChain(t)

	validator, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	receiver, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, validator.PublicKey, validator.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}
	bc.SetBalance(validator.Address, 100)

	tx := domain.Transaction{To: receiver.Address, Amount: 10}
	if err := consensus.SignTransaction(validator.PrivateKey, &tx); err != nil {
		t.Fatalf("sign tx: %v", err)
	}

	prev, block := buildBlock(t, bc, "", validator.PrivateKey, []domain.Transaction{tx})
	block.StateRoot = "badroot"
	if err := consensus.SignBlock(validator.PrivateKey, &block); err != nil {
		t.Fatalf("resign block: %v", err)
	}

	err = bc.verifyBlockOnAccept(prev, block, bc.State)
	if err == nil || !strings.Contains(err.Error(), "invalid state root") {
		t.Fatalf("expected invalid state root error, got: %v", err)
	}
}

func TestVerifyBlockOnAcceptRejectsWrongLeader(t *testing.T) {
	bc := newTestChain(t)

	alice, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	bob, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, alice.PublicKey, alice.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}
	if err := bc.AddValidator("Bob", 100, bob.PublicKey, bob.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}

	prev := bc.Blocks[bc.CanonicalTip]
	_, _ = bc.poh.Tick(consensus.TicksPerSlot)
	slot := bc.poh.Slot()
	bc.ensureSnapshotForSlot(slot)
	leader := bc.leaderForSlot(slot)

	wrongName := "Alice"
	wrongKey := alice.PrivateKey
	if leader == "Alice" {
		wrongName = "Bob"
		wrongKey = bob.PrivateKey
	}

	nextState, err := consensus.ApplyTransactions(bc.State, nil)
	if err != nil {
		t.Fatalf("apply txs: %v", err)
	}
	block := domain.Block{
		Index:        prev.Index + 1,
		PrevHash:     prev.Hash,
		Slot:         slot,
		Tick:         bc.poh.CurrentTick,
		Validator:    wrongName,
		TxRoot:       consensus.TxRoot(nil),
		StateRoot:    consensus.StateRoot(nextState),
		PoHHash:      consensus.PoHHashHex(bc.poh.Hash),
		Transactions: nil,
	}
	if err := consensus.SignBlock(wrongKey, &block); err != nil {
		t.Fatalf("sign block: %v", err)
	}

	err = bc.verifyBlockOnAccept(prev, block, bc.State)
	if err == nil || !strings.Contains(err.Error(), "wrong leader") {
		t.Fatalf("expected wrong leader error, got: %v", err)
	}
}

func TestVerifyBlockOnAcceptRejectsInvalidPrevHash(t *testing.T) {
	bc := newTestChain(t)

	validator, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, validator.PublicKey, validator.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}

	prev, block := buildBlock(t, bc, "", validator.PrivateKey, nil)
	block.PrevHash = "BAD_PREV"
	if err := consensus.SignBlock(validator.PrivateKey, &block); err != nil {
		t.Fatalf("resign block: %v", err)
	}

	err = bc.verifyBlockOnAccept(prev, block, bc.State)
	if err == nil || !strings.Contains(err.Error(), "invalid prev hash") {
		t.Fatalf("expected invalid prev hash error, got: %v", err)
	}
}

func TestVerifyBlockOnAcceptRejectsInvalidBlockSignature(t *testing.T) {
	bc := newTestChain(t)

	validator, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	other, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, validator.PublicKey, validator.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}

	prev, block := buildBlock(t, bc, "", validator.PrivateKey, nil)
	if err := consensus.SignBlock(other.PrivateKey, &block); err != nil {
		t.Fatalf("resign block: %v", err)
	}

	err = bc.verifyBlockOnAccept(prev, block, bc.State)
	if err == nil || !strings.Contains(err.Error(), "invalid block signature") {
		t.Fatalf("expected invalid block signature error, got: %v", err)
	}
}

func TestVerifyBlockOnAcceptRejectsInvalidTxRoot(t *testing.T) {
	bc := newTestChain(t)

	validator, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	receiver, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, validator.PublicKey, validator.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}
	bc.SetBalance(validator.Address, 100)

	tx := domain.Transaction{To: receiver.Address, Amount: 10}
	if err := consensus.SignTransaction(validator.PrivateKey, &tx); err != nil {
		t.Fatalf("sign tx: %v", err)
	}

	prev, block := buildBlock(t, bc, "", validator.PrivateKey, []domain.Transaction{tx})
	block.TxRoot = "badtxroot"
	if err := consensus.SignBlock(validator.PrivateKey, &block); err != nil {
		t.Fatalf("resign block: %v", err)
	}

	err = bc.verifyBlockOnAccept(prev, block, bc.State)
	if err == nil || !strings.Contains(err.Error(), "invalid tx root") {
		t.Fatalf("expected invalid tx root error, got: %v", err)
	}
}

func TestVerifyBlockOnAcceptRejectsExternalForkPrevMismatch(t *testing.T) {
	bc := newTestChain(t)

	alice, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	bob, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	charlie, err := domain.NewWallet()
	if err != nil {
		t.Fatalf("wallet: %v", err)
	}
	if err := bc.AddValidator("Alice", 100, alice.PublicKey, alice.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}
	if err := bc.AddValidator("Bob", 60, bob.PublicKey, bob.PrivateKey); err != nil {
		t.Fatalf("add validator: %v", err)
	}
	bc.SetBalance(alice.Address, 100)
	bc.SetBalance(bob.Address, 50)
	bc.SetBalance(charlie.Address, 30)

	tx1 := domain.Transaction{To: bob.Address, Amount: 10}
	if err := consensus.SignTransaction(alice.PrivateKey, &tx1); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	if err := bc.AddBlock([]domain.Transaction{tx1}); err != nil {
		t.Fatalf("add block: %v", err)
	}

	tx2 := domain.Transaction{To: charlie.Address, Amount: 5}
	if err := consensus.SignTransaction(bob.PrivateKey, &tx2); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	tip := bc.CanonicalTipHash()
	_, err = bc.AddBlockExternal(tip, []domain.Transaction{tx2})
	if err != nil {
		t.Fatalf("add external block: %v", err)
	}

	prev := bc.Blocks[bc.CanonicalTip]
	_, _ = bc.poh.Tick(consensus.TicksPerSlot)
	slot := bc.poh.Slot()
	bc.ensureSnapshotForSlot(slot)
	validator := bc.leaderForSlot(slot)
	v := bc.Validators[validator]
	if v == nil {
		t.Fatalf("missing leader validator")
	}
	stateAtTip, err := bc.stateAtTip(tip)
	if err != nil {
		t.Fatalf("state at tip: %v", err)
	}
	nextState, err := consensus.ApplyTransactions(stateAtTip, nil)
	if err != nil {
		t.Fatalf("apply txs: %v", err)
	}
	block := domain.Block{
		Index:        prev.Index + 1,
		PrevHash:     tip, // wrong parent for current tip
		Slot:         slot,
		Tick:         bc.poh.CurrentTick,
		Validator:    validator,
		TxRoot:       consensus.TxRoot(nil),
		StateRoot:    consensus.StateRoot(nextState),
		PoHHash:      consensus.PoHHashHex(bc.poh.Hash),
		Transactions: nil,
	}
	if err := consensus.SignBlock(v.PrivKey, &block); err != nil {
		t.Fatalf("sign block: %v", err)
	}

	err = bc.verifyBlockOnAccept(prev, block, bc.State)
	if err == nil || !strings.Contains(err.Error(), "invalid prev hash") {
		t.Fatalf("expected invalid prev hash error, got: %v", err)
	}
}
