package providerapps

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	MaxArtifactBytes     = 25 << 20
	MaxUncompressedBytes = 64 << 20
	MaxFiles             = 128
)

var (
	ErrInvalidBundle  = errors.New("provider app bundle is invalid")
	ErrBundleTooLarge = errors.New("provider app bundle exceeds the size limit")
)

// Manifest is the static contract shipped by a provider app. Uploaded apps
// deliberately cannot declare native_go: executable loading happens only in a
// separately managed runner, never inside the Payment Kernel process.
type Manifest struct {
	ContractVersion       string                                    `json:"contract_version"`
	Code                  string                                    `json:"code"`
	Name                  string                                    `json:"name"`
	Version               string                                    `json:"version"`
	Runtime               string                                    `json:"runtime"`
	SDKVersion            string                                    `json:"sdk_version"`
	Entrypoint            string                                    `json:"entrypoint"`
	Operations            []connector.Operation                     `json:"operations"`
	CredentialFields      []connector.CredentialField               `json:"credential_fields"`
	CertificationProfiles map[string]connector.CertificationProfile `json:"certification_profiles"`
	Environments          []string                                  `json:"environments"`
	PaymentMethods        []string                                  `json:"payment_methods"`
	OutboundHosts         []string                                  `json:"outbound_hosts"`
}

type Check struct {
	Code   string `json:"code"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type ScanReport struct {
	Passed           bool     `json:"passed"`
	FileCount        int      `json:"file_count"`
	UncompressedSize int64    `json:"uncompressed_size"`
	EntrypointSHA256 string   `json:"entrypoint_sha256"`
	Checks           []Check  `json:"checks"`
	Warnings         []string `json:"warnings"`
}

type Result struct {
	Manifest       Manifest   `json:"manifest"`
	Report         ScanReport `json:"scan_report"`
	ArtifactSHA256 string     `json:"artifact_sha256"`
}

func ValidateBundle(payload []byte) (Result, error) {
	if len(payload) == 0 {
		return Result{}, fmt.Errorf("%w: empty artifact", ErrInvalidBundle)
	}
	if len(payload) > MaxArtifactBytes {
		return Result{}, ErrBundleTooLarge
	}
	digest := sha256.Sum256(payload)
	result := Result{ArtifactSHA256: hex.EncodeToString(digest[:])}
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return Result{}, fmt.Errorf("%w: artifact is not a readable ZIP", ErrInvalidBundle)
	}
	if len(reader.File) == 0 || len(reader.File) > MaxFiles {
		return Result{}, fmt.Errorf("%w: bundle must contain 1 to %d files", ErrInvalidBundle, MaxFiles)
	}

	files := make(map[string][]byte, len(reader.File))
	var total int64
	for _, file := range reader.File {
		name := strings.TrimSpace(strings.ReplaceAll(file.Name, "\\", "/"))
		clean := path.Clean(name)
		if name == "" || clean == "." || clean != name || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return Result{}, fmt.Errorf("%w: unsafe ZIP path %q", ErrInvalidBundle, file.Name)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return Result{}, fmt.Errorf("%w: symbolic links are not allowed", ErrInvalidBundle)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if _, duplicate := files[clean]; duplicate {
			return Result{}, fmt.Errorf("%w: duplicate ZIP path %q", ErrInvalidBundle, clean)
		}
		total += int64(file.UncompressedSize64)
		if total > MaxUncompressedBytes {
			return Result{}, ErrBundleTooLarge
		}
		stream, openErr := file.Open()
		if openErr != nil {
			return Result{}, fmt.Errorf("%w: cannot read %q", ErrInvalidBundle, clean)
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, MaxUncompressedBytes+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || int64(len(content)) != int64(file.UncompressedSize64) {
			return Result{}, fmt.Errorf("%w: incomplete ZIP entry %q", ErrInvalidBundle, clean)
		}
		files[clean] = content
	}
	result.Report.FileCount = len(files)
	result.Report.UncompressedSize = total
	result.Report.Checks = append(result.Report.Checks, Check{Code: "archive_safety", Status: "PASSED", Detail: "ZIP paths, file count, and expanded size are within limits"})

	manifestPayload, ok := files["manifest.json"]
	if !ok {
		return Result{}, fmt.Errorf("%w: root manifest.json is required", ErrInvalidBundle)
	}
	decoder := json.NewDecoder(bytes.NewReader(manifestPayload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.Manifest); err != nil {
		return Result{}, fmt.Errorf("%w: manifest.json: %v", ErrInvalidBundle, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Result{}, fmt.Errorf("%w: manifest.json must contain one JSON object", ErrInvalidBundle)
	}
	if err := validateManifest(result.Manifest, files); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	result.Report.Checks = append(result.Report.Checks, Check{Code: "manifest_contract", Status: "PASSED", Detail: "Connector SDK v1 manifest is valid"})

	checksumsPayload, ok := files["checksums.txt"]
	if !ok {
		return Result{}, fmt.Errorf("%w: root checksums.txt is required", ErrInvalidBundle)
	}
	if err := validateChecksums(checksumsPayload, files); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	entrypointDigest := sha256.Sum256(files[result.Manifest.Entrypoint])
	result.Report.EntrypointSHA256 = hex.EncodeToString(entrypointDigest[:])
	result.Report.Checks = append(result.Report.Checks, Check{Code: "artifact_checksums", Status: "PASSED", Detail: "Every packaged file is covered by SHA-256"})
	result.Report.Checks = append(result.Report.Checks, Check{Code: "credential_separation", Status: "PASSED", Detail: "Manifest contains credential schema only; merchant secrets remain installation-scoped"})
	result.Report.Warnings = []string{"Cryptographic partner signature and malware scanning remain deployment gates before production publish."}
	result.Report.Passed = true
	return result, nil
}

func validateManifest(value Manifest, files map[string][]byte) error {
	if value.ContractVersion != "v1" || value.SDKVersion != "v1" {
		return errors.New("contract_version and sdk_version must be v1")
	}
	base := connector.Manifest{
		Code: value.Code, Name: value.Name, Version: value.Version, Runtime: value.Runtime,
		Operations: value.Operations, CredentialFields: value.CredentialFields,
		CertificationProfiles: value.CertificationProfiles,
	}
	if err := base.Validate(); err != nil {
		return err
	}
	if value.Runtime != "isolated_container" && value.Runtime != "remote_http" {
		return errors.New("uploaded runtime must be isolated_container or remote_http")
	}
	entrypoint := path.Clean(strings.TrimSpace(value.Entrypoint))
	if entrypoint == "." || entrypoint != value.Entrypoint || strings.HasPrefix(entrypoint, "/") || strings.HasPrefix(entrypoint, "../") {
		return errors.New("entrypoint must be a safe relative file path")
	}
	if _, ok := files[entrypoint]; !ok {
		return fmt.Errorf("entrypoint %q is missing from the bundle", entrypoint)
	}
	if err := uniqueAllowed(value.Environments, map[string]bool{"sandbox": true, "live": true}, "environment"); err != nil {
		return err
	}
	if len(value.PaymentMethods) == 0 {
		return errors.New("at least one payment method is required")
	}
	if err := uniqueIdentifiers(value.PaymentMethods, "payment method"); err != nil {
		return err
	}
	if len(value.OutboundHosts) == 0 {
		return errors.New("at least one outbound host is required")
	}
	seenHosts := map[string]bool{}
	for _, raw := range value.OutboundHosts {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" || host != raw || strings.ContainsAny(host, "/:* ") || net.ParseIP(host) != nil || host == "localhost" || !strings.Contains(host, ".") {
			return fmt.Errorf("outbound host %q must be a lowercase DNS hostname", raw)
		}
		if seenHosts[host] {
			return fmt.Errorf("duplicate outbound host %q", host)
		}
		seenHosts[host] = true
	}
	return nil
}

func uniqueAllowed(values []string, allowed map[string]bool, label string) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one %s is required", label)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !allowed[value] {
			return fmt.Errorf("unsupported %s %q", label, value)
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = true
	}
	return nil
}

func uniqueIdentifiers(values []string, label string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || value != strings.ToLower(strings.TrimSpace(value)) {
			return fmt.Errorf("invalid %s %q", label, value)
		}
		for index, char := range value {
			if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || ((char == '_' || char == '-') && index > 0) {
				continue
			}
			return fmt.Errorf("invalid %s %q", label, value)
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = true
	}
	return nil
}

func validateChecksums(payload []byte, files map[string][]byte) error {
	expected := make(map[string]string)
	for lineNumber, raw := range strings.Split(strings.TrimSpace(string(payload)), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || len(parts[0]) != 64 {
			return fmt.Errorf("checksums.txt line %d must be '<sha256>  <path>'", lineNumber+1)
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return fmt.Errorf("checksums.txt line %d has an invalid SHA-256", lineNumber+1)
		}
		name := strings.TrimPrefix(parts[1], "*")
		if name == "checksums.txt" {
			return errors.New("checksums.txt must not checksum itself")
		}
		if _, exists := expected[name]; exists {
			return fmt.Errorf("duplicate checksum path %q", name)
		}
		expected[name] = strings.ToLower(parts[0])
	}
	paths := make([]string, 0, len(files)-1)
	for name := range files {
		if name != "checksums.txt" {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	for _, name := range paths {
		want, exists := expected[name]
		if !exists {
			return fmt.Errorf("checksum is missing for %q", name)
		}
		digest := sha256.Sum256(files[name])
		if hex.EncodeToString(digest[:]) != want {
			return fmt.Errorf("checksum mismatch for %q", name)
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		for name := range expected {
			return fmt.Errorf("checksum references missing file %q", name)
		}
	}
	return nil
}
