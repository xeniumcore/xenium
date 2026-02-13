package ports

import "xenium/domain"

type Signer interface {
	SignTx(tx *domain.Transaction) error
	SignBlock(block *domain.Block) error
}
