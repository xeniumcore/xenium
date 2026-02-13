package core

import (
	"errors"
	"sort"
	"sync"

	"xenium/consensus"
	"xenium/domain"
)

type Mempool struct {
	mu     sync.Mutex
	byHash map[string]domain.Transaction
	list   []domain.Transaction
}

func NewMempool() *Mempool {
	return &Mempool{
		byHash: make(map[string]domain.Transaction),
	}
}

func (m *Mempool) Add(tx domain.Transaction) error {
	if tx.Hash == "" {
		return errors.New("missing tx hash")
	}
	if err := consensus.VerifyTransactionSignature(tx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byHash[tx.Hash]; ok {
		return errors.New("duplicate tx")
	}
	m.byHash[tx.Hash] = tx
	m.list = append(m.list, tx)
	sort.Slice(m.list, func(i, j int) bool {
		if m.list[i].Fee != m.list[j].Fee {
			return m.list[i].Fee > m.list[j].Fee
		}
		if m.list[i].From != m.list[j].From {
			return m.list[i].From < m.list[j].From
		}
		return m.list[i].Nonce < m.list[j].Nonce
	})
	return nil
}

func (m *Mempool) PopForBlock(state map[string]domain.Account, max int, producer string) []domain.Transaction {
	if max <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.list) == 0 {
		return nil
	}

	out := make([]domain.Transaction, 0, max)
	nextState := copyState(state)
	remaining := m.list[:0]

	for _, tx := range m.list {
		if len(out) >= max {
			remaining = append(remaining, tx)
			continue
		}
		applied, err := consensus.ApplyTransactions(nextState, []domain.Transaction{tx}, producer)
		if err != nil {
			remaining = append(remaining, tx)
			continue
		}
		out = append(out, tx)
		nextState = applied
		delete(m.byHash, tx.Hash)
	}

	m.list = remaining
	return out
}

func copyState(state map[string]domain.Account) map[string]domain.Account {
	next := make(map[string]domain.Account, len(state))
	for k, v := range state {
		next[k] = v
	}
	return next
}
