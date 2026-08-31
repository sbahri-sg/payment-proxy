package servicekeys

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"
)

type memoryRepository struct {
	items    []APIKey
	hashes   map[string]bool
	hashByID map[string]string
}

func (r *memoryRepository) List(context.Context) ([]APIKey, error) {
	return append([]APIKey(nil), r.items...), nil
}

func (r *memoryRepository) Create(_ context.Context, input CreateInput) (APIKey, error) {
	if r.hashes == nil {
		r.hashes = map[string]bool{}
		r.hashByID = map[string]string{}
	}
	r.hashes[string(input.KeyHash)] = true
	r.hashByID[input.ID] = string(input.KeyHash)
	item := APIKey{ID: input.ID, Name: input.Name, KeyHint: keyHint(input.KeyPrefix, input.KeyLast4), Scopes: input.Scopes, Status: StatusActive, CreatedBy: input.Actor, CreatedAt: time.Now()}
	r.items = append(r.items, item)
	return item, nil
}

func (r *memoryRepository) Revoke(_ context.Context, id, actor, _ string) (APIKey, error) {
	for index := range r.items {
		if r.items[index].ID == id && r.items[index].Status == StatusActive {
			now := time.Now()
			r.items[index].Status = StatusRevoked
			r.items[index].RevokedBy = actor
			r.items[index].RevokedAt = &now
			delete(r.hashes, r.hashByID[id])
			return r.items[index], nil
		}
	}
	return APIKey{}, ErrNotFound
}

func (r *memoryRepository) Authenticate(_ context.Context, hash []byte) (bool, error) {
	return r.hashes[string(hash)], nil
}

func TestGenerateReturnsOneTimeFullAccessKeyWithoutPersistingSecret(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository)
	generated, err := service.Generate(context.Background(), "Emisell Backend Production", "dashboard:operator", "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(generated.Secret, KeyNamespace) || len(generated.Secret) < 40 {
		t.Fatalf("invalid generated key %q", generated.Secret)
	}
	if generated.APIKey.Name != "Emisell Backend Production" || len(generated.APIKey.Scopes) != 1 || generated.APIKey.Scopes[0] != ScopeFull {
		t.Fatalf("unexpected generated metadata: %#v", generated.APIKey)
	}
	if strings.Contains(generated.APIKey.KeyHint, generated.Secret) || !strings.Contains(generated.APIKey.KeyHint, "••••") {
		t.Fatalf("secret was exposed in metadata: %q", generated.APIKey.KeyHint)
	}
	hash := sha256.Sum256([]byte(generated.Secret))
	if !repository.hashes[string(hash[:])] {
		t.Fatal("generated key hash was not persisted")
	}
	for stored := range repository.hashes {
		if bytes.Contains([]byte(stored), []byte(generated.Secret)) {
			t.Fatal("plaintext key was persisted")
		}
	}
	valid, err := service.Authenticate(context.Background(), generated.Secret)
	if err != nil || !valid {
		t.Fatalf("generated key did not authenticate: %v", err)
	}
}

func TestRevokeDisablesKeyMetadata(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository)
	generated, err := service.Generate(context.Background(), "Emisell Backend", "operator", "req_1")
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.Revoke(context.Background(), generated.APIKey.ID, "operator", "req_2")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusRevoked || item.RevokedAt == nil {
		t.Fatalf("key was not revoked: %#v", item)
	}
	valid, err := service.Authenticate(context.Background(), generated.Secret)
	if err != nil || valid {
		t.Fatalf("revoked key still authenticated: valid=%t err=%v", valid, err)
	}
}

func TestAuthenticateRejectsMalformedKeyWithoutRepositoryLookup(t *testing.T) {
	repository := &countingRepository{memoryRepository: memoryRepository{}}
	service := NewService(repository)
	for _, token := range []string{"", "epk_short", "other_" + strings.Repeat("x", 43), "epk_" + strings.Repeat("x", 10000)} {
		valid, err := service.Authenticate(context.Background(), token)
		if err != nil || valid {
			t.Fatalf("malformed token authenticated: valid=%t err=%v", valid, err)
		}
	}
	if repository.authenticateCalls != 0 {
		t.Fatalf("malformed tokens reached the repository %d times", repository.authenticateCalls)
	}
}

type countingRepository struct {
	memoryRepository
	authenticateCalls int
}

func (r *countingRepository) Authenticate(ctx context.Context, hash []byte) (bool, error) {
	r.authenticateCalls++
	return r.memoryRepository.Authenticate(ctx, hash)
}
