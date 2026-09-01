package providerapps

import (
	"testing"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

func TestVerifyRuntimeContractPassesMatchingRelease(t *testing.T) {
	profile := connector.CertificationProfile{Code: "provider/qris", Automated: true}
	release := Manifest{
		Code: "provider", Name: "Provider", Version: "1.0.0", Runtime: "isolated_container",
		Operations:            []connector.Operation{connector.OperationVerifyInstallation, connector.OperationCreatePayment},
		CredentialFields:      []connector.CredentialField{{Code: "api_key", InputType: "password", Secret: true, Required: true}},
		CertificationProfiles: map[string]connector.CertificationProfile{"qris": profile}, PaymentMethods: []string{"qris"},
	}
	runtime := connector.Manifest{
		Code: "provider", Name: "Provider", Version: "1.0.0", Runtime: "isolated_container", ExecutableSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Operations:            []connector.Operation{connector.OperationCreatePayment, connector.OperationVerifyInstallation},
		CredentialFields:      []connector.CredentialField{{Code: "api_key", InputType: "password", Secret: true, Required: true}},
		CertificationProfiles: map[string]connector.CertificationProfile{"qris": profile},
	}
	report := VerifyRuntimeContract(release, runtime)
	if !report.Passed {
		t.Fatalf("expected matching release to pass: %#v", report.Checks)
	}
}

func TestVerifyRuntimeContractRejectsRuntimeScopeDrift(t *testing.T) {
	release := Manifest{
		Code: "provider", Name: "Provider", Version: "1.0.0", Runtime: "isolated_container",
		Operations:            []connector.Operation{connector.OperationCreatePayment},
		CertificationProfiles: map[string]connector.CertificationProfile{"qris": {Code: "provider/qris", Automated: true}}, PaymentMethods: []string{"qris"},
	}
	runtime := connector.Manifest{
		Code: "provider", Name: "Provider", Version: "1.0.0", Runtime: "isolated_container", ExecutableSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Operations:            []connector.Operation{connector.OperationCreatePayment},
		CertificationProfiles: map[string]connector.CertificationProfile{"qris": {Code: "provider/qris-v2", Automated: true}},
	}
	report := VerifyRuntimeContract(release, runtime)
	if report.Passed {
		t.Fatal("expected profile drift to fail backend verification")
	}
}

func TestVerifyRuntimeContractRejectsInvalidRuntimeDigest(t *testing.T) {
	profile := connector.CertificationProfile{Code: "provider/qris", Automated: true}
	release := Manifest{
		Code: "provider", Name: "Provider", Version: "1.0.0", Runtime: "isolated_container",
		Operations: []connector.Operation{connector.OperationCreatePayment}, CertificationProfiles: map[string]connector.CertificationProfile{"qris": profile}, PaymentMethods: []string{"qris"},
	}
	runtime := connector.Manifest{
		Code: "provider", Name: "Provider", Version: "1.0.0", Runtime: "isolated_container", ExecutableSHA256: "not-a-sha256",
		Operations: []connector.Operation{connector.OperationCreatePayment}, CertificationProfiles: map[string]connector.CertificationProfile{"qris": profile},
	}
	if report := VerifyRuntimeContract(release, runtime); report.Passed {
		t.Fatal("expected an invalid runtime digest to fail backend verification")
	}
}
