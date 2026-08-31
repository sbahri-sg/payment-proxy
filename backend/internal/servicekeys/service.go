package servicekeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/ids"
)

const randomKeyBytes = 32

const encodedKeyLength = len(KeyNamespace) + 43

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context) ([]APIKey, error) {
	return s.repository.List(ctx)
}

func (s *Service) Generate(ctx context.Context, name, actor, requestID string) (GeneratedAPIKey, error) {
	randomValue := make([]byte, randomKeyBytes)
	if _, err := rand.Read(randomValue); err != nil {
		return GeneratedAPIKey{}, err
	}
	secret := KeyNamespace + base64.RawURLEncoding.EncodeToString(randomValue)
	hash := sha256.Sum256([]byte(secret))
	id, err := ids.New("sak")
	if err != nil {
		return GeneratedAPIKey{}, err
	}
	prefixLength := len(KeyNamespace) + 8
	item, err := s.repository.Create(ctx, CreateInput{
		ID: id, Name: strings.TrimSpace(name), KeyPrefix: secret[:prefixLength],
		KeyLast4: secret[len(secret)-4:], KeyHash: hash[:], Scopes: []string{ScopeFull},
		Actor: strings.TrimSpace(actor), RequestID: strings.TrimSpace(requestID),
	})
	if err != nil {
		return GeneratedAPIKey{}, err
	}
	return GeneratedAPIKey{APIKey: item, Secret: secret}, nil
}

func (s *Service) Revoke(ctx context.Context, id, actor, requestID string) (APIKey, error) {
	return s.repository.Revoke(ctx, strings.TrimSpace(id), strings.TrimSpace(actor), strings.TrimSpace(requestID))
}

func (s *Service) Authenticate(ctx context.Context, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if len(token) != encodedKeyLength || !strings.HasPrefix(token, KeyNamespace) {
		return false, nil
	}
	hash := sha256.Sum256([]byte(token))
	return s.repository.Authenticate(ctx, hash[:])
}
