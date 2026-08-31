package connector

import (
	"strings"
	"testing"
)

func TestManifestValidatesExecutableDigest(t *testing.T) {
	manifest := Manifest{
		Code: "test", Name: "Test", Version: "v1", Runtime: "isolated_container",
		ExecutableSHA256: strings.Repeat("a", 64),
		Operations:       []Operation{OperationCreatePayment},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("valid executable digest was rejected: %v", err)
	}
	manifest.ExecutableSHA256 = "not-a-digest"
	if err := manifest.Validate(); err == nil {
		t.Fatal("invalid executable digest was accepted")
	}
}
