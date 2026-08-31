package registry

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

// Registry is the only place where provider implementations are bound to the
// universal connector contract. It is assembled once at process startup.
type Registry struct {
	connectors map[string]connector.Connector
	manifests  map[string]connector.Manifest
}

// Pinger is optional so in-process test connectors remain small. Remote
// connectors implement it to make API readiness depend on their runner.
type Pinger interface {
	Ping(context.Context) error
}

func New(items ...connector.Connector) (*Registry, error) {
	result := &Registry{
		connectors: make(map[string]connector.Connector, len(items)),
		manifests:  make(map[string]connector.Manifest, len(items)),
	}
	for _, item := range items {
		if item == nil {
			return nil, fmt.Errorf("connector implementation is nil")
		}
		code := strings.ToLower(strings.TrimSpace(item.Code()))
		manifest := item.Manifest().Clone()
		if err := manifest.Validate(); err != nil {
			return nil, fmt.Errorf("connector %q manifest: %w", code, err)
		}
		if code == "" || code != manifest.Code {
			return nil, fmt.Errorf("connector code %q does not match manifest code %q", code, manifest.Code)
		}
		if _, exists := result.connectors[code]; exists {
			return nil, fmt.Errorf("duplicate connector %s", code)
		}
		result.connectors[code] = item
		result.manifests[code] = manifest
	}
	if len(result.connectors) == 0 {
		return nil, fmt.Errorf("at least one connector is required")
	}
	return result, nil
}

func (r *Registry) Connector(code string) (connector.Connector, error) {
	item, ok := r.connectors[strings.ToLower(strings.TrimSpace(code))]
	if !ok {
		return nil, &connector.APIError{Provider: code, Status: 422, Code: "CONNECTOR_NOT_AVAILABLE", Message: "connector is not installed"}
	}
	return item, nil
}

func (r *Registry) Manifest(code string) (connector.Manifest, error) {
	manifest, ok := r.manifests[strings.ToLower(strings.TrimSpace(code))]
	if !ok {
		return connector.Manifest{}, &connector.APIError{Provider: code, Status: 422, Code: "CONNECTOR_NOT_AVAILABLE", Message: "connector is not installed"}
	}
	return manifest.Clone(), nil
}

func (r *Registry) Codes() []string {
	codes := make([]string, 0, len(r.connectors))
	for code := range r.connectors {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func (r *Registry) Manifests() []connector.Manifest {
	codes := r.Codes()
	items := make([]connector.Manifest, 0, len(codes))
	for _, code := range codes {
		items = append(items, r.manifests[code].Clone())
	}
	return items
}

func (r *Registry) Supports(code string, operation connector.Operation) (bool, error) {
	manifest, ok := r.manifests[strings.ToLower(strings.TrimSpace(code))]
	if !ok {
		return false, &connector.APIError{Provider: code, Status: 422, Code: "CONNECTOR_NOT_AVAILABLE", Message: "connector is not installed"}
	}
	return manifest.Supports(operation), nil
}

func (r *Registry) Ping(ctx context.Context) error {
	for _, code := range r.Codes() {
		if pinger, ok := r.connectors[code].(Pinger); ok {
			if err := pinger.Ping(ctx); err != nil {
				return fmt.Errorf("connector %s is not ready: %w", code, err)
			}
		}
	}
	return nil
}
