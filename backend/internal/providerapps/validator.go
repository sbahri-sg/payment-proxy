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
	"regexp"
	"sort"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"gopkg.in/yaml.v3"
)

const (
	MaxArtifactBytes     = 25 << 20
	MaxUncompressedBytes = 64 << 20
	MaxFiles             = 128

	PackageFormatSubmissionV1 = "provider_submission_v1"
	PackageFormatLegacyBundle = "legacy_runtime_bundle"
)

var (
	ErrInvalidBundle  = errors.New("provider app bundle is invalid")
	ErrBundleTooLarge = errors.New("provider app bundle exceeds the size limit")
)

// Manifest is the static contract shipped by a provider app. Uploaded apps
// deliberately cannot declare native_go: executable loading happens only in a
// separately managed runner, never inside the Payment Kernel process.
type Manifest struct {
	PackageFormat         string                                    `json:"package_format"`
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
	PackageFormat    string   `json:"package_format"`
	Passed           bool     `json:"passed"`
	FileCount        int      `json:"file_count"`
	UncompressedSize int64    `json:"uncompressed_size"`
	EntrypointSHA256 string   `json:"entrypoint_sha256"`
	Checks           []Check  `json:"checks"`
	Warnings         []string `json:"warnings"`
}

type submissionManifest struct {
	SchemaVersion string `yaml:"schema_version"`
	Provider      struct {
		Code string `yaml:"code"`
		Name string `yaml:"name"`
	} `yaml:"provider"`
	Release struct {
		Version string `yaml:"version"`
	} `yaml:"release"`
	Connector struct {
		ContractVersion string `yaml:"contract_version"`
		Runtime         string `yaml:"runtime"`
		SDKVersion      string `yaml:"sdk_version"`
	} `yaml:"connector"`
	Operations  []connector.Operation `yaml:"operations"`
	Credentials struct {
		Fields []struct {
			Code      string `yaml:"code"`
			Label     string `yaml:"label"`
			InputType string `yaml:"input_type"`
			Secret    bool   `yaml:"secret"`
			Required  bool   `yaml:"required"`
		} `yaml:"fields"`
	} `yaml:"credentials"`
	CertificationProfiles map[string]struct {
		Code             string `yaml:"code"`
		Automated        bool   `yaml:"automated"`
		WebhookSetupHint string `yaml:"webhook_setup_hint,omitempty"`
	} `yaml:"certification_profiles"`
	Environments []struct {
		Code        string `yaml:"code"`
		Label       string `yaml:"label"`
		Description string `yaml:"description,omitempty"`
	} `yaml:"environments"`
	PaymentMethods []string `yaml:"payment_methods"`
	OutboundHosts  []string `yaml:"outbound_hosts"`
}

var blockedSubmissionExtensions = map[string]struct{}{
	".app": {}, ".bin": {}, ".dll": {}, ".dylib": {}, ".exe": {}, ".so": {},
}

var sensitiveSubmissionPatterns = []struct {
	code    string
	pattern *regexp.Regexp
}{
	{code: "private_key", pattern: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{code: "xendit_api_key", pattern: regexp.MustCompile(`xnd_(?:development|production)_[A-Za-z0-9._-]{20,}`)},
	{code: "generic_secret", pattern: regexp.MustCompile(`(?i)(?:api[_-]?key|secret|token)\s*[:=]\s*["']?[A-Za-z0-9._-]{24,}`)},
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
	if total > int64(len(payload))*100 && total > 10<<20 {
		return Result{}, fmt.Errorf("%w: unsafe ZIP compression ratio", ErrInvalidBundle)
	}

	_, hasSubmissionManifest := files["emisell-extension.yaml"]
	_, hasLegacyManifest := files["manifest.json"]
	if hasSubmissionManifest {
		if hasLegacyManifest {
			return Result{}, fmt.Errorf("%w: package cannot mix emisell-extension.yaml and legacy manifest.json", ErrInvalidBundle)
		}
		return validateSubmissionBundle(result, files)
	}
	return validateLegacyBundle(result, files)
}

func validateLegacyBundle(result Result, files map[string][]byte) (Result, error) {
	result.Manifest.PackageFormat = PackageFormatLegacyBundle
	result.Report.PackageFormat = PackageFormatLegacyBundle

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
	if err := validateLegacyManifest(result.Manifest, files); err != nil {
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

func validateSubmissionBundle(result Result, files map[string][]byte) (Result, error) {
	result.Manifest.PackageFormat = PackageFormatSubmissionV1
	result.Report.PackageFormat = PackageFormatSubmissionV1
	if err := validateSubmissionFiles(files); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	result.Report.Checks = append(result.Report.Checks, Check{Code: "source_only", Status: "PASSED", Detail: "Submission contains no native runtime binary or embedded credential"})

	manifestPayload := files["emisell-extension.yaml"]
	openAPIPayload, ok := files["openapi.yaml"]
	if !ok {
		return Result{}, fmt.Errorf("%w: root openapi.yaml is required", ErrInvalidBundle)
	}
	var declared submissionManifest
	decoder := yaml.NewDecoder(bytes.NewReader(manifestPayload))
	decoder.KnownFields(true)
	if err := decoder.Decode(&declared); err != nil {
		return Result{}, fmt.Errorf("%w: emisell-extension.yaml: %v", ErrInvalidBundle, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Result{}, fmt.Errorf("%w: emisell-extension.yaml must contain one YAML document", ErrInvalidBundle)
	}
	if declared.SchemaVersion != "1" {
		return Result{}, fmt.Errorf("%w: schema_version must be 1", ErrInvalidBundle)
	}
	result.Manifest.ContractVersion = strings.TrimSpace(declared.Connector.ContractVersion)
	result.Manifest.Code = strings.ToLower(strings.TrimSpace(declared.Provider.Code))
	result.Manifest.Name = strings.TrimSpace(declared.Provider.Name)
	result.Manifest.Version = strings.TrimSpace(declared.Release.Version)
	result.Manifest.Runtime = strings.TrimSpace(declared.Connector.Runtime)
	result.Manifest.SDKVersion = strings.TrimSpace(declared.Connector.SDKVersion)
	result.Manifest.Operations = append([]connector.Operation(nil), declared.Operations...)
	result.Manifest.CredentialFields = make([]connector.CredentialField, 0, len(declared.Credentials.Fields))
	for _, field := range declared.Credentials.Fields {
		result.Manifest.CredentialFields = append(result.Manifest.CredentialFields, connector.CredentialField{
			Code: strings.TrimSpace(field.Code), Label: strings.TrimSpace(field.Label), InputType: strings.TrimSpace(field.InputType),
			Secret: field.Secret, Required: field.Required,
		})
	}
	result.Manifest.CertificationProfiles = make(map[string]connector.CertificationProfile, len(declared.CertificationProfiles))
	for method, profile := range declared.CertificationProfiles {
		result.Manifest.CertificationProfiles[strings.TrimSpace(method)] = connector.CertificationProfile{
			Code: strings.TrimSpace(profile.Code), Automated: profile.Automated, WebhookSetupHint: strings.TrimSpace(profile.WebhookSetupHint),
		}
	}
	result.Manifest.Environments = make([]string, 0, len(declared.Environments))
	for _, environment := range declared.Environments {
		if strings.TrimSpace(environment.Label) == "" || len(environment.Label) > 80 || len(environment.Description) > 300 {
			return Result{}, fmt.Errorf("%w: every environment requires a valid label and description", ErrInvalidBundle)
		}
		result.Manifest.Environments = append(result.Manifest.Environments, strings.TrimSpace(environment.Code))
	}
	result.Manifest.PaymentMethods = append([]string(nil), declared.PaymentMethods...)
	result.Manifest.OutboundHosts = append([]string(nil), declared.OutboundHosts...)
	if err := validateNormalizedManifest(result.Manifest); err != nil {
		return Result{}, fmt.Errorf("%w: emisell-extension.yaml: %v", ErrInvalidBundle, err)
	}
	result.Report.Checks = append(result.Report.Checks, Check{Code: "manifest_contract", Status: "PASSED", Detail: "Provider submission schema v1 is valid"})
	if err := validateSubmissionOpenAPI(openAPIPayload, result.Manifest.Operations); err != nil {
		return Result{}, fmt.Errorf("%w: openapi.yaml: %v", ErrInvalidBundle, err)
	}
	result.Report.Checks = append(result.Report.Checks, Check{Code: "openapi_contract", Status: "PASSED", Detail: "OpenAPI 3.x declares every canonical operation"})
	result.Report.Checks = append(result.Report.Checks, Check{Code: "credential_separation", Status: "PASSED", Detail: "Submission contains credential schema only; merchant secrets remain installation-scoped"})
	if _, ok := files["README.md"]; !ok {
		result.Report.Warnings = append(result.Report.Warnings, "README.md is recommended for provider review.")
	}
	if _, ok := files["SECURITY.md"]; !ok {
		result.Report.Warnings = append(result.Report.Warnings, "SECURITY.md is recommended for provider review.")
	}
	result.Report.Warnings = append(result.Report.Warnings, "Runtime image is built, scanned, signed, and deployed separately from this review submission.")
	result.Report.Passed = true
	return result, nil
}

func validateSubmissionFiles(files map[string][]byte) error {
	findings := make([]string, 0)
	for name, content := range files {
		lowerName := strings.ToLower(name)
		if _, blocked := blockedSubmissionExtensions[strings.ToLower(path.Ext(lowerName))]; blocked {
			return fmt.Errorf("native runtime file %q is not allowed in a review submission", name)
		}
		base := strings.ToLower(path.Base(lowerName))
		if base == ".env" || strings.Contains(base, "id_rsa") || strings.Contains(base, "id_ed25519") {
			findings = append(findings, name)
		}
		if hasNativeExecutableMagic(content) {
			return fmt.Errorf("native executable content in %q is not allowed in a review submission", name)
		}
		for _, sensitive := range sensitiveSubmissionPatterns {
			if sensitive.pattern.Match(content) {
				findings = append(findings, name+":"+sensitive.code)
			}
		}
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		return fmt.Errorf("secret or sensitive file detected: %s", strings.Join(findings, ", "))
	}
	return nil
}

func hasNativeExecutableMagic(content []byte) bool {
	if len(content) >= 4 {
		magic := string(content[:4])
		if magic == "\x7fELF" || magic == "\xfe\xed\xfa\xce" || magic == "\xce\xfa\xed\xfe" || magic == "\xfe\xed\xfa\xcf" || magic == "\xcf\xfa\xed\xfe" || magic == "\xca\xfe\xba\xbe" {
			return true
		}
	}
	return len(content) >= 2 && content[0] == 'M' && content[1] == 'Z'
}

func validateSubmissionOpenAPI(payload []byte, operations []connector.Operation) error {
	var document struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return errors.New("document is not valid YAML")
	}
	if !strings.HasPrefix(document.OpenAPI, "3.") {
		return errors.New("document must use OpenAPI 3.x")
	}
	required := map[string]string{
		"/health": "get", "/capabilities": "get", "/payment-methods/validate": "post", "/payments/validate": "post",
	}
	operationPaths := map[connector.Operation]string{
		connector.OperationVerifyInstallation:   "POST /installations/verify",
		connector.OperationDisableInstallation:  "POST /installations/disable",
		connector.OperationCreatePayment:        "POST /payments/create",
		connector.OperationCreateHostedCheckout: "POST /payments/create",
		connector.OperationGetPayment:           "POST /payments/get",
		connector.OperationCapturePayment:       "POST /payments/capture",
		connector.OperationCancelPayment:        "POST /payments/cancel",
		connector.OperationSimulatePayment:      "POST /payments/simulate",
		connector.OperationCreateRefund:         "POST /refunds/create",
		connector.OperationGetRefund:            "POST /refunds/get",
		connector.OperationHandleWebhook:        "POST /webhooks/normalize",
	}
	for _, operation := range operations {
		declaration, ok := operationPaths[operation]
		if !ok {
			continue
		}
		parts := strings.SplitN(declaration, " ", 2)
		required[parts[1]] = strings.ToLower(parts[0])
	}
	missing := make([]string, 0)
	for requiredPath, method := range required {
		methods, ok := document.Paths[requiredPath]
		if !ok {
			missing = append(missing, strings.ToUpper(method)+" "+requiredPath)
			continue
		}
		if _, ok = methods[method]; !ok {
			missing = append(missing, strings.ToUpper(method)+" "+requiredPath)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("canonical operations are missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateLegacyManifest(value Manifest, files map[string][]byte) error {
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
	return validateNormalizedManifest(value)
}

func validateNormalizedManifest(value Manifest) error {
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
