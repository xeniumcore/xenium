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
	if snapshotStore != nil {
		_, _, _, _, _ = snapshotStore.LoadLatestSnapshot()
	}
	tip, ok := blockStore.GetTip()
	if !ok {
		return nil
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
