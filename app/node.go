package app

import (
	"xenium/adapters"
	"xenium/core"
	"xenium/ports"
)

type Node struct {
	Chain *core.Blockchain
}

func NewNode(cfg Config, clock ports.Clock, logger ports.Logger) (*Node, error) {
	chain := core.NewBlockchain(cfg.Chain, clock, logger)
	node := &Node{Chain: chain}

	if cfg.DataDir != "" {
		blockStore, err := adapters.NewFileBlockStore(cfg.DataDir)
		if err != nil {
			return nil, err
		}
		snapshotStore, err := adapters.NewFileSnapshotStore(cfg.DataDir)
		if err != nil {
			return nil, err
		}
		chain.SetStorage(blockStore, snapshotStore)
		if err := chain.RestoreFromStorage(blockStore, snapshotStore); err != nil {
			return nil, err
		}
	}

	return node, nil
}
