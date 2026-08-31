package providerapps

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/midtrans"
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
	if !result.Report.Passed || result.Manifest.Code != "midtrans" || result.Manifest.Version != "0.1.0" || result.ArtifactSHA256 == "" || result.Report.EntrypointSHA256 != hex.EncodeToString(entrypointHash[:]) {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestMidtransProviderAppManifestMatchesRuntimeRelease(t *testing.T) {
	manifest, err := os.ReadFile("../../../provider-apps/midtrans/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	entrypoint := []byte("connector-test-placeholder")
	manifestHash := sha256.Sum256(manifest)
	entrypointHash := sha256.Sum256(entrypoint)
	checksums := []byte(hex.EncodeToString(manifestHash[:]) + "  manifest.json\n" + hex.EncodeToString(entrypointHash[:]) + "  connector\n")
	files := map[string][]byte{"manifest.json": manifest, "connector": entrypoint, "checksums.txt": checksums}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range []string{"manifest.json", "connector", "checksums.txt"} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(files[name]); err != nil {
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
	if result.Manifest.Code != "midtrans" || result.Manifest.Version != client.Manifest().Version || len(result.Manifest.CredentialFields) != 2 {
		t.Fatalf("Midtrans Provider App manifest diverged from runtime release: %#v", result.Manifest)
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
