package core

import (
	"errors"

	"xenium/domain"
	"xenium/ports"
)

func (bc *Blockchain) RestoreFromStorage(blockStore ports.BlockStore, snapshotStore ports.SnapshotStore) error {
	if blockStore == nil {
		return errors.New("block store required")
	}
	tip, ok := blockStore.GetTip()
	if !ok {
		// Fresh store, persist genesis so the node can restart safely.
		if len(bc.Chain) > 0 {
			_ = blockStore.SaveBlock(bc.Chain[0])
		}
		return nil
	}
	if snapshotStore != nil {
		epoch, stateRoot, validatorSet, ok, err := snapshotStore.LoadLatestSnapshot()
		if err != nil {
			return err
		}
		if ok {
			snap := &EpochSnapshot{
				Epoch:      epoch,
				Validators: validatorSet,
			}
			for _, stake := range validatorSet {
				snap.TotalStake += stake
			}
			bc.snapshots[epoch] = snap
			bc.currentEpoch = epoch
			_ = stateRoot // reserved for Phase 3 (state replay from snapshot)
		}
	}
	blocks, err := blockStore.GetRange(0, tip.Index)
	if err != nil {
		return err
	}

	bc.Blocks = make(map[string]domain.Block)
	bc.Parents = make(map[string]string)
	for _, b := range blocks {
		bc.insertBlock(b)
	}
	bc.CanonicalTip = tip.Hash
	bc.rebuildCanonicalChain()
	bc.updateFinality()
	return nil
}
