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
	connectors map[runtimeKey]connector.Connector
	manifests  map[runtimeKey]connector.Manifest
	versions   map[string][]string
}

type runtimeKey struct {
	providerCode    string
	providerVersion string
}

// Pinger is optional so in-process test connectors remain small. Remote
// connectors implement it to make API readiness depend on their runner.
type Pinger interface {
	Ping(context.Context) error
}

func New(items ...connector.Connector) (*Registry, error) {
	result := &Registry{
		connectors: make(map[runtimeKey]connector.Connector, len(items)),
		manifests:  make(map[runtimeKey]connector.Manifest, len(items)),
		versions:   make(map[string][]string),
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
		key := runtimeKey{providerCode: code, providerVersion: strings.TrimSpace(manifest.Version)}
		if _, exists := result.connectors[key]; exists {
			return nil, fmt.Errorf("duplicate connector runtime %s@%s", code, key.providerVersion)
		}
		result.connectors[key] = item
		result.manifests[key] = manifest
		result.versions[code] = append(result.versions[code], key.providerVersion)
	}
	if len(result.connectors) == 0 {
		return nil, fmt.Errorf("at least one connector is required")
	}
	for code := range result.versions {
		sort.Strings(result.versions[code])
	}
	return result, nil
}

func (r *Registry) Connector(code string) (connector.Connector, error) {
	return r.ConnectorVersion(code, "")
}

func (r *Registry) Manifest(code string) (connector.Manifest, error) {
	return r.ManifestVersion(code, "")
}

// ConnectorVersion resolves one immutable shared runtime. An empty version is
// accepted only while a provider has exactly one loaded runtime, preserving
// compatibility during the staged migration to version-pinned dispatch.
func (r *Registry) ConnectorVersion(code, version string) (connector.Connector, error) {
	key, err := r.resolve(code, version)
	if err != nil {
		return nil, err
	}
	return r.connectors[key], nil
}

func (r *Registry) ManifestVersion(code, version string) (connector.Manifest, error) {
	key, err := r.resolve(code, version)
	if err != nil {
		return connector.Manifest{}, err
	}
	return r.manifests[key].Clone(), nil
}

func (r *Registry) Codes() []string {
	codes := make([]string, 0, len(r.versions))
	for code := range r.versions {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func (r *Registry) Manifests() []connector.Manifest {
	keys := r.runtimeKeys()
	items := make([]connector.Manifest, 0, len(keys))
	for _, key := range keys {
		items = append(items, r.manifests[key].Clone())
	}
	return items
}

func (r *Registry) Supports(code string, operation connector.Operation) (bool, error) {
	return r.SupportsVersion(code, "", operation)
}

func (r *Registry) SupportsVersion(code, version string, operation connector.Operation) (bool, error) {
	key, err := r.resolve(code, version)
	if err != nil {
		return false, err
	}
	manifest := r.manifests[key]
	return manifest.Supports(operation), nil
}

func (r *Registry) Ping(ctx context.Context) error {
	for _, key := range r.runtimeKeys() {
		if pinger, ok := r.connectors[key].(Pinger); ok {
			if err := pinger.Ping(ctx); err != nil {
				return fmt.Errorf("connector %s@%s is not ready: %w", key.providerCode, key.providerVersion, err)
			}
		}
	}
	return nil
}

func (r *Registry) resolve(code, version string) (runtimeKey, error) {
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	loadedVersions := r.versions[normalizedCode]
	if len(loadedVersions) == 0 {
		return runtimeKey{}, &connector.APIError{Provider: code, Status: 422, Code: "CONNECTOR_NOT_AVAILABLE", Message: "connector is not installed"}
	}
	normalizedVersion := strings.TrimSpace(version)
	if normalizedVersion == "" {
		if len(loadedVersions) != 1 {
			return runtimeKey{}, &connector.APIError{Provider: code, Status: 409, Code: "CONNECTOR_VERSION_REQUIRED", Message: "provider has multiple loaded connector versions"}
		}
		normalizedVersion = loadedVersions[0]
	}
	key := runtimeKey{providerCode: normalizedCode, providerVersion: normalizedVersion}
	if _, ok := r.connectors[key]; !ok {
		return runtimeKey{}, &connector.APIError{Provider: code, Status: 422, Code: "CONNECTOR_VERSION_NOT_AVAILABLE", Message: "connector version is not loaded"}
	}
	return key, nil
}

func (r *Registry) runtimeKeys() []runtimeKey {
	keys := make([]runtimeKey, 0, len(r.connectors))
	for key := range r.connectors {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].providerCode == keys[j].providerCode {
			return keys[i].providerVersion < keys[j].providerVersion
		}
		return keys[i].providerCode < keys[j].providerCode
	})
	return keys
}
