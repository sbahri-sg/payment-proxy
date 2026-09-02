package providerapps

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

type ReleaseVerificationCheck struct {
	Code   string `json:"code"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type ReleaseVerificationReport struct {
	Passed               bool                       `json:"passed"`
	Source               string                     `json:"source"`
	RuntimeVersion       string                     `json:"runtime_version"`
	RuntimeDigest        string                     `json:"runtime_digest"`
	VerifiedAt           string                     `json:"verified_at,omitempty"`
	VerifiedCapabilities []string                   `json:"verified_capabilities"`
	Checks               []ReleaseVerificationCheck `json:"checks"`
}

func (r *ReleaseVerificationReport) AddCheck(code string, passed bool, detail string) {
	status := "PASSED"
	if !passed {
		status = "FAILED"
		r.Passed = false
	}
	r.Checks = append(r.Checks, ReleaseVerificationCheck{Code: code, Status: status, Detail: detail})
}

func VerifyRuntimeContract(release Manifest, runtime connector.Manifest) ReleaseVerificationReport {
	report := ReleaseVerificationReport{
		Passed:         true,
		Source:         "automated_backend_verification",
		RuntimeVersion: runtime.Version,
		RuntimeDigest:  runtime.ExecutableSHA256,
		Checks:         make([]ReleaseVerificationCheck, 0, 6+len(release.PaymentMethods)),
	}

	identityMatches := release.Code == runtime.Code && release.Name == runtime.Name && release.Version == runtime.Version && release.Runtime == runtime.Runtime
	report.AddCheck("runtime_identity", identityMatches, fmt.Sprintf("release=%s@%s runtime=%s@%s", release.Code, release.Version, runtime.Code, runtime.Version))
	report.AddCheck("runtime_digest", validSHA256(runtime.ExecutableSHA256), "running connector exposes a lowercase immutable SHA-256 digest")
	report.AddCheck("operations", equalStrings(operationNames(release.Operations), operationNames(runtime.Operations)), "release operations match the loaded runtime")
	report.AddCheck("credential_schema", equalStrings(credentialSignatures(release.CredentialFields), credentialSignatures(runtime.CredentialFields)), "merchant credential fields match the loaded runtime")

	releaseMethods := sortedStrings(release.PaymentMethods)
	releaseProfileMethods := profileNames(release.CertificationProfiles)
	runtimeProfileMethods := profileNames(runtime.CertificationProfiles)
	report.AddCheck("release_scope", equalStrings(releaseMethods, releaseProfileMethods), "every declared payment method has one backend test profile")
	report.AddCheck("runtime_scope", equalStrings(releaseMethods, runtimeProfileMethods), "loaded runtime exposes the exact declared payment-method scope")

	for _, method := range releaseMethods {
		releaseProfile, releaseOK := release.CertificationProfiles[method]
		runtimeProfile, runtimeOK := runtime.CertificationProfiles[method]
		passed := releaseOK && runtimeOK && releaseProfile.Automated && runtimeProfile.Automated && releaseProfile.Code == runtimeProfile.Code && releaseProfile.CheckoutMode == runtimeProfile.CheckoutMode
		report.AddCheck("profile:"+method, passed, fmt.Sprintf("release=%s runtime=%s", releaseProfile.Code, runtimeProfile.Code))
	}
	return report
}

func validSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func operationNames(values []connector.Operation) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return sortedStrings(result)
}

func credentialSignatures(values []connector.CredentialField) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, fmt.Sprintf("%s|%s|%t|%t", value.Code, value.InputType, value.Secret, value.Required))
	}
	return sortedStrings(result)
}

func profileNames(values map[string]connector.CertificationProfile) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return sortedStrings(result)
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
