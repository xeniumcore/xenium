package domain

type Block struct {
	Index        uint64
	PrevHash     string
	Slot         uint64
	Tick         uint64
	Validator    string
	TxRoot       string
	StateRoot    string
	PoHHash      string
	Signature    []byte
	Hash         string
	Transactions []Transaction
}
