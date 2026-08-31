package servicekeys

import (
	"context"
	"errors"
	"time"
)

const (
	KeyNamespace  = "epk_"
	ScopeFull     = "gateway:full"
	StatusActive  = "ACTIVE"
	StatusRevoked = "REVOKED"
)

var ErrNotFound = errors.New("service API key not found")

type APIKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	KeyHint   string     `json:"key_hint"`
	Scopes    []string   `json:"scopes"`
	Status    string     `json:"status"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedBy string     `json:"revoked_by,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type GeneratedAPIKey struct {
	APIKey APIKey `json:"api_key"`
	Secret string `json:"secret"`
}

type CreateInput struct {
	ID        string
	Name      string
	KeyPrefix string
	KeyLast4  string
	KeyHash   []byte
	Scopes    []string
	Actor     string
	RequestID string
}

type Repository interface {
	List(context.Context) ([]APIKey, error)
	Create(context.Context, CreateInput) (APIKey, error)
	Revoke(context.Context, string, string, string) (APIKey, error)
	Authenticate(context.Context, []byte) (bool, error)
}

func keyHint(prefix, lastFour string) string {
	if prefix == "" || lastFour == "" {
		return ""
	}
	return prefix + "••••••••" + lastFour
}
