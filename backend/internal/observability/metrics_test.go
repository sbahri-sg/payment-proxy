package observability

import (
	"strings"
	"testing"
	"time"
)

func TestMetricsSnapshotAndSLO(t *testing.T) {
	metrics := New()
	for index := 0; index < 99; index++ {
		metrics.RequestStarted()
		metrics.RequestFinished(200, 20*time.Millisecond)
	}
	metrics.RequestStarted()
	metrics.RequestFinished(500, 800*time.Millisecond)
	metrics.RecordConnectorOutcome("unknown")
	metrics.RecordProviderWebhook("accepted")
	metrics.RecordProviderWebhook("duplicate")
	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != 100 || snapshot.Responses.Status5xx != 1 || snapshot.InFlight != 0 {
		t.Fatalf("unexpected request metrics: %#v", snapshot)
	}
	if snapshot.SLO.Status != "BREACHED" || snapshot.SLO.AvailabilityPercent != 99 || snapshot.SLO.LatencyP95MS != 25 {
		t.Fatalf("unexpected SLO snapshot: %#v", snapshot.SLO)
	}
	if snapshot.Connector.UnknownOutcome != 1 || snapshot.ProviderWebhook.Accepted != 1 || snapshot.ProviderWebhook.Duplicate != 1 {
		t.Fatalf("unexpected operational counters: %#v", snapshot)
	}
	text := metrics.Prometheus()
	for _, marker := range []string{"emisell_http_requests_total", "emisell_http_request_duration_seconds_bucket", "emisell_connector_outcomes_total", "emisell_provider_webhooks_total"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("Prometheus output is missing %s", marker)
		}
	}
}

func TestMetricsNoDataIsExplicit(t *testing.T) {
	snapshot := New().Snapshot()
	if snapshot.SLO.Status != "NO_DATA" || snapshot.SLO.AvailabilityPercent != 100 || snapshot.SLO.LatencyP95MS != 0 {
		t.Fatalf("unexpected empty snapshot: %#v", snapshot)
	}
}
