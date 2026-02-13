package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"xenium/domain"
)

func HashBlock(index uint64, prevHash string, slot uint64, tick uint64, validator string, txRoot string, stateRoot string, pohHash string) string {
	var b strings.Builder
	b.Grow(200)
	b.WriteString(strconv.FormatUint(index, 10))
	b.WriteString("|")
	b.WriteString(prevHash)
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(slot, 10))
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(tick, 10))
	b.WriteString("|")
	b.WriteString(validator)
	b.WriteString("|")
	b.WriteString(txRoot)
	b.WriteString("|")
	b.WriteString(stateRoot)
	b.WriteString("|")
	b.WriteString(pohHash)

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func HashTx(tx domain.Transaction) []byte {
	var b strings.Builder
	b.Grow(128)
	b.WriteString(tx.From)
	b.WriteString("|")
	b.WriteString(tx.To)
	b.WriteString("|")
	b.WriteString(strconv.Itoa(tx.Amount))
	b.WriteString("|")
	b.WriteString(strconv.Itoa(tx.Fee))
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(tx.Nonce, 10))
	b.WriteString("|")
	b.WriteString(tx.PubKey)
	sum := sha256.Sum256([]byte(b.String()))
	return sum[:]
}

func TxRoot(txs []domain.Transaction) string {
	if len(txs) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}
	h := sha256.New()
	for i := range txs {
		h.Write(HashTx(txs[i]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func StateRoot(state map[string]domain.Account) string {
	if len(state) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(":"))
		h.Write([]byte(strconv.Itoa(state[k].Balance)))
		h.Write([]byte("|"))
		h.Write([]byte(strconv.FormatUint(state[k].Nonce, 10)))
		h.Write([]byte(";"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func SignTransaction(priv *ecdsa.PrivateKey, tx *domain.Transaction) error {
	if tx == nil {
		return errors.New("nil transaction")
	}
	pubBytes := elliptic.Marshal(priv.Curve, priv.PublicKey.X, priv.PublicKey.Y)
	tx.PubKey = hex.EncodeToString(pubBytes)
	addr, err := domain.AddressFromPubKey(tx.PubKey)
	if err != nil {
		return err
	}
	tx.From = addr
	digest := HashTx(*tx)
	tx.Hash = hex.EncodeToString(digest)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest)
	if err != nil {
		return err
	}
	tx.Signature = hex.EncodeToString(sig)
	return nil
}

func VerifyTransactionSignature(tx domain.Transaction) error {
	if tx.PubKey == "" || tx.Signature == "" {
		return errors.New("missing pubkey or signature")
	}
	pubBytes, err := hex.DecodeString(tx.PubKey)
	if err != nil {
		return err
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), pubBytes)
	if x == nil || y == nil {
		return errors.New("invalid pubkey")
	}
	addr, err := domain.AddressFromPubKey(tx.PubKey)
	if err != nil {
		return err
	}
	if tx.From != addr {
		return errors.New("from address does not match pubkey")
	}
	sigBytes, err := hex.DecodeString(tx.Signature)
	if err != nil {
		return err
	}
	digest := HashTx(tx)
	hashHex := hex.EncodeToString(digest)
	if tx.Hash == "" {
		return errors.New("missing tx hash")
	}
	if tx.Hash != hashHex {
		return errors.New("tx hash mismatch")
	}
	if !ecdsa.VerifyASN1(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest, sigBytes) {
		return errors.New("invalid signature")
	}
	return nil
}

func SignBlock(priv *ecdsa.PrivateKey, block *domain.Block) error {
	if block == nil {
		return errors.New("nil block")
	}
	if priv == nil {
		return errors.New("missing validator private key")
	}
	digest := HashBlock(block.Index, block.PrevHash, block.Slot, block.Tick, block.Validator, block.TxRoot, block.StateRoot, block.PoHHash)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, []byte(digest))
	if err != nil {
		return err
	}
	block.Signature = sig
	block.Hash = digest
	return nil
}

func VerifyBlockSignature(block domain.Block, pubKeyHex string) error {
	if pubKeyHex == "" {
		return errors.New("missing validator pubkey")
	}
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return err
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), pubBytes)
	if x == nil || y == nil {
		return errors.New("invalid validator pubkey")
	}
	if len(block.Signature) == 0 {
		return errors.New("missing block signature")
	}
	digest := HashBlock(block.Index, block.PrevHash, block.Slot, block.Tick, block.Validator, block.TxRoot, block.StateRoot, block.PoHHash)
	if !ecdsa.VerifyASN1(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, []byte(digest), block.Signature) {
		return errors.New("invalid block signature")
	}
	if block.Hash != digest {
		return errors.New("invalid block hash")
	}
	return nil
}
