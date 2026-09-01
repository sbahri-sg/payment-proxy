package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadOnlyLoadRunner(t *testing.T) {
	t.Setenv("SERVICE_API_KEY", "test-service-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedEnvironment := ""
		if r.URL.Path == "/api/v1/provider-options" {
			expectedEnvironment = "sandbox"
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-service-key" || r.Header.Get("X-Emisell-Execution-Mode") != "" || r.URL.Query().Get("environment") != expectedEnvironment {
			t.Errorf("unsafe or unauthenticated load request: %s %#v", r.Method, r.Header)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, path := range []string{"/api/v1/payment-options", "/api/v1/provider-options"} {
		if err := run(server.URL, path, 50, 5, time.Second, time.Second, 0); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if err := run(server.URL, "/api/v1/payment-sessions", 1, 1, time.Second, time.Second, 0); err == nil {
		t.Fatal("mutation endpoint was accepted by the load runner")
	}
}
