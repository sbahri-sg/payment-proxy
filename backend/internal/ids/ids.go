package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func New(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}
