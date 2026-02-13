package adapters

import "xenium/domain"

type MemoryBlockStore struct {
	blocks map[string]domain.Block
}

func NewMemoryBlockStore() *MemoryBlockStore {
	return &MemoryBlockStore{blocks: make(map[string]domain.Block)}
}

func (s *MemoryBlockStore) Get(hash string) (domain.Block, bool) {
	b, ok := s.blocks[hash]
	return b, ok
}

func (s *MemoryBlockStore) Put(block domain.Block) {
	s.blocks[block.Hash] = block
}
