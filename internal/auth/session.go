package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type SessionStore interface {
	Set(ctx context.Context, id string, userID uint, ttl time.Duration) error
	Get(ctx context.Context, id string) (uint, error)
	Delete(ctx context.Context, id string) error
}

func NewSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
