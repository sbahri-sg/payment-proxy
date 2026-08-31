package secrets

import (
	"encoding/base64"
	"testing"
)

func TestCipherRoundTripAndAAD(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cipher, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := cipher.Encrypt([]byte("secret payload"), []byte("event-1"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := cipher.Decrypt(ciphertext, []byte("event-1"))
	if err != nil || string(plaintext) != "secret payload" {
		t.Fatalf("round trip failed: %q, %v", plaintext, err)
	}
	if _, err := cipher.Decrypt(ciphertext, []byte("event-2")); err == nil {
		t.Fatal("expected authentication failure for different AAD")
	}
}
