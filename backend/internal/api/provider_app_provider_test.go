package api

import (
	"testing"

	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/model"
)

func TestOptionalPublicHTTPSURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "optional", value: "", want: true},
		{name: "public domain", value: "https://docs.midtrans.com/reference", want: true},
		{name: "public ip", value: "https://8.8.8.8/docs", want: true},
		{name: "http", value: "http://docs.midtrans.com", want: false},
		{name: "credentials", value: "https://user:pass@docs.midtrans.com", want: false},
		{name: "fragment", value: "https://docs.midtrans.com/#secret", want: false},
		{name: "localhost", value: "https://localhost/docs", want: false},
		{name: "local suffix", value: "https://provider.local/docs", want: false},
		{name: "private ipv4", value: "https://10.10.10.10/docs", want: false},
		{name: "loopback ipv6", value: "https://[::1]/docs", want: false},
		{name: "single label", value: "https://provider/docs", want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := optionalPublicHTTPSURL(test.value); got != test.want {
				t.Fatalf("optionalPublicHTTPSURL(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestWithProviderWebhookURL(t *testing.T) {
	t.Parallel()
	server := &Server{cfg: config.Config{PublicBaseURL: "https://payments.example.com/"}}
	installation := server.withProviderWebhookURL(model.Installation{ID: "ins_test", ProviderCode: "midtrans"})
	if installation.PublicWebhookURL != "https://payments.example.com/webhooks/v1/providers/midtrans/ins_test" {
		t.Fatalf("unexpected provider webhook URL: %q", installation.PublicWebhookURL)
	}
}
