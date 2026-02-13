package ports

import "xenium/domain"

type BlockStore interface {
	Get(hash string) (domain.Block, bool)
	Put(block domain.Block)
}
