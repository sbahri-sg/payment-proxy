package providerapps

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/doku"
	"github.com/emisell/api-payment-proxy/internal/duitku"
	"github.com/emisell/api-payment-proxy/internal/ipaymu"
	"github.com/emisell/api-payment-proxy/internal/midtrans"
	"github.com/emisell/api-payment-proxy/internal/xendit"
)

func validBundle(t *testing.T) []byte {
	t.Helper()
	manifest := []byte(`{"contract_version":"v1","code":"midtrans","name":"Midtrans","version":"0.1.0","runtime":"isolated_container","sdk_version":"v1","entrypoint":"connector","operations":["verify_installation","create_payment","get_payment","handle_webhook"],"credential_fields":[{"code":"server_key","label":"Server key","input_type":"password","secret":true,"required":true}],"certification_profiles":{"qris":{"code":"midtrans/qris","automated":true}},"environments":["sandbox","live"],"payment_methods":["qris"],"outbound_hosts":["api.midtrans.com"]}`)
	entrypoint := []byte("connector-placeholder")
	manifestHash := sha256.Sum256(manifest)
	entrypointHash := sha256.Sum256(entrypoint)
	checksums := []byte(hex.EncodeToString(manifestHash[:]) + "  manifest.json\n" + hex.EncodeToString(entrypointHash[:]) + "  connector\n")
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string][]byte{"manifest.json": manifest, "connector": entrypoint, "checksums.txt": checksums} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestValidateBundle(t *testing.T) {
	result, err := ValidateBundle(validBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	entrypointHash := sha256.Sum256([]byte("connector-placeholder"))
	if !result.Report.Passed || result.Manifest.PackageFormat != PackageFormatLegacyBundle || result.Report.PackageFormat != PackageFormatLegacyBundle || result.Manifest.Code != "midtrans" || result.Manifest.Version != "0.1.0" || result.ArtifactSHA256 == "" || result.Report.EntrypointSHA256 != hex.EncodeToString(entrypointHash[:]) {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func submissionBundle(t *testing.T, extra map[string][]byte) []byte {
	t.Helper()
	files := map[string][]byte{
		"emisell-extension.yaml": []byte(`schema_version: "1"
provider:
  code: xendit
  name: Xendit
release:
  version: emisell-xendit-v1
connector:
  contract_version: v1
  runtime: isolated_container
  sdk_version: v1
operations: [verify_installation, create_payment, get_payment, handle_webhook]
credentials:
  fields:
    - {code: api_key, label: Secret API key, input_type: password, secret: true, required: true}
certification_profiles:
  qris: {code: xendit-payments-v3/qris, automated: true}
environments:
  - {code: live, label: Live, description: Production payment processing.}
  - {code: sandbox, label: Sandbox, description: Emisell simulation flow.}
payment_methods: [qris]
outbound_hosts: [api.xendit.co]
`),
		"openapi.yaml": []byte(`openapi: 3.0.3
info: {title: Xendit connector, version: 1.0.0}
paths:
  /health: {get: {responses: {"200": {description: Ready}}}}
  /capabilities: {get: {responses: {"200": {description: Capabilities}}}}
  /payment-methods/validate: {post: {responses: {"200": {description: Valid}}}}
  /payments/validate: {post: {responses: {"200": {description: Valid}}}}
  /installations/verify: {post: {responses: {"200": {description: Verified}}}}
  /payments/create: {post: {responses: {"200": {description: Created}}}}
  /payments/get: {post: {responses: {"200": {description: Payment}}}}
  /webhooks/normalize: {post: {responses: {"200": {description: Event}}}}
`),
		"README.md":   []byte("# Xendit Provider App\n"),
		"SECURITY.md": []byte("# Security\nNo credentials are packaged.\n"),
	}
	for name, content := range extra {
		files[name] = content
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestValidateSubmissionBundle(t *testing.T) {
	result, err := ValidateBundle(submissionBundle(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Report.Passed || result.Manifest.PackageFormat != PackageFormatSubmissionV1 || result.Report.PackageFormat != PackageFormatSubmissionV1 || result.Manifest.Entrypoint != "" || result.Report.EntrypointSHA256 != "" {
		t.Fatalf("unexpected submission result: %#v", result)
	}
}

func TestValidateSubmissionRejectsNativeBinary(t *testing.T) {
	if _, err := ValidateBundle(submissionBundle(t, map[string][]byte{"connector.bin": []byte("native")})); err == nil {
		t.Fatal("submission with native binary was accepted")
	}
}

func TestValidateSubmissionRequiresDeclaredOpenAPIOperations(t *testing.T) {
	bundle := submissionBundle(t, map[string][]byte{
		"openapi.yaml": []byte(`openapi: 3.0.3
info: {title: Incomplete, version: 1.0.0}
paths:
  /health: {get: {responses: {"200": {description: Ready}}}}
`),
	})
	if _, err := ValidateBundle(bundle); err == nil {
		t.Fatal("submission with incomplete OpenAPI contract was accepted")
	}
}

func TestMidtransProviderAppManifestMatchesRuntimeRelease(t *testing.T) {
	files := []struct {
		archivePath string
		sourcePath  string
	}{
		{archivePath: "emisell-extension.yaml", sourcePath: "../../../provider-apps/midtrans/emisell-extension.yaml"},
		{archivePath: "openapi.yaml", sourcePath: "../../../provider-apps/midtrans/openapi.yaml"},
		{archivePath: "README.md", sourcePath: "../../../provider-apps/midtrans/README.md"},
		{archivePath: "SECURITY.md", sourcePath: "../../../provider-apps/midtrans/SECURITY.md"},
		{archivePath: "contract-tests/README.md", sourcePath: "../../../provider-apps/midtrans/contract-tests/README.md"},
		{archivePath: "src/midtrans/client.go", sourcePath: "../midtrans/client.go"},
		{archivePath: "src/midtrans/manifest.go", sourcePath: "../midtrans/manifest.go"},
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range files {
		content, err := os.ReadFile(item.sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		file, err := writer.Create(item.archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ValidateBundle(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	client, err := midtrans.New("", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.SetExecutableSHA256(strings.Repeat("a", 64))
	runtimeManifest := client.Manifest()
	verification := VerifyRuntimeContract(result.Manifest, runtimeManifest)
	if result.Manifest.Code != "midtrans" || result.Manifest.Version != client.Manifest().Version || len(result.Manifest.CredentialFields) != 2 || len(result.Manifest.PaymentMethods) != 18 {
		t.Fatalf("Midtrans Provider App manifest diverged from runtime release: %#v", result.Manifest)
	}
	if !result.Report.Passed || result.Report.PackageFormat != PackageFormatSubmissionV1 || result.Report.FileCount != len(files) || !verification.Passed {
		t.Fatalf("Midtrans submission failed release verification: submission=%#v verification=%#v", result, verification)
	}
}

func TestDuitkuProviderAppManifestMatchesRuntimeRelease(t *testing.T) {
	files := []struct {
		archivePath string
		sourcePath  string
	}{
		{archivePath: "emisell-extension.yaml", sourcePath: "../../../provider-apps/duitku/emisell-extension.yaml"},
		{archivePath: "openapi.yaml", sourcePath: "../../../provider-apps/duitku/openapi.yaml"},
		{archivePath: "README.md", sourcePath: "../../../provider-apps/duitku/README.md"},
		{archivePath: "SECURITY.md", sourcePath: "../../../provider-apps/duitku/SECURITY.md"},
		{archivePath: "contract-tests/README.md", sourcePath: "../../../provider-apps/duitku/contract-tests/README.md"},
		{archivePath: "src/duitku/client.go", sourcePath: "../duitku/client.go"},
		{archivePath: "src/duitku/manifest.go", sourcePath: "../duitku/manifest.go"},
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range files {
		content, err := os.ReadFile(item.sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		file, err := writer.Create(item.archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ValidateBundle(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	client, err := duitku.New("", "", "", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.SetExecutableSHA256(strings.Repeat("b", 64))
	verification := VerifyRuntimeContract(result.Manifest, client.Manifest())
	if result.Manifest.Code != "duitku" || result.Manifest.Version != client.Manifest().Version || len(result.Manifest.CredentialFields) != 2 {
		t.Fatalf("Duitku Provider App manifest diverged from runtime release: %#v", result.Manifest)
	}
	if !result.Report.Passed || result.Report.PackageFormat != PackageFormatSubmissionV1 || result.Report.FileCount != len(files) || !verification.Passed {
		t.Fatalf("Duitku submission failed release verification: submission=%#v verification=%#v", result, verification)
	}
}

func TestDOKUProviderAppManifestMatchesRuntimeRelease(t *testing.T) {
	files := []struct {
		archivePath string
		sourcePath  string
	}{
		{archivePath: "emisell-extension.yaml", sourcePath: "../../../provider-apps/doku/emisell-extension.yaml"},
		{archivePath: "openapi.yaml", sourcePath: "../../../provider-apps/doku/openapi.yaml"},
		{archivePath: "README.md", sourcePath: "../../../provider-apps/doku/README.md"},
		{archivePath: "SECURITY.md", sourcePath: "../../../provider-apps/doku/SECURITY.md"},
		{archivePath: "contract-tests/README.md", sourcePath: "../../../provider-apps/doku/contract-tests/README.md"},
		{archivePath: "src/doku/client.go", sourcePath: "../doku/client.go"},
		{archivePath: "src/doku/manifest.go", sourcePath: "../doku/manifest.go"},
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range files {
		content, err := os.ReadFile(item.sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		file, err := writer.Create(item.archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ValidateBundle(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	client, err := doku.New("", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.SetExecutableSHA256(strings.Repeat("c", 64))
	verification := VerifyRuntimeContract(result.Manifest, client.Manifest())
	if result.Manifest.Code != "doku" || result.Manifest.Version != client.Manifest().Version || len(result.Manifest.CredentialFields) != 2 || len(result.Manifest.PaymentMethods) != 20 {
		t.Fatalf("DOKU Provider App manifest diverged from runtime release: %#v", result.Manifest)
	}
	if !result.Report.Passed || result.Report.PackageFormat != PackageFormatSubmissionV1 || result.Report.FileCount != len(files) || !verification.Passed {
		t.Fatalf("DOKU submission failed release verification: submission=%#v verification=%#v", result, verification)
	}
}

func TestIPaymuProviderAppManifestMatchesRuntimeRelease(t *testing.T) {
	files := []struct {
		archivePath string
		sourcePath  string
	}{
		{archivePath: "emisell-extension.yaml", sourcePath: "../../../provider-apps/ipaymu/emisell-extension.yaml"},
		{archivePath: "openapi.yaml", sourcePath: "../../../provider-apps/ipaymu/openapi.yaml"},
		{archivePath: "README.md", sourcePath: "../../../provider-apps/ipaymu/README.md"},
		{archivePath: "SECURITY.md", sourcePath: "../../../provider-apps/ipaymu/SECURITY.md"},
		{archivePath: "contract-tests/README.md", sourcePath: "../../../provider-apps/ipaymu/contract-tests/README.md"},
		{archivePath: "src/ipaymu/client.go", sourcePath: "../ipaymu/client.go"},
		{archivePath: "src/ipaymu/manifest.go", sourcePath: "../ipaymu/manifest.go"},
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range files {
		content, err := os.ReadFile(item.sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		file, err := writer.Create(item.archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ValidateBundle(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	client, err := ipaymu.New("", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.SetExecutableSHA256(strings.Repeat("d", 64))
	verification := VerifyRuntimeContract(result.Manifest, client.Manifest())
	if result.Manifest.Code != "ipaymu" || result.Manifest.Version != client.Manifest().Version || len(result.Manifest.CredentialFields) != 2 || len(result.Manifest.PaymentMethods) != 18 {
		t.Fatalf("iPaymu Provider App manifest diverged from runtime release: %#v", result.Manifest)
	}
	if !result.Report.Passed || result.Report.PackageFormat != PackageFormatSubmissionV1 || result.Report.FileCount != len(files) || !verification.Passed {
		t.Fatalf("iPaymu submission failed release verification: submission=%#v verification=%#v", result, verification)
	}
}

func TestXenditSubmissionMatchesRuntimeRelease(t *testing.T) {
	files := []struct {
		archivePath string
		sourcePath  string
	}{
		{archivePath: "emisell-extension.yaml", sourcePath: "../../../provider-apps/xendit/emisell-extension.yaml"},
		{archivePath: "openapi.yaml", sourcePath: "../../../provider-apps/xendit/openapi.yaml"},
		{archivePath: "README.md", sourcePath: "../../../provider-apps/xendit/README.md"},
		{archivePath: "SECURITY.md", sourcePath: "../../../provider-apps/xendit/SECURITY.md"},
		{archivePath: "contract-tests/README.md", sourcePath: "../../../provider-apps/xendit/contract-tests/README.md"},
		{archivePath: "src/xendit/client.go", sourcePath: "../xendit/client.go"},
		{archivePath: "src/xendit/manifest.go", sourcePath: "../xendit/manifest.go"},
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range files {
		content, err := os.ReadFile(item.sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		file, err := writer.Create(item.archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := ValidateBundle(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	client, err := xendit.New("", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runtimeManifest := client.Manifest()
	if !result.Report.Passed || result.Report.FileCount != len(files) || result.Manifest.Code != runtimeManifest.Code || result.Manifest.Name != runtimeManifest.Name || result.Manifest.Version != runtimeManifest.Version || result.Manifest.Runtime != runtimeManifest.Runtime {
		t.Fatalf("Xendit submission diverged from runtime release: submission=%#v runtime=%#v", result.Manifest, runtimeManifest)
	}
}

func TestValidateBundleRejectsTraversal(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, _ := writer.Create("../manifest.json")
	_, _ = file.Write([]byte(`{}`))
	_ = writer.Close()
	if _, err := ValidateBundle(buffer.Bytes()); err == nil {
		t.Fatal("unsafe ZIP path was accepted")
	}
}

func TestValidateBundleRejectsSymlink(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: "manifest.json", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	file, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("connector"))
	_ = writer.Close()
	if _, err := ValidateBundle(buffer.Bytes()); err == nil {
		t.Fatal("symbolic link was accepted")
	}
}

func TestValidateBundleRejectsMissingChecksum(t *testing.T) {
	bundle := validBundle(t)
	reader, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, source := range reader.File {
		if source.Name == "checksums.txt" {
			continue
		}
		input, _ := source.Open()
		content := new(bytes.Buffer)
		_, _ = content.ReadFrom(input)
		_ = input.Close()
		output, _ := writer.Create(source.Name)
		_, _ = output.Write(content.Bytes())
	}
	_ = writer.Close()
	if _, err := ValidateBundle(buffer.Bytes()); err == nil {
		t.Fatal("bundle without checksums.txt was accepted")
	}
}
