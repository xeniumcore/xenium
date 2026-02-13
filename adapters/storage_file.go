package adapters

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"xenium/domain"
)

type FileBlockStore struct {
	dir          string
	blocksPath   string
	indexPath    string
	blocks       map[string]domain.Block
	heightToHash map[uint64]string
	tipHash      string
	tipHeight    uint64
	mu           sync.RWMutex
}

type blockIndex struct {
	HeightToHash map[uint64]string `json:"height_to_hash"`
	TipHash      string           `json:"tip_hash"`
	TipHeight    uint64           `json:"tip_height"`
}

func NewFileBlockStore(dir string) (*FileBlockStore, error) {
	if dir == "" {
		return nil, errors.New("data dir required")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	blocksPath := filepath.Join(dir, "blocks.jsonl")
	indexPath := filepath.Join(dir, "index.json")

	store := &FileBlockStore{
		dir:          dir,
		blocksPath:   blocksPath,
		indexPath:    indexPath,
		blocks:       make(map[string]domain.Block),
		heightToHash: make(map[uint64]string),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileBlockStore) SaveBlock(block domain.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.appendBlock(block); err != nil {
		return err
	}
	s.blocks[block.Hash] = block
	s.heightToHash[block.Index] = block.Hash
	if block.Index >= s.tipHeight {
		s.tipHeight = block.Index
		s.tipHash = block.Hash
	}
	return s.writeIndex()
}

func (s *FileBlockStore) GetBlockByHash(hash string) (domain.Block, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blocks[hash]
	return b, ok
}

func (s *FileBlockStore) GetBlockByHeight(height uint64) (domain.Block, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hash, ok := s.heightToHash[height]
	if !ok {
		return domain.Block{}, false
	}
	b, ok := s.blocks[hash]
	return b, ok
}

func (s *FileBlockStore) GetTip() (domain.Block, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.tipHash == "" {
		return domain.Block{}, false
	}
	b, ok := s.blocks[s.tipHash]
	return b, ok
}

func (s *FileBlockStore) GetRange(startHeight uint64, endHeight uint64) ([]domain.Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if endHeight < startHeight {
		return nil, nil
	}
	out := make([]domain.Block, 0, endHeight-startHeight+1)
	for h := startHeight; h <= endHeight; h++ {
		hash, ok := s.heightToHash[h]
		if !ok {
			return nil, fmt.Errorf("missing block at height %d", h)
		}
		b, ok := s.blocks[hash]
		if !ok {
			return nil, fmt.Errorf("missing block hash %s", hash)
		}
		out = append(out, b)
	}
	return out, nil
}

func (s *FileBlockStore) load() error {
	if err := s.loadIndex(); err != nil {
		return err
	}
	if err := s.loadBlocks(); err != nil {
		return err
	}
	if len(s.heightToHash) == 0 {
		if err := s.rebuildIndex(); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileBlockStore) loadIndex() error {
	data, err := os.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var idx blockIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	s.heightToHash = idx.HeightToHash
	s.tipHash = idx.TipHash
	s.tipHeight = idx.TipHeight
	if s.heightToHash == nil {
		s.heightToHash = make(map[uint64]string)
	}
	return nil
}

func (s *FileBlockStore) loadBlocks() error {
	f, err := os.Open(s.blocksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		var b domain.Block
		if err := json.Unmarshal(line, &b); err != nil {
			return err
		}
		s.blocks[b.Hash] = b
	}
	return scanner.Err()
}

func (s *FileBlockStore) rebuildIndex() error {
	if len(s.blocks) == 0 {
		return nil
	}
	s.heightToHash = make(map[uint64]string)
	var maxHeight uint64
	var tipHash string
	for _, b := range s.blocks {
		s.heightToHash[b.Index] = b.Hash
		if b.Index >= maxHeight {
			maxHeight = b.Index
			tipHash = b.Hash
		}
	}
	s.tipHeight = maxHeight
	s.tipHash = tipHash
	return s.writeIndex()
}

func (s *FileBlockStore) appendBlock(block domain.Block) error {
	f, err := os.OpenFile(s.blocksPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(block)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *FileBlockStore) writeIndex() error {
	idx := blockIndex{
		HeightToHash: s.heightToHash,
		TipHash:      s.tipHash,
		TipHeight:    s.tipHeight,
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	tmp := s.indexPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath)
}

type FileSnapshotStore struct {
	dir string
	mu  sync.RWMutex
}

type snapshotFile struct {
	Epoch        uint64            `json:"epoch"`
	StateRoot    string            `json:"state_root"`
	ValidatorSet map[string]uint64 `json:"validator_set"`
}

func NewFileSnapshotStore(dir string) (*FileSnapshotStore, error) {
	if dir == "" {
		return nil, errors.New("data dir required")
	}
	snapDir := filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		return nil, err
	}
	return &FileSnapshotStore{dir: snapDir}, nil
}

func (s *FileSnapshotStore) SaveEpochSnapshot(epoch uint64, stateRoot string, validatorSet map[string]uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, fmt.Sprintf("epoch_%d.json", epoch))
	data, err := json.Marshal(snapshotFile{
		Epoch:        epoch,
		StateRoot:    stateRoot,
		ValidatorSet: validatorSet,
	})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *FileSnapshotStore) LoadLatestSnapshot() (epoch uint64, stateRoot string, validatorSet map[string]uint64, ok bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, "", nil, false, nil
		}
		return 0, "", nil, false, err
	}
	var latestEpoch uint64
	var latestPath string
	for _, e := range entries {
		var ep uint64
		if _, err := fmt.Sscanf(e.Name(), "epoch_%d.json", &ep); err == nil {
			if ep >= latestEpoch {
				latestEpoch = ep
				latestPath = filepath.Join(s.dir, e.Name())
			}
		}
	}
	if latestPath == "" {
		return 0, "", nil, false, nil
	}
	sf, err := s.loadSnapshotFile(latestPath)
	if err != nil {
		return 0, "", nil, false, err
	}
	return sf.Epoch, sf.StateRoot, sf.ValidatorSet, true, nil
}

func (s *FileSnapshotStore) LoadSnapshotByEpoch(epoch uint64) (stateRoot string, validatorSet map[string]uint64, ok bool, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, fmt.Sprintf("epoch_%d.json", epoch))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil, false, nil
		}
		return "", nil, false, err
	}
	sf, err := s.loadSnapshotFile(path)
	if err != nil {
		return "", nil, false, err
	}
	return sf.StateRoot, sf.ValidatorSet, true, nil
}

func (s *FileSnapshotStore) loadSnapshotFile(path string) (*snapshotFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sf snapshotFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	if sf.ValidatorSet == nil {
		sf.ValidatorSet = make(map[string]uint64)
	}
	return &sf, nil
}
