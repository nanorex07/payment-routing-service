package service

import (
	"crypto/rand"
	"encoding/hex"
)

type IDGenerator interface {
	NewID() string
}

type CryptoIDGenerator struct{}

func (CryptoIDGenerator) NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "txn_fallback"
	}
	return "txn_" + hex.EncodeToString(b[:])
}
