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
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-service-key" || r.Header.Get("X-Emisell-Execution-Mode") != "sandbox" {
			t.Errorf("unsafe or unauthenticated load request: %s %#v", r.Method, r.Header)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := run(server.URL, "/api/v1/payment-options", 50, 5, time.Second, time.Second, 0); err != nil {
		t.Fatal(err)
	}
	if err := run(server.URL, "/api/v1/payment-sessions", 1, 1, time.Second, time.Second, 0); err == nil {
		t.Fatal("mutation endpoint was accepted by the load runner")
	}
}
