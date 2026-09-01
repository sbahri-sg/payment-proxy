package providerstatus

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emisell/api-payment-proxy/internal/model"
)

const (
	statusAvailable   = "AVAILABLE"
	statusDegraded    = "DEGRADED"
	statusUnavailable = "UNAVAILABLE"
	statusUnknown     = "UNKNOWN"
	cacheTTL          = time.Minute
	maxStatusBody     = 4 << 20
)

type source struct {
	Code, Name, PageURL, SummaryURL, IncidentsURL, Kind string
}

var officialSources = []source{
	{Code: "xendit", Name: "Xendit", PageURL: "https://status.xendit.co", SummaryURL: "https://status.xendit.co/api/v2/summary.json", IncidentsURL: "https://status.xendit.co/api/v2/incidents.json", Kind: "statuspage_v2"},
	{Code: "midtrans", Name: "Midtrans", PageURL: "https://midtrans.com/id/status", SummaryURL: "https://midtrans.com/id/status", Kind: "midtrans_html"},
	{Code: "duitku", Name: "Duitku", PageURL: "https://duitku.statuspage.io", SummaryURL: "https://duitku.statuspage.io/api/v2/summary.json", IncidentsURL: "https://duitku.statuspage.io/api/v2/incidents.json", Kind: "statuspage_v2"},
	{Code: "doku", Name: "DOKU", PageURL: "https://status.doku.com/posts/dashboard", SummaryURL: "https://status.doku.com/api/services", IncidentsURL: "https://status.doku.com/api/posts?is_featured=true&limit=500", Kind: "pagerduty_status_dashboard"},
	{Code: "ipaymu", Name: "iPaymu", PageURL: "https://status.ipaymu.com", SummaryURL: "https://my.ipaymu.com/api/v2/healthcheck", Kind: "ipaymu_healthcheck"},
}

var allowedStatusHosts = map[string]struct{}{
	"status.xendit.co":                       {},
	"midtrans.com":                           {},
	"www.midtrans.com":                       {},
	"midtrans-website.i.p-sg-mp-01.gopay.sh": {},
	"duitku.statuspage.io":                   {},
	"status.doku.com":                        {},
	"status.ipaymu.com":                      {},
	"my.ipaymu.com":                          {},
}

type Client struct {
	http      *http.Client
	sources   []source
	mu        sync.Mutex
	cached    []model.OfficialProviderStatus
	expiresAt time.Time
}

type CheckoutMethodRestriction struct {
	PaymentMethodCode string
	Reason            string
}

type CheckoutRestrictions struct {
	ProviderUnavailable bool
	ProviderReason      string
	Methods             []CheckoutMethodRestriction
}

func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 8
	transport.MaxIdleConnsPerHost = 2
	return &Client{sources: append([]source(nil), officialSources...), http: &http.Client{
		Timeout:   6 * time.Second,
		Transport: transport,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if _, allowed := allowedStatusHosts[strings.ToLower(request.URL.Hostname())]; !allowed {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}}
}

func (c *Client) Snapshot(ctx context.Context) []model.OfficialProviderStatus {
	c.mu.Lock()
	if len(c.cached) > 0 && time.Now().Before(c.expiresAt) {
		result := cloneStatuses(c.cached)
		c.mu.Unlock()
		return result
	}
	c.mu.Unlock()
	return c.Refresh(ctx)
}

func (c *Client) Refresh(ctx context.Context) []model.OfficialProviderStatus {
	sources := c.sources
	if len(sources) == 0 {
		sources = officialSources
	}
	result := make([]model.OfficialProviderStatus, len(sources))
	var wg sync.WaitGroup
	for index, item := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result[index] = c.fetch(ctx, item)
		}()
	}
	wg.Wait()
	sort.Slice(result, func(i, j int) bool { return result[i].ProviderName < result[j].ProviderName })
	c.mu.Lock()
	c.cached, c.expiresAt = cloneStatuses(result), time.Now().Add(cacheTTL)
	c.mu.Unlock()
	return result
}

// Run refreshes the public provider-status cache independently from merchant
// traffic. It performs one immediate refresh, polls with a small negative
// jitter to avoid synchronized replica bursts, and backs off only when every
// official source is unavailable.
func (c *Client) Run(ctx context.Context, interval, fetchTimeout time.Duration, logger *slog.Logger) {
	if interval <= 0 {
		interval = time.Minute
	}
	if fetchTimeout <= 0 {
		fetchTimeout = 8 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	consecutiveTotalFailures := 0
	for {
		refreshContext, cancel := context.WithTimeout(ctx, fetchTimeout)
		statuses := c.Refresh(refreshContext)
		cancel()
		if ctx.Err() != nil {
			return
		}
		availableSources, degraded, unavailable := statusCounts(statuses)
		logger.Info("official provider status refreshed",
			"sources", len(statuses), "available_sources", availableSources,
			"degraded", degraded, "unavailable", unavailable,
		)
		nextInterval := interval
		if availableSources == 0 {
			consecutiveTotalFailures++
			nextInterval = boundedBackoff(interval, consecutiveTotalFailures, 5*interval)
			logger.Warn("all official provider status sources unavailable", "retry_in", nextInterval)
		} else {
			consecutiveTotalFailures = 0
		}
		timer := time.NewTimer(negativeJitter(nextInterval))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

func statusCounts(statuses []model.OfficialProviderStatus) (availableSources, degraded, unavailable int) {
	for _, status := range statuses {
		if status.SourceAvailable {
			availableSources++
		}
		switch status.Status {
		case statusDegraded:
			degraded++
		case statusUnavailable:
			unavailable++
		}
	}
	return availableSources, degraded, unavailable
}

func boundedBackoff(base time.Duration, failures int, maximum time.Duration) time.Duration {
	result := base
	for index := 0; index < failures && result < maximum; index++ {
		result *= 2
	}
	if result > maximum {
		return maximum
	}
	return result
}

func negativeJitter(value time.Duration) time.Duration {
	spread := value / 10
	if spread <= 0 {
		return value
	}
	offset := time.Duration(time.Now().UnixNano() % int64(spread+1))
	return value - offset
}

// CheckoutRestrictions converts fresh official status evidence into a
// conservative checkout guard. An unreachable/unknown official source is
// deliberately fail-open because the credentialed installation probe remains
// the primary source of truth for a merchant connection.
func (c *Client) CheckoutRestrictions(ctx context.Context, providerCode, environment string) CheckoutRestrictions {
	providerCode = strings.ToLower(strings.TrimSpace(providerCode))
	environment = strings.ToLower(strings.TrimSpace(environment))
	for _, provider := range c.Snapshot(ctx) {
		if provider.ProviderCode != providerCode || !provider.SourceAvailable || provider.Status == statusUnknown {
			continue
		}
		if provider.Status == statusUnavailable {
			return CheckoutRestrictions{ProviderUnavailable: true, ProviderReason: "OFFICIAL_PROVIDER_OUTAGE", Methods: []CheckoutMethodRestriction{}}
		}
		methods := make(map[string]string)
		for _, component := range provider.Components {
			if component.Status != statusDegraded && component.Status != statusUnavailable {
				continue
			}
			if providerWideComponent(providerCode, component.Name, environment) {
				return CheckoutRestrictions{ProviderUnavailable: true, ProviderReason: officialRestrictionReason(component.Status), Methods: []CheckoutMethodRestriction{}}
			}
			for _, methodCode := range componentPaymentMethods(providerCode, component.Name, component.Group) {
				methods[methodCode] = officialRestrictionReason(component.Status)
			}
		}
		result := CheckoutRestrictions{Methods: make([]CheckoutMethodRestriction, 0, len(methods))}
		for methodCode, reason := range methods {
			result.Methods = append(result.Methods, CheckoutMethodRestriction{PaymentMethodCode: methodCode, Reason: reason})
		}
		sort.Slice(result.Methods, func(i, j int) bool { return result.Methods[i].PaymentMethodCode < result.Methods[j].PaymentMethodCode })
		return result
	}
	return CheckoutRestrictions{Methods: []CheckoutMethodRestriction{}}
}

func officialRestrictionReason(status string) string {
	if status == statusUnavailable {
		return "OFFICIAL_PROVIDER_OFFLINE"
	}
	return "OFFICIAL_PROVIDER_MAINTENANCE"
}

func providerWideComponent(providerCode, name, environment string) bool {
	value := strings.ToLower(strings.TrimSpace(name))
	switch providerCode {
	case "doku":
		return value == "checkout page" || value == "payment link" || value == "check status" || (value == "sandbox environment" && environment == "sandbox")
	case "midtrans":
		return strings.Contains(value, "core api") || strings.Contains(value, "payment link") || strings.Contains(value, "snap")
	}
	return false
}

func componentPaymentMethods(providerCode, name, group string) []string {
	value := strings.ToLower(strings.TrimSpace(name + " " + group))
	switch {
	case strings.Contains(value, "qris"):
		return []string{"qris"}
	case strings.Contains(value, "credit card") || strings.Contains(value, "debit card") || strings.TrimSpace(strings.ToLower(name)) == "cards":
		return []string{"card"}
	case strings.Contains(value, "gopay") || strings.Contains(value, "go-pay"):
		return []string{"ewallet_gopay"}
	case strings.Contains(value, "shopee"):
		return []string{"ewallet_shopeepay"}
	case strings.Contains(value, "linkaja"):
		return []string{"ewallet_linkaja"}
	case strings.Contains(value, "astrapay"):
		return []string{"ewallet_astrapay"}
	case strings.Contains(value, "dana"):
		return []string{"ewallet_dana"}
	case strings.Contains(value, "ovo"):
		return []string{"ewallet_ovo"}
	case strings.Contains(value, "akulaku"):
		return []string{"paylater_akulaku"}
	case strings.Contains(value, "kredivo"):
		return []string{"paylater_kredivo"}
	case strings.Contains(value, "indodana"):
		return []string{"paylater_indodana"}
	case strings.Contains(value, "atome"):
		return []string{"paylater_atome"}
	case strings.Contains(value, "alfamart"):
		return []string{"retail_alfamart"}
	case strings.Contains(value, "indomaret"):
		return []string{"retail_indomaret"}
	case strings.Contains(value, "jenius"):
		return []string{"digital_banking_jenius"}
	}
	for token, code := range map[string]string{
		"arta graha": "va_arta_graha", "atm bersama": "va_atm_bersama", "bca": "va_bca", "bni": "va_bni", "bri": "va_bri",
		"bsi": "va_bsi", "btn": "va_btn", "cimb": "va_cimb", "danamon": "va_danamon", "mandiri": "va_mandiri",
		"maybank": "va_maybank", "muamalat": "va_muamalat", "permata": "va_permata", "sahabat sampoerna": "va_sahabat_sampoerna",
	} {
		if strings.Contains(value, token) {
			return []string{code}
		}
	}
	category := ""
	switch {
	case strings.Contains(value, "virtual account"):
		category = "va"
	case strings.Contains(value, "e-wallet") || strings.Contains(value, "ewallet"):
		category = "ewallet"
	case strings.Contains(value, "retail") || strings.Contains(value, "offline to online"):
		category = "retail"
	case strings.Contains(value, "buy now pay later") || strings.Contains(value, "paylater"):
		category = "paylater"
	}
	return providerCategoryMethods(providerCode, category)
}

func providerCategoryMethods(providerCode, category string) []string {
	methods := map[string][]string{
		"doku:va":         {"va_bca", "va_mandiri", "va_bni", "va_bri", "va_permata", "va_cimb", "va_danamon", "va_bsi", "va_bnc", "va_btn", "va_doku"},
		"doku:ewallet":    {"ewallet_ovo", "ewallet_dana", "ewallet_shopeepay", "ewallet_linkaja", "ewallet_doku"},
		"doku:retail":     {"retail_alfamart", "retail_indomaret"},
		"ipaymu:va":       {"va_arta_graha", "va_bca", "va_bni", "va_cimb", "va_mandiri", "va_muamalat", "va_bri", "va_bsi", "va_permata", "va_danamon", "va_btn"},
		"ipaymu:ewallet":  {"ewallet_dana", "ewallet_shopeepay"},
		"ipaymu:retail":   {"retail_alfamart", "retail_indomaret"},
		"ipaymu:paylater": {"paylater_akulaku"},
	}
	return append([]string(nil), methods[providerCode+":"+category]...)
}

func (c *Client) fetch(ctx context.Context, item source) model.OfficialProviderStatus {
	fallback := model.OfficialProviderStatus{
		ProviderCode: item.Code, ProviderName: item.Name, StatusPageURL: item.PageURL,
		Status: statusUnknown, Description: "Official status source is temporarily unavailable.",
		Source: item.Kind, Components: []model.OfficialProviderComponent{}, ActiveIncidents: []model.OfficialProviderEvent{},
		Maintenance: []model.OfficialProviderEvent{}, RecentIncidents: []model.OfficialProviderEvent{},
	}
	var body []byte
	var err error
	if item.Kind == "ipaymu_healthcheck" {
		body, err = c.postJSON(ctx, item.SummaryURL, []byte(`{"type":"payment_channel","search":"","page":1,"perpage":100,"sortBy":[],"direction":[]}`))
	} else {
		body, err = c.get(ctx, item.SummaryURL)
	}
	if err != nil {
		return fallback
	}
	var result model.OfficialProviderStatus
	switch item.Kind {
	case "midtrans_html":
		result, err = parseMidtransHTML(body, item)
	case "pagerduty_status_dashboard":
		var postBody []byte
		postBody, err = c.get(ctx, item.IncidentsURL)
		if err == nil {
			result, err = parseDOKUStatus(body, postBody, item, time.Now())
		}
	case "ipaymu_healthcheck":
		result, err = parseIPaymuStatus(body, item)
	default:
		result, err = parseStatuspage(body, item)
	}
	if err != nil {
		return fallback
	}
	result.SourceAvailable = true
	if item.Kind == "statuspage_v2" && item.IncidentsURL != "" {
		if incidentBody, incidentErr := c.get(ctx, item.IncidentsURL); incidentErr == nil {
			result.RecentIncidents = parseStatuspageIncidentHistory(incidentBody, item, 5)
		}
	}
	return result
}

func (c *Client) postJSON(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Emisell-Payment-Proxy-Status/1.0")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, io.ErrUnexpectedEOF
	}
	return io.ReadAll(io.LimitReader(response.Body, maxStatusBody))
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json,text/html;q=0.9")
	request.Header.Set("User-Agent", "Emisell-Payment-Proxy-Status/1.0")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, io.ErrUnexpectedEOF
	}
	return io.ReadAll(io.LimitReader(response.Body, maxStatusBody))
}

type statuspagePayload struct {
	Page struct {
		UpdatedAt time.Time `json:"updated_at"`
	} `json:"page"`
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Components []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		GroupID string `json:"group_id"`
		Group   bool   `json:"group"`
	} `json:"components"`
	Incidents             []statuspageEvent `json:"incidents"`
	ScheduledMaintenances []statuspageEvent `json:"scheduled_maintenances"`
}

type statuspageEvent struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	Impact         string     `json:"impact"`
	Shortlink      string     `json:"shortlink"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ScheduledFor   *time.Time `json:"scheduled_for"`
	ScheduledUntil *time.Time `json:"scheduled_until"`
	Components     []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		GroupID string `json:"group_id"`
		Group   bool   `json:"group"`
	} `json:"components"`
	IncidentUpdates []struct {
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"incident_updates"`
}

func parseStatuspage(body []byte, item source) (model.OfficialProviderStatus, error) {
	var payload statuspagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return model.OfficialProviderStatus{}, err
	}
	groups := make(map[string]string)
	for _, component := range payload.Components {
		if component.Group {
			groups[component.ID] = component.Name
		}
	}
	components := make([]model.OfficialProviderComponent, 0)
	for _, component := range payload.Components {
		group := groups[component.GroupID]
		if !relevantStatuspageComponent(item.Code, component.Name, group, component.Group) {
			continue
		}
		components = append(components, model.OfficialProviderComponent{Name: component.Name, Group: group, Status: normalizeStatuspageStatus(component.Status)})
	}
	status := normalizeStatuspageIndicator(payload.Status.Indicator)
	result := model.OfficialProviderStatus{
		ProviderCode: item.Code, ProviderName: item.Name, StatusPageURL: item.PageURL,
		Status: status, Description: payload.Status.Description, Source: item.Kind, UpdatedAt: timePointer(payload.Page.UpdatedAt),
		Components: components, ActiveIncidents: make([]model.OfficialProviderEvent, 0), Maintenance: make([]model.OfficialProviderEvent, 0), RecentIncidents: make([]model.OfficialProviderEvent, 0),
	}
	for _, incident := range payload.Incidents {
		if eventRelevant(item.Code, incident, groups) {
			result.ActiveIncidents = append(result.ActiveIncidents, normalizeStatuspageEvent(incident, "INCIDENT"))
		}
	}
	for _, maintenance := range payload.ScheduledMaintenances {
		if eventRelevant(item.Code, maintenance, groups) {
			result.Maintenance = append(result.Maintenance, normalizeStatuspageEvent(maintenance, "MAINTENANCE"))
		}
	}
	return result, nil
}

func parseStatuspageIncidentHistory(body []byte, item source, limit int) []model.OfficialProviderEvent {
	var payload struct {
		Incidents []statuspageEvent `json:"incidents"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return []model.OfficialProviderEvent{}
	}
	result := make([]model.OfficialProviderEvent, 0, limit)
	for _, incident := range payload.Incidents {
		groups := make(map[string]string)
		for _, component := range incident.Components {
			if component.Group {
				groups[component.ID] = component.Name
			}
		}
		if !eventRelevant(item.Code, incident, groups) {
			continue
		}
		result = append(result, normalizeStatuspageEvent(incident, "INCIDENT"))
		if len(result) == limit {
			break
		}
	}
	return result
}

func normalizeStatuspageEvent(event statuspageEvent, eventType string) model.OfficialProviderEvent {
	components := make([]string, 0, len(event.Components))
	for _, component := range event.Components {
		components = append(components, component.Name)
	}
	summary := ""
	if len(event.IncidentUpdates) > 0 {
		summary = compactText(event.IncidentUpdates[0].Body, 600)
	}
	return model.OfficialProviderEvent{
		ID: event.ID, Type: eventType, Name: event.Name, Status: strings.ToUpper(event.Status), Impact: strings.ToUpper(event.Impact),
		Summary: summary, URL: event.Shortlink, Components: components, StartedAt: timePointer(event.CreatedAt),
		ScheduledFor: event.ScheduledFor, ScheduledUntil: event.ScheduledUntil, UpdatedAt: timePointer(event.UpdatedAt),
	}
}

func relevantStatuspageComponent(providerCode, name, group string, isGroup bool) bool {
	nameLower, groupLower := strings.ToLower(name), strings.ToLower(group)
	switch providerCode {
	case "xendit":
		if isGroup {
			return nameLower == "api" || nameLower == "callback" || nameLower == "credit/debit cards" || nameLower == "indonesia - payments" || nameLower == "test mode"
		}
		return groupLower == "indonesia - payments" || groupLower == "api" || groupLower == "callback" || groupLower == "credit/debit cards" || groupLower == "test mode"
	case "duitku":
		groups := []string{"credit card", "credit payment", "e-wallet", "qris", "retail", "virtual account", "internet banking"}
		for _, candidate := range groups {
			if (isGroup && nameLower == candidate) || groupLower == candidate {
				return true
			}
		}
	}
	return false
}

func eventRelevant(providerCode string, event statuspageEvent, groups map[string]string) bool {
	for _, component := range event.Components {
		if relevantStatuspageComponent(providerCode, component.Name, groups[component.GroupID], component.Group) {
			return true
		}
	}
	name := strings.ToLower(event.Name)
	if providerCode == "xendit" {
		if strings.Contains(name, "payout") || strings.Contains(name, "disbursement") || strings.Contains(name, "remittance") {
			return false
		}
		return strings.Contains(name, "indonesia")
	}
	return providerCode == "duitku" && !strings.Contains(name, "disbursement") && !strings.Contains(name, "remittance")
}

type dokuServicePayload struct {
	Services []struct {
		ID                string `json:"id"`
		BusinessServiceID string `json:"business_service_id"`
		DisplayName       string `json:"display_name"`
		Active            bool   `json:"is_active"`
	} `json:"services"`
}

type dokuPostPayload struct {
	Posts []dokuPost `json:"posts"`
}

type dokuPost struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	PostType string `json:"post_type"`
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at"`
	Latest   struct {
		StatusID   string `json:"status_id"`
		SeverityID string `json:"severity_id"`
		ReportedAt int64  `json:"reported_at"`
		Message    string `json:"message"`
		Impacts    []struct {
			ServiceID  string `json:"service_id"`
			SeverityID string `json:"severity_id"`
		} `json:"impacts"`
	} `json:"latest_update"`
}

var dokuPaymentServices = map[string]struct{}{
	"virtual account":         {},
	"cards":                   {},
	"checkout page":           {},
	"sandbox environment":     {},
	"offline to online (o2o)": {},
	"direct debit":            {},
	"payment link":            {},
	"qris":                    {},
	"buy now pay later":       {},
	"e-wallet":                {},
	"digital banking":         {},
	"notification":            {},
	"check status":            {},
}

func parseDOKUStatus(serviceBody, postBody []byte, item source, now time.Time) (model.OfficialProviderStatus, error) {
	var services dokuServicePayload
	if err := json.Unmarshal(serviceBody, &services); err != nil {
		return model.OfficialProviderStatus{}, err
	}
	var posts dokuPostPayload
	if err := json.Unmarshal(postBody, &posts); err != nil {
		return model.OfficialProviderStatus{}, err
	}
	result := model.OfficialProviderStatus{
		ProviderCode: item.Code, ProviderName: item.Name, StatusPageURL: item.PageURL, Source: item.Kind,
		Status: statusUnknown, Description: "Official DOKU status", Components: []model.OfficialProviderComponent{},
		ActiveIncidents: []model.OfficialProviderEvent{}, Maintenance: []model.OfficialProviderEvent{}, RecentIncidents: []model.OfficialProviderEvent{},
	}
	componentIndex := make(map[string]int)
	componentNames := make(map[string]string)
	for _, service := range services.Services {
		name := strings.TrimSpace(service.DisplayName)
		if _, relevant := dokuPaymentServices[strings.ToLower(name)]; !relevant || !service.Active {
			continue
		}
		componentIndex[service.ID] = len(result.Components)
		componentNames[service.ID] = name
		result.Components = append(result.Components, model.OfficialProviderComponent{Name: name, Group: "DOKU payment service", Status: statusAvailable})
	}
	for _, post := range posts.Posts {
		impacted := make([]string, 0, len(post.Latest.Impacts))
		for _, impact := range post.Latest.Impacts {
			if name := componentNames[impact.ServiceID]; name != "" {
				impacted = append(impacted, name)
			}
		}
		if len(impacted) == 0 {
			continue
		}
		reportedAt := unixMilliPointer(post.Latest.ReportedAt)
		if reportedAt != nil && (result.UpdatedAt == nil || reportedAt.After(*result.UpdatedAt)) {
			result.UpdatedAt = reportedAt
		}
		event := model.OfficialProviderEvent{
			ID: post.ID, Name: strings.TrimSpace(post.Title), Status: dokuPostStatus(post), Summary: compactText(cleanHTML(post.Latest.Message), 600),
			URL: "https://status.doku.com/posts/details/" + url.PathEscape(post.ID), Components: impacted, StartedAt: reportedAt, UpdatedAt: reportedAt,
		}
		switch strings.ToLower(post.PostType) {
		case "incident":
			componentStatus := dokuIncidentStatus(post)
			event.Type = "INCIDENT"
			event.Impact = mapOperationalImpact(componentStatus)
			result.ActiveIncidents = append(result.ActiveIncidents, event)
			for _, impact := range post.Latest.Impacts {
				if index, found := componentIndex[impact.ServiceID]; found {
					result.Components[index].Status = worseStatus(result.Components[index].Status, dokuImpactStatus(impact.SeverityID))
				}
			}
		case "maintenance":
			event.Type = "MAINTENANCE"
			event.Impact = "MAINTENANCE"
			event.ScheduledFor = unixMilliPointer(post.StartsAt)
			event.ScheduledUntil = unixMilliPointer(post.EndsAt)
			result.Maintenance = append(result.Maintenance, event)
			if dokuMaintenanceActive(post, now) {
				for _, impact := range post.Latest.Impacts {
					if index, found := componentIndex[impact.ServiceID]; found {
						result.Components[index].Status = worseStatus(result.Components[index].Status, statusDegraded)
					}
				}
			}
		}
	}
	sort.Slice(result.Components, func(i, j int) bool { return result.Components[i].Name < result.Components[j].Name })
	result.Status = aggregateComponentStatus(result.Components)
	result.Description = descriptionForStatus(result.Status)
	return result, nil
}

func dokuPostStatus(post dokuPost) string {
	switch post.Latest.StatusID {
	case "P4PQPKC":
		return "INVESTIGATING"
	case "P0D2P8P":
		return "DETECTED"
	case "PJZOPVW":
		return "RESOLVED"
	case "P7N67Y0":
		return "SCHEDULED"
	case "P3AI6IA":
		return "IN_PROGRESS"
	case "P01X66N":
		return "COMPLETED"
	default:
		return strings.ToUpper(strings.TrimSpace(post.PostType))
	}
}

func dokuIncidentStatus(post dokuPost) string {
	result := statusDegraded
	if post.Latest.SeverityID == "PYERNRT" {
		result = statusUnavailable
	}
	for _, impact := range post.Latest.Impacts {
		result = worseStatus(result, dokuImpactStatus(impact.SeverityID))
	}
	return result
}

func dokuImpactStatus(severityID string) string {
	switch severityID {
	case "PNJU8NQ":
		return statusUnavailable
	case "PRVF8MD", "PY2J4PR":
		return statusDegraded
	default:
		return statusDegraded
	}
}

func dokuMaintenanceActive(post dokuPost, now time.Time) bool {
	if post.Latest.StatusID == "P3AI6IA" {
		return true
	}
	start, end := unixMilliPointer(post.StartsAt), unixMilliPointer(post.EndsAt)
	return start != nil && !now.Before(*start) && (end == nil || now.Before(*end))
}

type ipaymuHealthPayload struct {
	Data struct {
		HealthChecks []struct {
			Code       string `json:"Code"`
			Type       string `json:"Type"`
			Name       string `json:"Name"`
			StatusText string `json:"StatusText"`
			UpdatedAt  string `json:"UpdatedAt"`
		} `json:"HealthCheck"`
	} `json:"Data"`
}

func parseIPaymuStatus(body []byte, item source) (model.OfficialProviderStatus, error) {
	var payload ipaymuHealthPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return model.OfficialProviderStatus{}, err
	}
	result := model.OfficialProviderStatus{
		ProviderCode: item.Code, ProviderName: item.Name, StatusPageURL: item.PageURL, Source: item.Kind,
		Status: statusUnknown, Description: "Official iPaymu status", Components: []model.OfficialProviderComponent{},
		ActiveIncidents: []model.OfficialProviderEvent{}, Maintenance: []model.OfficialProviderEvent{}, RecentIncidents: []model.OfficialProviderEvent{},
	}
	for _, health := range payload.Data.HealthChecks {
		if !strings.EqualFold(strings.TrimSpace(health.Type), "Payment Channel") {
			continue
		}
		name := strings.TrimSpace(health.Name)
		componentStatus := normalizeIPaymuStatus(health.StatusText)
		updatedAt := parseIPaymuTime(health.UpdatedAt)
		if updatedAt != nil && (result.UpdatedAt == nil || updatedAt.After(*result.UpdatedAt)) {
			result.UpdatedAt = updatedAt
		}
		result.Components = append(result.Components, model.OfficialProviderComponent{Name: name, Group: "Payment Channel", Status: componentStatus})
		event := model.OfficialProviderEvent{
			ID: "ipaymu-" + strings.ToLower(strings.TrimSpace(health.Code)), Name: name, Status: strings.ToUpper(strings.TrimSpace(health.StatusText)),
			URL: item.PageURL, Components: []string{name}, StartedAt: updatedAt, UpdatedAt: updatedAt,
		}
		switch componentStatus {
		case statusUnavailable:
			event.Type = "INCIDENT"
			event.Impact = "OUTAGE"
			event.Summary = name + " is reported offline by iPaymu."
			result.ActiveIncidents = append(result.ActiveIncidents, event)
		case statusDegraded:
			event.Type = "MAINTENANCE"
			event.Impact = "MAINTENANCE"
			event.Summary = name + " is reported under maintenance by iPaymu."
			result.Maintenance = append(result.Maintenance, event)
		}
	}
	sort.Slice(result.Components, func(i, j int) bool { return result.Components[i].Name < result.Components[j].Name })
	result.Status = aggregateComponentStatus(result.Components)
	if result.Status == statusAvailable {
		result.Description = "All payment channels online"
	} else if result.Status == statusUnavailable {
		result.Description = "All reported payment channels unavailable"
	} else if result.Status == statusDegraded {
		result.Description = strconv.Itoa(len(result.ActiveIncidents)+len(result.Maintenance)) + " payment channels affected"
	} else {
		result.Description = "Official iPaymu status returned no usable payment channels"
	}
	return result, nil
}

func normalizeIPaymuStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "online":
		return statusAvailable
	case "maintenance":
		return statusDegraded
	case "offline":
		return statusUnavailable
	default:
		return statusUnknown
	}
}

func parseIPaymuTime(value string) *time.Time {
	location := time.FixedZone("WIB", 7*60*60)
	parsed, err := time.ParseInLocation("02/01/2006 15:04 WIB", strings.TrimSpace(value), location)
	if err != nil {
		return nil
	}
	return timePointer(parsed)
}

func unixMilliPointer(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	parsed := time.UnixMilli(value).UTC()
	return &parsed
}

func worseStatus(current, candidate string) string {
	rank := map[string]int{statusUnknown: 0, statusAvailable: 1, statusDegraded: 2, statusUnavailable: 3}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}

func aggregateComponentStatus(components []model.OfficialProviderComponent) string {
	available, affected, unavailable := 0, 0, 0
	for _, component := range components {
		switch component.Status {
		case statusAvailable:
			available++
		case statusDegraded:
			affected++
		case statusUnavailable:
			affected++
			unavailable++
		}
	}
	known := available + affected
	if known == 0 {
		return statusUnknown
	}
	if unavailable == known {
		return statusUnavailable
	}
	if affected > 0 {
		return statusDegraded
	}
	return statusAvailable
}

func mapOperationalImpact(status string) string {
	if status == statusUnavailable {
		return "OUTAGE"
	}
	return "PARTIAL_OUTAGE"
}

func descriptionForStatus(status string) string {
	switch status {
	case statusAvailable:
		return "All systems operational"
	case statusDegraded:
		return "Partial outage"
	case statusUnavailable:
		return "Major outage"
	default:
		return "Official status is unavailable"
	}
}

var (
	midtransComponentPattern   = regexp.MustCompile(`(?s)<div class="status-list">\s*<h3>(.*?)</h3>.*?<span class="status-badge">(.*?)</span>.*?<div class="status-opt">\s*(.*?)\s*</div>`)
	midtransHeadingPattern     = regexp.MustCompile(`(?s)<div class="status-head clearfix">\s*<h2>(.*?)</h2>\s*<span class="s-h2-sub">(.*?)</span>`)
	midtransMaintenancePattern = regexp.MustCompile(`(?s)<div class="status-sch">.*?<div class="status-sch-head clearfix">\s*<h3>(.*?)</h3>\s*<span>(.*?)</span>.*?<div class="status-sch-c-title">\s*Schedule\s*</div>\s*<div class="status-sch-c-content">(.*?)</div>.*?<div class="status-sch-c-title">\s*Components\s*</div>\s*<div class="status-sch-c-content">(.*?)</div>.*?<div class="status-sch-c-title">\s*Descriptions\s*</div>\s*<div class="status-sch-c-content">(.*?)</div>`)
	tagPattern                 = regexp.MustCompile(`<[^>]+>`)
)

func parseMidtransHTML(body []byte, item source) (model.OfficialProviderStatus, error) {
	page := string(body)
	result := model.OfficialProviderStatus{
		ProviderCode: item.Code, ProviderName: item.Name, StatusPageURL: item.PageURL, Source: item.Kind,
		Status: statusUnknown, Description: "Official Midtrans status", Components: []model.OfficialProviderComponent{},
		ActiveIncidents: []model.OfficialProviderEvent{}, Maintenance: []model.OfficialProviderEvent{}, RecentIncidents: []model.OfficialProviderEvent{},
	}
	heading := midtransHeadingPattern.FindStringSubmatch(page)
	if len(heading) > 1 {
		description := cleanHTML(heading[1])
		// Midtrans renders scheduled maintenance in a second status heading.
		// On the current page the primary heading may not contain an h2, so a
		// heading-only parser can mistake that future notice for a live outage.
		if !strings.Contains(strings.ToLower(description), "maintenance") {
			result.Description = description
			result.Status = normalizeDescriptionStatus(description)
		}
	}
	for _, match := range midtransComponentPattern.FindAllStringSubmatch(page, -1) {
		if len(match) < 4 {
			continue
		}
		name, group, componentStatus := cleanHTML(match[1]), cleanHTML(match[2]), normalizeDescriptionStatus(cleanHTML(match[3]))
		if !relevantMidtransComponent(name, group) {
			continue
		}
		result.Components = append(result.Components, model.OfficialProviderComponent{Name: name, Group: group, Status: componentStatus})
	}
	if componentStatus := aggregateComponentStatus(result.Components); componentStatus != statusUnknown {
		result.Status = worseStatus(result.Status, componentStatus)
		result.Description = descriptionForStatus(result.Status)
	}
	for index, match := range midtransMaintenancePattern.FindAllStringSubmatch(page, -1) {
		if len(match) < 6 {
			continue
		}
		componentNames := strings.Split(cleanHTML(match[4]), ",")
		for componentIndex := range componentNames {
			componentNames[componentIndex] = strings.TrimSpace(componentNames[componentIndex])
		}
		if !midtransEventRelevant(componentNames) {
			continue
		}
		result.Maintenance = append(result.Maintenance, model.OfficialProviderEvent{
			ID: "midtrans-maintenance-" + strconv.Itoa(index+1), Type: "MAINTENANCE", Name: cleanHTML(match[1]), Status: strings.ToUpper(cleanHTML(match[2])),
			Impact: "MAINTENANCE", Schedule: cleanHTML(match[3]), Summary: compactText(cleanHTML(match[5]), 600), URL: item.PageURL, Components: componentNames,
		})
	}
	return result, nil
}

func relevantMidtransComponent(name, group string) bool {
	value := strings.ToLower(name + " " + group)
	return !strings.Contains(value, "disbursement") && !strings.Contains(value, "payout") && !strings.Contains(value, "merchant administration portal") && !strings.Contains(value, "promo engine") && !strings.Contains(value, "pixel")
}

func midtransEventRelevant(components []string) bool {
	for _, component := range components {
		if relevantMidtransComponent(component, "") {
			return true
		}
	}
	return false
}

func normalizeStatuspageIndicator(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return statusAvailable
	case "minor", "maintenance":
		return statusDegraded
	case "major", "critical":
		return statusUnavailable
	default:
		return statusUnknown
	}
}

func normalizeStatuspageStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "operational":
		return statusAvailable
	case "degraded_performance", "partial_outage", "under_maintenance":
		return statusDegraded
	case "major_outage":
		return statusUnavailable
	default:
		return statusUnknown
	}
}

func normalizeDescriptionStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "all systems operational") || value == "operational" {
		return statusAvailable
	}
	if strings.Contains(value, "major") || strings.Contains(value, "unavailable") || strings.Contains(value, "outage") {
		return statusUnavailable
	}
	if strings.Contains(value, "maintenance") || strings.Contains(value, "disruption") || strings.Contains(value, "degraded") {
		return statusDegraded
	}
	return statusUnknown
}

func cleanHTML(value string) string {
	value = strings.ReplaceAll(value, "<br>", " ")
	value = strings.ReplaceAll(value, "<br/>", " ")
	value = strings.ReplaceAll(value, "<br />", " ")
	return strings.Join(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(value, " "))), " ")
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copyValue := value
	return &copyValue
}

func cloneStatuses(items []model.OfficialProviderStatus) []model.OfficialProviderStatus {
	result := make([]model.OfficialProviderStatus, len(items))
	copy(result, items)
	return result
}

func init() {
	for host := range allowedStatusHosts {
		if parsed, err := url.Parse("https://" + host); err != nil || parsed.Hostname() == "" {
			panic("invalid official provider status host")
		}
	}
}
