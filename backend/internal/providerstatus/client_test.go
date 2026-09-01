package providerstatus

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/model"
)

func TestRunRefreshesImmediatelyAndContinuesUntilCancelled(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"page":{"updated_at":"2026-09-01T13:00:00Z"},"status":{"indicator":"none","description":"All Systems Operational"},"components":[],"incidents":[],"scheduled_maintenances":[]}`)
	}))
	defer server.Close()

	client := &Client{
		http: server.Client(),
		sources: []source{{
			Code: "xendit", Name: "Xendit", PageURL: server.URL,
			SummaryURL: server.URL, Kind: "statuspage_v2",
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.Run(ctx, 20*time.Millisecond, 100*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()

	deadline := time.NewTimer(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for requests.Load() < 2 {
		select {
		case <-deadline.C:
			t.Fatalf("background refresher made %d request(s), want at least 2", requests.Load())
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background refresher did not stop after cancellation")
	}
}

func TestBackoffAndJitterRemainBounded(t *testing.T) {
	if got := boundedBackoff(time.Minute, 1, 5*time.Minute); got != 2*time.Minute {
		t.Fatalf("boundedBackoff() = %s, want 2m", got)
	}
	if got := boundedBackoff(time.Minute, 10, 5*time.Minute); got != 5*time.Minute {
		t.Fatalf("boundedBackoff() = %s, want 5m", got)
	}
	for index := 0; index < 100; index++ {
		got := negativeJitter(time.Minute)
		if got < 54*time.Second || got > time.Minute {
			t.Fatalf("negativeJitter() = %s, want 54s..60s", got)
		}
	}
}

func TestParseStatuspageFiltersIrrelevantRegionalMaintenance(t *testing.T) {
	body := []byte(`{
  "page":{"updated_at":"2026-09-01T19:01:07+07:00"},
  "status":{"indicator":"none","description":"All Systems Operational"},
  "components":[
    {"id":"id-payments","name":"Indonesia - Payments","status":"operational","group":true,"group_id":null},
    {"id":"qris","name":"QRIS","status":"operational","group":false,"group_id":"id-payments"},
    {"id":"ph-payments","name":"Philippines - Payments","status":"operational","group":true,"group_id":null},
    {"id":"bpi","name":"BPI Direct Debit","status":"under_maintenance","group":false,"group_id":"ph-payments"}
  ],
  "incidents":[],
  "scheduled_maintenances":[{
    "id":"maintenance-bpi","name":"BPI maintenance","status":"scheduled","impact":"maintenance",
    "created_at":"2026-09-01T10:00:00+07:00","updated_at":"2026-09-01T10:00:00+07:00",
    "components":[{"id":"bpi","name":"BPI Direct Debit","status":"under_maintenance","group":false,"group_id":"ph-payments"}]
  }]
}`)
	result, err := parseStatuspage(body, officialSources[0])
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusAvailable || len(result.Components) != 2 {
		t.Fatalf("unexpected filtered status: %#v", result)
	}
	if len(result.Maintenance) != 0 {
		t.Fatalf("Philippines maintenance leaked into Indonesia status: %#v", result.Maintenance)
	}
}

func TestParseMidtransHTMLKeepsPaymentComponentsAndMaintenance(t *testing.T) {
	body := []byte(`<div class="status-head clearfix"><h2>All Systems Operational</h2><span class="s-h2-sub">Updated now</span></div>
<div class="status-list"><h3>Core API</h3><span class="status-badge">Primary Data Center</span><div class="status-opt">Operational</div></div>
<div class="status-list"><h3>Disbursement - BCA</h3><span class="status-badge">Disbursement (Payouts)</span><div class="status-opt">Operational</div></div>
<div class="status-sch"><div class="status-sch-head clearfix"><h3>Sandbox maintenance</h3><span>Planned Maintenance</span></div>
<div class="status-sch-main"><div class="status-sch-c clearfix"><div class="status-sch-c-title">Schedule</div><div class="status-sch-c-content">3 September 2026 21:00 WIB</div></div>
<div class="status-sch-c clearfix"><div class="status-sch-c-title">Components</div><div class="status-sch-c-content">[Sandbox] Core API, GO-PAY</div></div>
<div class="status-sch-c clearfix"><div class="status-sch-c-title">Descriptions</div><div class="status-sch-c-content">Partial disruption is possible.</div></div></div></div>`)
	result, err := parseMidtransHTML(body, officialSources[1])
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusAvailable || len(result.Components) != 1 || result.Components[0].Name != "Core API" {
		t.Fatalf("unexpected Midtrans components: %#v", result)
	}
	if len(result.Maintenance) != 1 || result.Maintenance[0].Name != "Sandbox maintenance" {
		t.Fatalf("Midtrans maintenance was not parsed: %#v", result.Maintenance)
	}
}

func TestParseMidtransHTMLDoesNotTreatScheduledMaintenanceAsCurrentDisruption(t *testing.T) {
	body := []byte(`<div class="status-head clearfix"><span class="s-h2-sub">Updated now</span></div>
<div class="status-list"><h3>SNAP</h3><span class="status-badge">Primary Data Center</span><div class="status-opt">Operational</div></div>
<div class="status-list"><h3>Core API</h3><span class="status-badge">Primary Data Center</span><div class="status-opt">Operational</div></div>
<div class="status-head clearfix"><h2>Scheduled Maintenance</h2><span class="s-h2-sub">Updated now</span></div>
<div class="status-sch"><div class="status-sch-head clearfix"><h3>Sandbox maintenance on 3 September</h3><span>Planned Maintenance</span></div>
<div class="status-sch-main"><div class="status-sch-c clearfix"><div class="status-sch-c-title">Schedule</div><div class="status-sch-c-content">September 03, 2026 21:00 PM - 23:59 PM +07</div></div>
<div class="status-sch-c clearfix"><div class="status-sch-c-title">Components</div><div class="status-sch-c-content">[Sandbox] SNAP, GO-PAY</div></div>
<div class="status-sch-c clearfix"><div class="status-sch-c-title">Descriptions</div><div class="status-sch-c-content">Partial disruption is possible.</div></div></div></div>`)
	result, err := parseMidtransHTML(body, officialSources[1])
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusAvailable || result.Description != "All systems operational" {
		t.Fatalf("future maintenance changed current Midtrans status: %#v", result)
	}
	if len(result.Maintenance) != 1 {
		t.Fatalf("scheduled maintenance should remain visible: %#v", result.Maintenance)
	}
}

func TestParseDOKUStatusMapsActiveIncidentAndFutureMaintenance(t *testing.T) {
	services := []byte(`{"services":[
  {"id":"cards","business_service_id":"cards-business","display_name":"Cards","is_active":true},
  {"id":"qris","business_service_id":"qris-business","display_name":"QRIS","is_active":true},
  {"id":"payout","business_service_id":"payout-business","display_name":"Disbursement","is_active":true}
]}`)
	posts := []byte(`{"posts":[
  {"id":"incident-card","title":"Card disruption","post_type":"incident","latest_update":{"status_id":"P0D2P8P","severity_id":"PCQCO3G","reported_at":1788249600000,"message":"<p>Card transactions are intermittent.</p>","impacts":[{"service_id":"cards","severity_id":"PRVF8MD"}]}},
  {"id":"maintenance-qris","title":"QRIS maintenance","post_type":"maintenance","starts_at":1788292800000,"ends_at":1788293700000,"latest_update":{"status_id":"P7N67Y0","severity_id":"PFOV5E4","reported_at":1788162400000,"message":"Scheduled maintenance.","impacts":[{"service_id":"qris","severity_id":"PY2J4PR"}]}},
  {"id":"payout-incident","title":"Payout disruption","post_type":"incident","latest_update":{"status_id":"P0D2P8P","severity_id":"PYERNRT","reported_at":1788249600000,"message":"Payout unavailable.","impacts":[{"service_id":"payout","severity_id":"PNJU8NQ"}]}}
]}`)
	item := source{Code: "doku", Name: "DOKU", PageURL: "https://status.doku.com/posts/dashboard", Kind: "pagerduty_status_dashboard"}
	result, err := parseDOKUStatus(services, posts, item, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusDegraded || result.Description != "Partial outage" {
		t.Fatalf("unexpected DOKU status: %#v", result)
	}
	if len(result.Components) != 2 || len(result.ActiveIncidents) != 1 || result.ActiveIncidents[0].Name != "Card disruption" {
		t.Fatalf("unexpected DOKU incident mapping: %#v", result)
	}
	if len(result.Maintenance) != 1 || result.Maintenance[0].ScheduledFor == nil {
		t.Fatalf("DOKU maintenance was not parsed: %#v", result.Maintenance)
	}
}

func TestParseIPaymuStatusMapsOfflineAndMaintenancePaymentChannels(t *testing.T) {
	body := []byte(`{"Status":200,"Data":{"HealthCheck":[
  {"Code":"cc","Type":"Payment Channel","Name":"Credit Card","StatusText":"Online","UpdatedAt":"01/09/2026 20:20 WIB"},
  {"Code":"debitonline","Type":"Payment Channel","Name":"Debit Online","StatusText":"Offline","UpdatedAt":"01/09/2026 20:20 WIB"},
  {"Code":"bsiva","Type":"Payment Channel","Name":"VA BSI","StatusText":"Maintenance","UpdatedAt":"16/07/2026 13:19 WIB"},
  {"Code":"withdraw","Type":"Withdraw","Name":"Withdraw","StatusText":"Maintenance","UpdatedAt":"01/09/2026 20:20 WIB"}
]}}`)
	item := source{Code: "ipaymu", Name: "iPaymu", PageURL: "https://status.ipaymu.com", Kind: "ipaymu_healthcheck"}
	result, err := parseIPaymuStatus(body, item)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusDegraded || result.Description != "2 payment channels affected" {
		t.Fatalf("unexpected iPaymu status: %#v", result)
	}
	if len(result.Components) != 3 || len(result.ActiveIncidents) != 1 || result.ActiveIncidents[0].Name != "Debit Online" {
		t.Fatalf("unexpected iPaymu incident mapping: %#v", result)
	}
	if len(result.Maintenance) != 1 || result.Maintenance[0].Name != "VA BSI" {
		t.Fatalf("unexpected iPaymu maintenance mapping: %#v", result.Maintenance)
	}
}

func TestCheckoutRestrictionsMapAffectedComponentsAndFailOpen(t *testing.T) {
	client := &Client{
		cached: []model.OfficialProviderStatus{
			{ProviderCode: "doku", Status: statusDegraded, SourceAvailable: true, Components: []model.OfficialProviderComponent{{Name: "Cards", Status: statusDegraded}, {Name: "QRIS", Status: statusAvailable}}},
			{ProviderCode: "ipaymu", Status: statusDegraded, SourceAvailable: true, Components: []model.OfficialProviderComponent{{Name: "VA BSI", Status: statusDegraded}, {Name: "Debit Online", Status: statusUnavailable}}},
			{ProviderCode: "xendit", Status: statusUnknown, SourceAvailable: false},
		},
		expiresAt: time.Now().Add(time.Minute),
	}
	doku := client.CheckoutRestrictions(context.Background(), "doku", "live")
	if doku.ProviderUnavailable || len(doku.Methods) != 1 || doku.Methods[0].PaymentMethodCode != "card" || doku.Methods[0].Reason != "OFFICIAL_PROVIDER_MAINTENANCE" {
		t.Fatalf("unexpected DOKU checkout restrictions: %#v", doku)
	}
	ipaymu := client.CheckoutRestrictions(context.Background(), "ipaymu", "live")
	if ipaymu.ProviderUnavailable || len(ipaymu.Methods) != 1 || ipaymu.Methods[0].PaymentMethodCode != "va_bsi" {
		t.Fatalf("unexpected iPaymu checkout restrictions: %#v", ipaymu)
	}
	xendit := client.CheckoutRestrictions(context.Background(), "xendit", "live")
	if xendit.ProviderUnavailable || len(xendit.Methods) != 0 {
		t.Fatalf("unknown source must fail open: %#v", xendit)
	}
}

func TestCheckoutRestrictionsBlockProviderWideCheckoutComponent(t *testing.T) {
	client := &Client{
		cached: []model.OfficialProviderStatus{{
			ProviderCode: "doku", Status: statusDegraded, SourceAvailable: true,
			Components: []model.OfficialProviderComponent{{Name: "Checkout Page", Status: statusUnavailable}},
		}},
		expiresAt: time.Now().Add(time.Minute),
	}
	result := client.CheckoutRestrictions(context.Background(), "doku", "live")
	if !result.ProviderUnavailable || result.ProviderReason != "OFFICIAL_PROVIDER_OFFLINE" {
		t.Fatalf("provider-wide checkout outage was not enforced: %#v", result)
	}
}
