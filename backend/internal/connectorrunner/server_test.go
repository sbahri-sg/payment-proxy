package connectorrunner

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

func TestInvalidCredentialErrorContract(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).connectorError(response, connector.ErrInvalidCredential)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"INVALID_PROVIDER_CREDENTIAL"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestHostedPaymentRestrictionErrorContract(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).connectorError(response, connector.ErrHostedPaymentRestrictionUnsupported)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"code":"HOSTED_PAYMENT_METHOD_RESTRICTION_UNSUPPORTED"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}
