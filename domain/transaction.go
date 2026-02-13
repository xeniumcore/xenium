package domain

type Transaction struct {
	From      string
	To        string
	Amount    int
	PubKey    string
	Signature string
}
