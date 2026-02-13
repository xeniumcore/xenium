package domain

type Transaction struct {
	From      string
	To        string
	Amount    int
	Fee       int
	Nonce     uint64
	PubKey    string
	Signature string
	Hash      string
}
