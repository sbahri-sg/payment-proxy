package observability

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

var latencyBounds = [...]time.Duration{
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
	10 * time.Second,
}

type Metrics struct {
	startedAt time.Time
	inFlight  atomic.Int64
	requests  atomic.Uint64
	status2xx atomic.Uint64
	status3xx atomic.Uint64
	status4xx atomic.Uint64
	status5xx atomic.Uint64
	duration  atomic.Uint64
	buckets   [len(latencyBounds)]atomic.Uint64

	connectorUnknown      atomic.Uint64
	connectorNotSupported atomic.Uint64
	connectorRejected     atomic.Uint64
	webhookAccepted       atomic.Uint64
	webhookDuplicate      atomic.Uint64
	webhookInvalid        atomic.Uint64
}

type Snapshot struct {
	StartedAt       time.Time       `json:"started_at"`
	UptimeSeconds   int64           `json:"uptime_seconds"`
	InFlight        int64           `json:"in_flight"`
	RequestsTotal   uint64          `json:"requests_total"`
	Responses       ResponseTotals  `json:"responses"`
	Latency         LatencySnapshot `json:"latency"`
	Connector       ConnectorTotals `json:"connector_outcomes"`
	ProviderWebhook WebhookTotals   `json:"provider_webhooks"`
	SLO             SLOSnapshot     `json:"slo"`
}

type ResponseTotals struct {
	Status2xx uint64 `json:"status_2xx"`
	Status3xx uint64 `json:"status_3xx"`
	Status4xx uint64 `json:"status_4xx"`
	Status5xx uint64 `json:"status_5xx"`
}

type LatencySnapshot struct {
	AverageMilliseconds float64 `json:"average_ms"`
	P95Milliseconds     int64   `json:"p95_ms"`
}

type ConnectorTotals struct {
	UnknownOutcome uint64 `json:"unknown_outcome"`
	NotSupported   uint64 `json:"not_supported"`
	Rejected       uint64 `json:"rejected"`
}

type WebhookTotals struct {
	Accepted  uint64 `json:"accepted"`
	Duplicate uint64 `json:"duplicate"`
	Invalid   uint64 `json:"invalid"`
}

type SLOSnapshot struct {
	Status                    string  `json:"status"`
	AvailabilityTargetPercent float64 `json:"availability_target_percent"`
	AvailabilityPercent       float64 `json:"availability_percent"`
	LatencyP95TargetMS        int64   `json:"latency_p95_target_ms"`
	LatencyP95MS              int64   `json:"latency_p95_ms"`
}

func New() *Metrics {
	return &Metrics{startedAt: time.Now().UTC()}
}

func (m *Metrics) RequestStarted() {
	m.inFlight.Add(1)
}

func (m *Metrics) RequestFinished(status int, elapsed time.Duration) {
	m.inFlight.Add(-1)
	m.requests.Add(1)
	switch status / 100 {
	case 2:
		m.status2xx.Add(1)
	case 3:
		m.status3xx.Add(1)
	case 4:
		m.status4xx.Add(1)
	case 5:
		m.status5xx.Add(1)
	}
	if elapsed < 0 {
		elapsed = 0
	}
	m.duration.Add(uint64(elapsed.Nanoseconds()))
	for index, bound := range latencyBounds {
		if elapsed <= bound {
			m.buckets[index].Add(1)
		}
	}
}

func (m *Metrics) RecordConnectorOutcome(outcome string) {
	switch outcome {
	case "unknown":
		m.connectorUnknown.Add(1)
	case "not_supported":
		m.connectorNotSupported.Add(1)
	case "rejected":
		m.connectorRejected.Add(1)
	}
}

func (m *Metrics) RecordProviderWebhook(outcome string) {
	switch outcome {
	case "accepted":
		m.webhookAccepted.Add(1)
	case "duplicate":
		m.webhookDuplicate.Add(1)
	case "invalid":
		m.webhookInvalid.Add(1)
	}
}

func (m *Metrics) Snapshot() Snapshot {
	total := m.requests.Load()
	status5xx := m.status5xx.Load()
	availability := 100.0
	if total > 0 {
		availability = 100 * float64(total-status5xx) / float64(total)
	}
	p95 := m.percentileMilliseconds(total, 0.95)
	average := 0.0
	if total > 0 {
		average = float64(m.duration.Load()) / float64(total) / float64(time.Millisecond)
	}
	sloStatus := "NO_DATA"
	if total > 0 {
		sloStatus = "MEETING"
		if availability < 99.9 || p95 > 500 {
			sloStatus = "BREACHED"
		}
	}
	return Snapshot{
		StartedAt:     m.startedAt,
		UptimeSeconds: int64(time.Since(m.startedAt).Seconds()),
		InFlight:      m.inFlight.Load(),
		RequestsTotal: total,
		Responses: ResponseTotals{
			Status2xx: m.status2xx.Load(),
			Status3xx: m.status3xx.Load(),
			Status4xx: m.status4xx.Load(),
			Status5xx: status5xx,
		},
		Latency: LatencySnapshot{AverageMilliseconds: math.Round(average*100) / 100, P95Milliseconds: p95},
		Connector: ConnectorTotals{
			UnknownOutcome: m.connectorUnknown.Load(),
			NotSupported:   m.connectorNotSupported.Load(),
			Rejected:       m.connectorRejected.Load(),
		},
		ProviderWebhook: WebhookTotals{
			Accepted:  m.webhookAccepted.Load(),
			Duplicate: m.webhookDuplicate.Load(),
			Invalid:   m.webhookInvalid.Load(),
		},
		SLO: SLOSnapshot{
			Status: sloStatus, AvailabilityTargetPercent: 99.9, AvailabilityPercent: math.Round(availability*1000) / 1000,
			LatencyP95TargetMS: 500, LatencyP95MS: p95,
		},
	}
}

func (m *Metrics) percentileMilliseconds(total uint64, percentile float64) int64 {
	if total == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(total) * percentile))
	for index, bound := range latencyBounds {
		if m.buckets[index].Load() >= target {
			return bound.Milliseconds()
		}
	}
	return latencyBounds[len(latencyBounds)-1].Milliseconds()
}

func (m *Metrics) Prometheus() string {
	snapshot := m.Snapshot()
	var output strings.Builder
	fmt.Fprintf(&output, "# HELP emisell_http_requests_total HTTP requests handled by response class.\n")
	fmt.Fprintf(&output, "# TYPE emisell_http_requests_total counter\n")
	fmt.Fprintf(&output, "emisell_http_requests_total{class=\"2xx\"} %d\n", snapshot.Responses.Status2xx)
	fmt.Fprintf(&output, "emisell_http_requests_total{class=\"3xx\"} %d\n", snapshot.Responses.Status3xx)
	fmt.Fprintf(&output, "emisell_http_requests_total{class=\"4xx\"} %d\n", snapshot.Responses.Status4xx)
	fmt.Fprintf(&output, "emisell_http_requests_total{class=\"5xx\"} %d\n", snapshot.Responses.Status5xx)
	fmt.Fprintf(&output, "# TYPE emisell_http_requests_in_flight gauge\n")
	fmt.Fprintf(&output, "emisell_http_requests_in_flight %d\n", snapshot.InFlight)
	fmt.Fprintf(&output, "# TYPE emisell_http_request_duration_seconds histogram\n")
	for index, bound := range latencyBounds {
		fmt.Fprintf(&output, "emisell_http_request_duration_seconds_bucket{le=\"%g\"} %d\n", bound.Seconds(), m.buckets[index].Load())
	}
	fmt.Fprintf(&output, "emisell_http_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", snapshot.RequestsTotal)
	fmt.Fprintf(&output, "emisell_http_request_duration_seconds_sum %g\n", float64(m.duration.Load())/float64(time.Second))
	fmt.Fprintf(&output, "emisell_http_request_duration_seconds_count %d\n", snapshot.RequestsTotal)
	fmt.Fprintf(&output, "# TYPE emisell_connector_outcomes_total counter\n")
	fmt.Fprintf(&output, "emisell_connector_outcomes_total{outcome=\"unknown\"} %d\n", snapshot.Connector.UnknownOutcome)
	fmt.Fprintf(&output, "emisell_connector_outcomes_total{outcome=\"not_supported\"} %d\n", snapshot.Connector.NotSupported)
	fmt.Fprintf(&output, "emisell_connector_outcomes_total{outcome=\"rejected\"} %d\n", snapshot.Connector.Rejected)
	fmt.Fprintf(&output, "# TYPE emisell_provider_webhooks_total counter\n")
	fmt.Fprintf(&output, "emisell_provider_webhooks_total{outcome=\"accepted\"} %d\n", snapshot.ProviderWebhook.Accepted)
	fmt.Fprintf(&output, "emisell_provider_webhooks_total{outcome=\"duplicate\"} %d\n", snapshot.ProviderWebhook.Duplicate)
	fmt.Fprintf(&output, "emisell_provider_webhooks_total{outcome=\"invalid\"} %d\n", snapshot.ProviderWebhook.Invalid)
	return output.String()
}
