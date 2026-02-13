package ports

import "xenium/domain"

type BlockStore interface {
	SaveBlock(block domain.Block) error
	GetBlockByHash(hash string) (domain.Block, bool)
	GetBlockByHeight(height uint64) (domain.Block, bool)
	GetTip() (domain.Block, bool)
	GetRange(startHeight uint64, endHeight uint64) ([]domain.Block, error)
}

type SnapshotStore interface {
	SaveEpochSnapshot(epoch uint64, stateRoot string, validatorSet map[string]uint64) error
	LoadLatestSnapshot() (epoch uint64, stateRoot string, validatorSet map[string]uint64, ok bool, err error)
	LoadSnapshotByEpoch(epoch uint64) (stateRoot string, validatorSet map[string]uint64, ok bool, err error)
}
