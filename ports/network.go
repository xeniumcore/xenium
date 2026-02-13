package ports

import "xenium/domain"

type Network interface {
	BroadcastBlock(block domain.Block) error
}
