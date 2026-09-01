package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
	"github.com/emisell/api-payment-proxy/internal/ids"
	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrConflict            = errors.New("resource already exists")
	ErrInvalidState        = errors.New("resource state does not allow this operation")
	ErrVersionConflict     = errors.New("resource version has changed")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
)

type Postgres struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (s *Postgres) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

const providerAppSelect = `
	SELECT id,provider_code,provider_name,version,status,runtime,sdk_version,
	       file_name,content_type,artifact_size,artifact_sha256,manifest,scan_report,
	       review_note,submitted_by,reviewed_by,created_at,updated_at,published_at
	FROM provider_app_versions
`

const providerAppProviderSelect = `
	SELECT r.provider_code,p.name,p.description,r.website_url,r.documentation_url,r.support_email,r.status,
	       count(v.id)::int,
	       COALESCE(max(v.version) FILTER (WHERE v.status='PUBLISHED'),''),
	       COALESCE(latest.version,''),COALESCE(latest.status,''),
	       r.created_by,r.updated_by,r.created_at,r.updated_at
	FROM provider_app_providers r
	JOIN providers p ON p.code=r.provider_code
	LEFT JOIN provider_app_versions v ON v.provider_code=r.provider_code
	LEFT JOIN LATERAL (
		SELECT version,status FROM provider_app_versions
		WHERE provider_code=r.provider_code
		ORDER BY created_at DESC,id DESC LIMIT 1
	) latest ON true
`

type CreateProviderAppProviderInput struct {
	ProviderCode, ProviderName, Description, WebsiteURL, DocumentationURL, SupportEmail, Actor, RequestID string
}

func (s *Postgres) CreateProviderAppProvider(ctx context.Context, in CreateProviderAppProviderInput) (model.ProviderAppProvider, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ProviderAppProvider{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('provider-app:' || $1,0))`, in.ProviderCode); err != nil {
		return model.ProviderAppProvider{}, err
	}
	var providerExists, registryExists bool
	if err = tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM providers WHERE code=$1),
		       EXISTS(SELECT 1 FROM provider_app_providers WHERE provider_code=$1)
	`, in.ProviderCode).Scan(&providerExists, &registryExists); err != nil {
		return model.ProviderAppProvider{}, err
	}
	if registryExists {
		return model.ProviderAppProvider{}, ErrConflict
	}
	if providerExists {
		if _, err = tx.Exec(ctx, `
			UPDATE providers SET name=$2,description=$3,engine_connector=$1,updated_at=now()
			WHERE code=$1
		`, in.ProviderCode, in.ProviderName, in.Description); err != nil {
			return model.ProviderAppProvider{}, err
		}
	} else if _, err = tx.Exec(ctx, `
		INSERT INTO providers(code,name,description,available,engine_connector,credential_schema,environments,payment_methods)
		VALUES($1,$2,$3,false,$1,'[]'::jsonb,'["sandbox","live"]'::jsonb,'[]'::jsonb)
	`, in.ProviderCode, in.ProviderName, in.Description); err != nil {
		return model.ProviderAppProvider{}, translateConstraint(err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO provider_app_providers(provider_code,website_url,documentation_url,support_email,created_by,updated_by)
		VALUES($1,$2,$3,$4,$5,$5)
	`, in.ProviderCode, in.WebsiteURL, in.DocumentationURL, in.SupportEmail, in.Actor); err != nil {
		return model.ProviderAppProvider{}, translateConstraint(err)
	}
	if err = audit(ctx, tx, "", in.Actor, "provider_app.provider.create", "provider", in.ProviderCode, in.RequestID, map[string]any{
		"provider_code": in.ProviderCode, "provider_name": in.ProviderName,
	}); err != nil {
		return model.ProviderAppProvider{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ProviderAppProvider{}, err
	}
	return s.GetProviderAppProvider(ctx, in.ProviderCode)
}

func (s *Postgres) ListProviderAppProviders(ctx context.Context) ([]model.ProviderAppProvider, error) {
	rows, err := s.pool.Query(ctx, providerAppProviderSelect+`
		GROUP BY r.provider_code,p.name,p.description,r.website_url,r.documentation_url,r.support_email,r.status,
		         latest.version,latest.status,r.created_by,r.updated_by,r.created_at,r.updated_at
		ORDER BY r.updated_at DESC,r.provider_code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ProviderAppProvider, 0)
	for rows.Next() {
		item, scanErr := scanProviderAppProvider(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Postgres) GetProviderAppProvider(ctx context.Context, providerCode string) (model.ProviderAppProvider, error) {
	item, err := scanProviderAppProvider(s.pool.QueryRow(ctx, providerAppProviderSelect+`
		WHERE r.provider_code=$1
		GROUP BY r.provider_code,p.name,p.description,r.website_url,r.documentation_url,r.support_email,r.status,
		         latest.version,latest.status,r.created_by,r.updated_by,r.created_at,r.updated_at
	`, providerCode))
	return item, translateNotFound(err)
}

func scanProviderAppProvider(row providerAppScanner) (model.ProviderAppProvider, error) {
	var item model.ProviderAppProvider
	err := row.Scan(&item.ProviderCode, &item.ProviderName, &item.Description, &item.WebsiteURL,
		&item.DocumentationURL, &item.SupportEmail, &item.Status, &item.VersionCount,
		&item.ActiveVersion, &item.LatestVersion, &item.LatestStatus, &item.CreatedBy,
		&item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

type CreateProviderAppInput struct {
	ID, ProviderCode, ProviderName, Version, Runtime, SDKVersion string
	FileName, ContentType, ArtifactSHA256, Actor, RequestID      string
	Artifact                                                     []byte
	Manifest, ScanReport                                         []byte
}

func (s *Postgres) CreateProviderApp(ctx context.Context, in CreateProviderAppInput) (model.ProviderAppVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ProviderAppVersion{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('provider-app:' || $1,0))`, in.ProviderCode); err != nil {
		return model.ProviderAppVersion{}, err
	}
	var registeredName, registryStatus string
	if err = tx.QueryRow(ctx, `
		SELECT p.name,r.status FROM provider_app_providers r JOIN providers p ON p.code=r.provider_code
		WHERE r.provider_code=$1
	`, in.ProviderCode).Scan(&registeredName, &registryStatus); err != nil {
		return model.ProviderAppVersion{}, translateNotFound(err)
	}
	if registryStatus == "DISABLED" || !strings.EqualFold(strings.TrimSpace(registeredName), strings.TrimSpace(in.ProviderName)) {
		return model.ProviderAppVersion{}, ErrInvalidState
	}
	var count int
	var bytesUsed int64
	if err = tx.QueryRow(ctx, `SELECT count(*)::int,COALESCE(sum(artifact_size),0)::bigint FROM provider_app_versions WHERE provider_code=$1`, in.ProviderCode).Scan(&count, &bytesUsed); err != nil {
		return model.ProviderAppVersion{}, err
	}
	if count >= 25 || bytesUsed+int64(len(in.Artifact)) > 250<<20 {
		return model.ProviderAppVersion{}, ErrInvalidState
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO provider_app_versions(
			id,provider_code,provider_name,version,status,runtime,sdk_version,
			file_name,content_type,artifact_size,artifact_sha256,artifact,
			manifest,scan_report,submitted_by
		) VALUES($1,$2,$3,$4,'UPLOADED',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, in.ID, in.ProviderCode, in.ProviderName, in.Version, in.Runtime, in.SDKVersion,
		in.FileName, in.ContentType, len(in.Artifact), in.ArtifactSHA256, in.Artifact,
		in.Manifest, in.ScanReport, in.Actor)
	if err != nil {
		return model.ProviderAppVersion{}, translateConstraint(err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE provider_app_providers SET updated_by=$2,updated_at=now()
		WHERE provider_code=$1
	`, in.ProviderCode, in.Actor); err != nil {
		return model.ProviderAppVersion{}, err
	}
	if err = audit(ctx, tx, "", in.Actor, "provider_app.upload", "provider_app_version", in.ID, in.RequestID, map[string]any{
		"provider_code": in.ProviderCode, "version": in.Version, "artifact_size": len(in.Artifact), "artifact_sha256": in.ArtifactSHA256,
	}); err != nil {
		return model.ProviderAppVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ProviderAppVersion{}, err
	}
	return s.GetProviderApp(ctx, in.ID)
}

func (s *Postgres) ListProviderApps(ctx context.Context) ([]model.ProviderAppVersion, error) {
	rows, err := s.pool.Query(ctx, providerAppSelect+` ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ProviderAppVersion, 0)
	for rows.Next() {
		item, scanErr := scanProviderApp(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Postgres) ListProviderAppsByProvider(ctx context.Context, providerCode string) ([]model.ProviderAppVersion, error) {
	rows, err := s.pool.Query(ctx, providerAppSelect+` WHERE provider_code=$1 ORDER BY created_at DESC,id DESC`, providerCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ProviderAppVersion, 0)
	for rows.Next() {
		item, scanErr := scanProviderApp(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Postgres) GetProviderApp(ctx context.Context, id string) (model.ProviderAppVersion, error) {
	item, err := scanProviderApp(s.pool.QueryRow(ctx, providerAppSelect+` WHERE id=$1`, id))
	return item, translateNotFound(err)
}

func (s *Postgres) GetProviderAppArtifact(ctx context.Context, id string) ([]byte, error) {
	var artifact []byte
	err := s.pool.QueryRow(ctx, `SELECT artifact FROM provider_app_versions WHERE id=$1`, id).Scan(&artifact)
	return artifact, translateNotFound(err)
}

type ProviderAppTransitionInput struct {
	ID, ExpectedStatus, Status, ReviewNote, RuntimeDigest, Actor, RequestID string
}

func (s *Postgres) TransitionProviderApp(ctx context.Context, in ProviderAppTransitionInput) (model.ProviderAppVersion, error) {
	if in.Status == "PUBLISHED" && !validRuntimeDigest(in.RuntimeDigest) {
		return model.ProviderAppVersion{}, ErrInvalidState
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ProviderAppVersion{}, err
	}
	defer tx.Rollback(ctx)
	var providerCode, previousStatus string
	if err = tx.QueryRow(ctx, `SELECT provider_code,status FROM provider_app_versions WHERE id=$1 FOR UPDATE`, in.ID).Scan(&providerCode, &previousStatus); err != nil {
		return model.ProviderAppVersion{}, translateNotFound(err)
	}
	if previousStatus != in.ExpectedStatus || !validProviderAppTransition(previousStatus, in.Status) {
		return model.ProviderAppVersion{}, ErrInvalidState
	}
	if in.Status == "PUBLISHED" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('provider-app:' || $1,0))`, providerCode); err != nil {
			return model.ProviderAppVersion{}, err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE provider_app_versions SET status='DEPRECATED',updated_at=now()
			WHERE provider_code=$1 AND status='PUBLISHED' AND id<>$2
		`, providerCode, in.ID); err != nil {
			return model.ProviderAppVersion{}, err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO providers(code,name,description,available,engine_connector,credential_schema,environments,payment_methods)
			SELECT provider_code,provider_name,'Connector published from a certified Emisell Provider App.',true,provider_code,
			       manifest->'credential_fields',manifest->'environments',manifest->'payment_methods'
			FROM provider_app_versions WHERE id=$1
			ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,
			available=true,engine_connector=EXCLUDED.engine_connector,credential_schema=EXCLUDED.credential_schema,
			environments=EXCLUDED.environments,payment_methods=EXCLUDED.payment_methods,updated_at=now()
		`, in.ID); err != nil {
			return model.ProviderAppVersion{}, err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE provider_versions SET status='SUPERSEDED'
			WHERE provider_code=$1 AND status='RELEASED'
		`, providerCode); err != nil {
			return model.ProviderAppVersion{}, err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO provider_versions(id,provider_code,version,engine_kind,artifact_digest,status,released_at)
			SELECT id,provider_code,version,runtime,$2,'RELEASED',now()
			FROM provider_app_versions WHERE id=$1
			ON CONFLICT(provider_code,version) DO UPDATE SET engine_kind=EXCLUDED.engine_kind,
			artifact_digest=EXCLUDED.artifact_digest,status='RELEASED',released_at=now()
		`, in.ID, in.RuntimeDigest); err != nil {
			return model.ProviderAppVersion{}, err
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE provider_app_versions SET status=$2,review_note=$3,reviewed_by=$4,
		updated_at=now(),published_at=CASE WHEN $2='PUBLISHED' THEN now() ELSE published_at END
		WHERE id=$1
	`, in.ID, in.Status, in.ReviewNote, in.Actor)
	if err != nil {
		return model.ProviderAppVersion{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE provider_app_providers
		SET status=CASE WHEN $2='PUBLISHED' THEN 'ACTIVE' ELSE status END,updated_by=$3,updated_at=now()
		WHERE provider_code=$1
	`, providerCode, in.Status, in.Actor); err != nil {
		return model.ProviderAppVersion{}, err
	}
	if err = audit(ctx, tx, "", in.Actor, "provider_app.transition", "provider_app_version", in.ID, in.RequestID, map[string]any{
		"provider_code": providerCode, "previous_status": previousStatus, "status": in.Status, "review_note": in.ReviewNote, "runtime_digest": in.RuntimeDigest,
	}); err != nil {
		return model.ProviderAppVersion{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ProviderAppVersion{}, err
	}
	return s.GetProviderApp(ctx, in.ID)
}

func validRuntimeDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validProviderAppTransition(from, to string) bool {
	switch from + ":" + to {
	case "UPLOADED:VALIDATED", "VALIDATED:CERTIFIED", "CERTIFIED:PUBLISHED",
		"UPLOADED:DISABLED", "VALIDATED:DISABLED", "CERTIFIED:DISABLED",
		"PUBLISHED:DEPRECATED", "DEPRECATED:DISABLED":
		return true
	default:
		return false
	}
}

type providerAppScanner interface{ Scan(...any) error }

func scanProviderApp(row providerAppScanner) (model.ProviderAppVersion, error) {
	var item model.ProviderAppVersion
	err := row.Scan(&item.ID, &item.ProviderCode, &item.ProviderName, &item.Version, &item.Status,
		&item.Runtime, &item.SDKVersion, &item.FileName, &item.ContentType, &item.ArtifactSize,
		&item.ArtifactSHA256, &item.Manifest, &item.ScanReport, &item.ReviewNote,
		&item.SubmittedBy, &item.ReviewedBy, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt)
	return item, err
}

func (s *Postgres) DashboardOverview(ctx context.Context) (model.DashboardOverview, error) {
	result := model.DashboardOverview{
		GeneratedAt:     time.Now().UTC(),
		PaymentStatuses: make([]model.DashboardStatusMetric, 0),
		VolumeDaily:     make([]model.DashboardVolumeMetric, 0, 7),
		Providers:       make([]model.DashboardProviderMetric, 0),
		RecentPayments:  make([]model.DashboardRecentPayment, 0),
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE created_at >= now()-interval '24 hours'),
			count(*) FILTER (WHERE created_at >= now()-interval '48 hours' AND created_at < now()-interval '24 hours'),
			COALESCE(sum(amount) FILTER (WHERE status='SUCCEEDED' AND created_at >= now()-interval '24 hours'),0),
			COALESCE(sum(amount) FILTER (WHERE status='SUCCEEDED' AND created_at >= now()-interval '48 hours' AND created_at < now()-interval '24 hours'),0),
			COALESCE(round(100.0 * count(*) FILTER (WHERE status='SUCCEEDED' AND created_at >= now()-interval '24 hours') /
				NULLIF(count(*) FILTER (WHERE status IN ('SUCCEEDED','FAILED','CANCELLED','EXPIRED') AND created_at >= now()-interval '24 hours'),0),1),0)
		FROM payment_sessions
	`).Scan(
		&result.Summary.Payments24h,
		&result.Summary.PreviousPayments24h,
		&result.Summary.SucceededVolume24h,
		&result.Summary.PreviousVolume24h,
		&result.Summary.SuccessRate24h,
	); err != nil {
		return model.DashboardOverview{}, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM provider_installations WHERE status='ACTIVE'`).Scan(&result.Summary.ActiveInstallations); err != nil {
		return model.DashboardOverview{}, err
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(round(100.0 * count(*) FILTER (WHERE status IN ('PROCESSED','IGNORED')) /
			NULLIF(count(*) FILTER (WHERE status IN ('PROCESSED','IGNORED','FAILED') AND received_at >= now()-interval '24 hours'),0),1),100)
		FROM webhook_inbox WHERE received_at >= now()-interval '24 hours'
	`).Scan(&result.Summary.WebhookSuccessRate24); err != nil {
		return model.DashboardOverview{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT status,count(*) FROM payment_sessions
		WHERE created_at >= now()-interval '24 hours'
		GROUP BY status ORDER BY count(*) DESC,status
	`)
	if err != nil {
		return model.DashboardOverview{}, err
	}
	for rows.Next() {
		var item model.DashboardStatusMetric
		if err = rows.Scan(&item.Status, &item.Count); err != nil {
			rows.Close()
			return model.DashboardOverview{}, err
		}
		result.PaymentStatuses = append(result.PaymentStatuses, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return model.DashboardOverview{}, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		WITH days AS (
			SELECT generate_series(current_date-6,current_date,interval '1 day')::date AS day
		)
		SELECT to_char(days.day,'YYYY-MM-DD'),
			COALESCE(sum(p.amount) FILTER (WHERE p.status='SUCCEEDED'),0),
			count(p.id) FILTER (WHERE p.status='SUCCEEDED')
		FROM days
		LEFT JOIN payment_sessions p ON p.created_at >= days.day AND p.created_at < days.day+interval '1 day'
		GROUP BY days.day ORDER BY days.day
	`)
	if err != nil {
		return model.DashboardOverview{}, err
	}
	for rows.Next() {
		var item model.DashboardVolumeMetric
		if err = rows.Scan(&item.Date, &item.Amount, &item.Count); err != nil {
			rows.Close()
			return model.DashboardOverview{}, err
		}
		result.VolumeDaily = append(result.VolumeDaily, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return model.DashboardOverview{}, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT p.code,p.name,p.available,p.payment_methods,
			count(ps.id) FILTER (WHERE ps.created_at >= now()-interval '24 hours'),
			count(ps.id) FILTER (WHERE ps.status='SUCCEEDED' AND ps.created_at >= now()-interval '24 hours'),
			count(ps.id) FILTER (WHERE ps.status='FAILED' AND ps.created_at >= now()-interval '24 hours'),
			COALESCE(sum(ps.amount) FILTER (WHERE ps.status='SUCCEEDED' AND ps.created_at >= now()-interval '24 hours'),0)
		FROM providers p LEFT JOIN payment_sessions ps ON ps.provider_code=p.code
		GROUP BY p.code,p.name,p.available,p.payment_methods ORDER BY p.available DESC,p.name
	`)
	if err != nil {
		return model.DashboardOverview{}, err
	}
	for rows.Next() {
		var item model.DashboardProviderMetric
		if err = rows.Scan(&item.Code, &item.Name, &item.Available, &item.PaymentMethods, &item.Payments24h, &item.Succeeded24h, &item.Failed24h, &item.Volume24h); err != nil {
			rows.Close()
			return model.DashboardOverview{}, err
		}
		result.Providers = append(result.Providers, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return model.DashboardOverview{}, err
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `
		SELECT id,merchant_reference,provider_code,environment,amount,currency,status,created_at
		FROM payment_sessions ORDER BY created_at DESC LIMIT 8
	`)
	if err != nil {
		return model.DashboardOverview{}, err
	}
	for rows.Next() {
		var item model.DashboardRecentPayment
		if err = rows.Scan(&item.ID, &item.MerchantReference, &item.ProviderCode, &item.Environment, &item.Amount, &item.Currency, &item.Status, &item.CreatedAt); err != nil {
			rows.Close()
			return model.DashboardOverview{}, err
		}
		result.RecentPayments = append(result.RecentPayments, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return model.DashboardOverview{}, err
	}
	rows.Close()

	if err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM payment_sessions WHERE status='UNKNOWN'),
			(SELECT count(*) FROM outbox_events WHERE status IN ('PENDING','PROCESSING')),
			(SELECT count(*) FROM outbox_events WHERE status='DEAD'),
			(SELECT count(*) FROM webhook_inbox WHERE status='FAILED')
	`).Scan(
		&result.OperationalBacklog.UnknownPayments,
		&result.OperationalBacklog.PendingOutbox,
		&result.OperationalBacklog.DeadOutbox,
		&result.OperationalBacklog.FailedWebhooks,
	); err != nil {
		return model.DashboardOverview{}, err
	}
	return result, nil
}

func (s *Postgres) ListProviders(ctx context.Context) ([]model.Provider, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code,name,description,available,engine_connector,credential_schema,environments,
		       payment_methods,created_at,updated_at
		FROM providers ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Provider, 0)
	for rows.Next() {
		var item model.Provider
		if err := rows.Scan(&item.Code, &item.Name, &item.Description, &item.Available, &item.EngineConnector,
			&item.CredentialSchema, &item.Environments, &item.PaymentMethods, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Postgres) GetProvider(ctx context.Context, code string) (model.Provider, error) {
	var item model.Provider
	err := s.pool.QueryRow(ctx, `
		SELECT code,name,description,available,engine_connector,credential_schema,environments,
		       payment_methods,created_at,updated_at FROM providers WHERE code=$1
	`, code).Scan(&item.Code, &item.Name, &item.Description, &item.Available, &item.EngineConnector,
		&item.CredentialSchema, &item.Environments, &item.PaymentMethods, &item.CreatedAt, &item.UpdatedAt)
	return item, translateNotFound(err)
}

func (s *Postgres) GetReleasedProviderVersion(ctx context.Context, providerCode string) (string, error) {
	var version string
	err := s.pool.QueryRow(ctx, `
		SELECT version
		FROM provider_versions
		WHERE provider_code=$1 AND status='RELEASED'
		ORDER BY released_at DESC NULLS LAST,created_at DESC,version DESC
		LIMIT 1
	`, providerCode).Scan(&version)
	return version, translateNotFound(err)
}

func (s *Postgres) ListPaymentMethods(ctx context.Context) ([]model.PaymentMethodCatalogItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.code,m.category,m.name,m.description,m.countries,m.currencies,m.active,m.sort_order,
			COALESCE(jsonb_agg(jsonb_build_object(
				'provider_code',c.provider_code,
				'provider_name',p.name,
				'provider_available',p.available,
				'provider_method',c.provider_method,
				'provider_method_type',c.provider_method_type,
				'provider_channel_code',c.provider_channel_code,
				'support_status',c.support_status,
				'source_url',c.source_url,
				'metadata',c.metadata
			) ORDER BY p.name) FILTER (WHERE c.provider_code IS NOT NULL),'[]'::jsonb)
		FROM payment_methods m
		LEFT JOIN provider_payment_method_capabilities c ON c.payment_method_code=m.code AND c.support_status <> 'DISABLED'
		LEFT JOIN providers p ON p.code=c.provider_code
		WHERE m.active=true
		GROUP BY m.code,m.category,m.name,m.description,m.countries,m.currencies,m.active,m.sort_order
		ORDER BY m.sort_order,m.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.PaymentMethodCatalogItem, 0)
	for rows.Next() {
		var item model.PaymentMethodCatalogItem
		if err = rows.Scan(&item.Code, &item.Category, &item.Name, &item.Description, &item.Countries, &item.Currencies, &item.Active, &item.SortOrder, &item.Providers); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Postgres) GetProviderPaymentMethodCapability(ctx context.Context, providerCode, paymentMethodCode string) (model.ProviderPaymentMethodCapability, error) {
	var item model.ProviderPaymentMethodCapability
	err := s.pool.QueryRow(ctx, `
		SELECT c.provider_code,p.name,p.available,c.payment_method_code,c.provider_method,c.provider_method_type,
			c.provider_channel_code,c.support_status,c.source_url,c.metadata
		FROM provider_payment_method_capabilities c
		JOIN providers p ON p.code=c.provider_code
		JOIN payment_methods m ON m.code=c.payment_method_code AND m.active=true
		WHERE c.provider_code=$1 AND c.payment_method_code=$2
	`, providerCode, paymentMethodCode).Scan(
		&item.ProviderCode, &item.ProviderName, &item.ProviderAvailable, &item.PaymentMethodCode,
		&item.ProviderMethod, &item.ProviderMethodType, &item.ProviderChannelCode,
		&item.SupportStatus, &item.SourceURL, &item.Metadata,
	)
	return item, translateNotFound(err)
}

type CreateConnectorCertificationRunInput struct {
	ID, TenantID, InstallationID, ProviderCode, PaymentMethodCode, Environment string
	Status, PaymentID, Message, Actor, RequestID                               string
	Checks                                                                     []byte
}

func (s *Postgres) CreateConnectorCertificationRun(ctx context.Context, in CreateConnectorCertificationRunInput) (model.ConnectorCertificationRun, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ConnectorCertificationRun{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO connector_certification_runs
		(id,tenant_id,installation_id,provider_code,payment_method_code,environment,status,checks,payment_id,message,initiated_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11)
	`, in.ID, in.TenantID, in.InstallationID, in.ProviderCode, in.PaymentMethodCode, in.Environment, in.Status, in.Checks, in.PaymentID, in.Message, in.Actor)
	if err != nil {
		return model.ConnectorCertificationRun{}, translateConstraint(err)
	}
	if err = audit(ctx, tx, in.TenantID, in.Actor, "connector.certification.run", "connector_certification", in.ID, in.RequestID, map[string]any{
		"provider_code": in.ProviderCode, "payment_method_code": in.PaymentMethodCode, "environment": in.Environment, "status": in.Status,
	}); err != nil {
		return model.ConnectorCertificationRun{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ConnectorCertificationRun{}, err
	}
	return s.GetConnectorCertificationRun(ctx, in.TenantID, in.ID)
}

func (s *Postgres) CertifyProviderPaymentMethodCapability(ctx context.Context, tenantID, providerCode, paymentMethodCode, runID, actor, requestID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE provider_payment_method_capabilities
		SET support_status='CERTIFIED',
			metadata=metadata || jsonb_build_object(
				'engine','emisell_native',
				'provider_api',CASE WHEN payment_method_code='card' THEN 'payment_sessions' ELSE 'payments_v3' END,
				'conformance_profile',CASE WHEN payment_method_code='card' THEN 'xendit-payment-session/card' ELSE 'xendit-payments-v3/' || payment_method_code END,
				'certification_run_id',$3::text
			),
			updated_at=now()
		WHERE provider_code=$1 AND payment_method_code=$2
	`, providerCode, paymentMethodCode, runID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err = audit(ctx, tx, tenantID, actor, "connector.capability.certify", "provider_payment_method_capability", providerCode+":"+paymentMethodCode, requestID, map[string]any{
		"provider_code": providerCode, "payment_method_code": paymentMethodCode, "certification_run_id": runID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Postgres) CertificationPaymentMatches(ctx context.Context, tenantID, paymentID, providerCode, paymentMethodCode string) (bool, error) {
	var matches bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM connector_certification_runs
			WHERE tenant_id=$1 AND payment_id=$2 AND provider_code=$3 AND payment_method_code=$4
		)
	`, tenantID, paymentID, providerCode, paymentMethodCode).Scan(&matches)
	return matches, err
}

func (s *Postgres) GetConnectorCertificationRun(ctx context.Context, tenantID, id string) (model.ConnectorCertificationRun, error) {
	item, err := scanConnectorCertificationRun(s.pool.QueryRow(ctx, connectorCertificationRunSelect+` WHERE r.tenant_id=$1 AND r.id=$2`, tenantID, id))
	return item, translateNotFound(err)
}

func (s *Postgres) ListConnectorCertificationRuns(ctx context.Context, tenantID, environment, providerCode string, limit int) ([]model.ConnectorCertificationRun, error) {
	rows, err := s.pool.Query(ctx, connectorCertificationRunSelect+`
		WHERE r.tenant_id=$1 AND ($2='' OR r.environment=$2) AND ($3='' OR r.provider_code=$3)
		ORDER BY r.completed_at DESC,r.id DESC LIMIT $4
	`, tenantID, environment, providerCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ConnectorCertificationRun, 0)
	for rows.Next() {
		item, scanErr := scanConnectorCertificationRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type CreateInstallationInput struct {
	ID, TenantID, ProviderCode, ProviderVersion, Environment, ProfileID, Actor, RequestID string
}

func (s *Postgres) CreateInstallation(ctx context.Context, in CreateInstallationInput) (model.Installation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Installation{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO provider_installations
		(id,tenant_id,provider_code,provider_version,environment,engine_profile_id,status,created_by,updated_by)
		SELECT $1,$2,p.code,pv.version,$4,$5,'CONFIG_REQUIRED',$6,$6
		FROM providers p JOIN provider_versions pv ON pv.provider_code=p.code
		WHERE p.code=$7 AND p.available=true AND pv.version=$3 AND pv.status='RELEASED'
	`, in.ID, in.TenantID, in.ProviderVersion, in.Environment, in.ProfileID, in.Actor, in.ProviderCode)
	if err != nil {
		return model.Installation{}, translateConstraint(err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_installations WHERE id=$1)`, in.ID).Scan(&exists); err != nil {
		return model.Installation{}, err
	}
	if !exists {
		return model.Installation{}, ErrNotFound
	}
	if err := audit(ctx, tx, in.TenantID, in.Actor, "provider.install", "provider_installation", in.ID, in.RequestID, map[string]any{
		"provider_code": in.ProviderCode, "environment": in.Environment,
	}); err != nil {
		return model.Installation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Installation{}, err
	}
	return s.GetInstallation(ctx, in.TenantID, in.ID)
}

func (s *Postgres) ListInstallations(ctx context.Context, tenantID, environment string) ([]model.Installation, error) {
	query := installationSelect + ` WHERE i.tenant_id=$1`
	args := []any{tenantID}
	if environment != "" {
		query += ` AND i.environment=$2`
		args = append(args, environment)
	}
	query += ` ORDER BY i.updated_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Installation, 0)
	for rows.Next() {
		item, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Postgres) GetInstallation(ctx context.Context, tenantID, id string) (model.Installation, error) {
	item, err := scanInstallation(s.pool.QueryRow(ctx, installationSelect+` WHERE i.tenant_id=$1 AND i.id=$2`, tenantID, id))
	return item, translateNotFound(err)
}

func (s *Postgres) BeginCredentialConfig(ctx context.Context, tenantID, id, actor, requestID string) (model.Installation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Installation{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE provider_installations SET status='VERIFYING',last_error='',updated_by=$3,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2 AND status IN ('CONFIG_REQUIRED','READY','INACTIVE','ERROR')
	`, tenantID, id, actor)
	if err != nil {
		return model.Installation{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, getErr := s.GetInstallation(ctx, tenantID, id); getErr != nil {
			return model.Installation{}, getErr
		}
		return model.Installation{}, ErrInvalidState
	}
	if err := audit(ctx, tx, tenantID, actor, "provider.credentials.configure", "provider_installation", id, requestID, nil); err != nil {
		return model.Installation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Installation{}, err
	}
	return s.GetInstallation(ctx, tenantID, id)
}

func (s *Postgres) RecordEngineConnector(ctx context.Context, tenantID, id, connectorID, actor string) (model.Installation, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE provider_installations SET engine_connector_id=$3,updated_by=$4,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2 AND status='VERIFYING' AND engine_connector_id IS NULL
	`, tenantID, id, connectorID, actor)
	if err != nil {
		return model.Installation{}, translateConstraint(err)
	}
	if tag.RowsAffected() == 0 {
		return model.Installation{}, ErrInvalidState
	}
	return s.GetInstallation(ctx, tenantID, id)
}

type CompleteInstallationVerificationInput struct {
	ID, TenantID, InstallationID, ProviderCode, ProviderVersion, Environment string
	ManifestDigest, ConnectorID, Actor, RequestID                            string
	Metadata, PaymentMethods                                                 []byte
	WebhookReady                                                             bool
	VerifiedAt                                                               time.Time
}

// CompleteInstallationVerification atomically promotes a verified installation
// to READY and records the immutable runtime evidence used for that decision.
func (s *Postgres) CompleteInstallationVerification(ctx context.Context, in CompleteInstallationVerificationInput) (model.Installation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return model.Installation{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE provider_installations
		SET engine_connector_id=$6,credential_metadata=$7,payment_methods=$8,
		    status='READY',last_error='',updated_by=$9,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2 AND provider_code=$3 AND provider_version=$4
		  AND environment=$5 AND status='VERIFYING'
	`, in.TenantID, in.InstallationID, in.ProviderCode, in.ProviderVersion, in.Environment,
		in.ConnectorID, in.Metadata, in.PaymentMethods, in.Actor)
	if err != nil {
		return model.Installation{}, translateConstraint(err)
	}
	if tag.RowsAffected() == 0 {
		return model.Installation{}, ErrInvalidState
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO installation_verifications
		(id,tenant_id,installation_id,provider_code,provider_version,environment,
		 manifest_digest,result,connector_id,webhook_ready,verified_by,request_id,verified_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'PASSED',$8,$9,$10,$11,$12)
	`, in.ID, in.TenantID, in.InstallationID, in.ProviderCode, in.ProviderVersion,
		in.Environment, in.ManifestDigest, in.ConnectorID, in.WebhookReady, in.Actor,
		in.RequestID, in.VerifiedAt); err != nil {
		return model.Installation{}, translateConstraint(err)
	}
	if err = audit(ctx, tx, in.TenantID, in.Actor, "provider.credentials.verified", "provider_installation", in.InstallationID, in.RequestID, map[string]any{
		"verification_id": in.ID, "provider_version": in.ProviderVersion,
		"manifest_digest": in.ManifestDigest, "environment": in.Environment,
		"webhook_ready": in.WebhookReady,
	}); err != nil {
		return model.Installation{}, err
	}
	item, err := scanInstallation(tx.QueryRow(ctx, installationSelect+` WHERE i.tenant_id=$1 AND i.id=$2`, in.TenantID, in.InstallationID))
	if err != nil {
		return model.Installation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.Installation{}, err
	}
	return item, nil
}

type FailInstallationVerificationInput struct {
	ID, TenantID, InstallationID, ProviderCode, ProviderVersion, Environment string
	ManifestDigest, ErrorCode, ErrorMessage, Actor, RequestID                string
	VerifiedAt                                                               time.Time
}

// FailInstallationVerification records a failed provider/runtime verification
// in the same transaction that moves the installation to ERROR.
func (s *Postgres) FailInstallationVerification(ctx context.Context, in FailInstallationVerificationInput) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE provider_installations
		SET status='ERROR',last_error=$6,updated_by=$7,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2 AND provider_code=$3 AND provider_version=$4
		  AND environment=$5 AND status='VERIFYING'
	`, in.TenantID, in.InstallationID, in.ProviderCode, in.ProviderVersion,
		in.Environment, truncate(in.ErrorMessage, 1000), in.Actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO installation_verifications
		(id,tenant_id,installation_id,provider_code,provider_version,environment,
		 manifest_digest,result,error_code,error_message,verified_by,request_id,verified_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,'FAILED',$8,$9,$10,$11,$12)
	`, in.ID, in.TenantID, in.InstallationID, in.ProviderCode, in.ProviderVersion,
		in.Environment, in.ManifestDigest, truncate(in.ErrorCode, 128),
		truncate(in.ErrorMessage, 1000), in.Actor, in.RequestID, in.VerifiedAt); err != nil {
		return translateConstraint(err)
	}
	if err = audit(ctx, tx, in.TenantID, in.Actor, "provider.credentials.verification_failed", "provider_installation", in.InstallationID, in.RequestID, map[string]any{
		"verification_id": in.ID, "provider_version": in.ProviderVersion,
		"manifest_digest": in.ManifestDigest, "environment": in.Environment,
		"error_code": in.ErrorCode,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Postgres) SaveProviderCredentials(ctx context.Context, tenantID, installationID string, ciphertext []byte) error {
	if len(ciphertext) == 0 {
		return ErrInvalidState
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO provider_credentials(installation_id,tenant_id,ciphertext)
		SELECT id,tenant_id,$3 FROM provider_installations
		WHERE tenant_id=$1 AND id=$2 AND status='VERIFYING'
		ON CONFLICT(installation_id) DO UPDATE SET
			ciphertext=EXCLUDED.ciphertext,key_version=provider_credentials.key_version+1,updated_at=now()
	`, tenantID, installationID, ciphertext)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}

func (s *Postgres) GetProviderCredentials(ctx context.Context, tenantID, installationID string) ([]byte, error) {
	var ciphertext []byte
	err := s.pool.QueryRow(ctx, `
		SELECT c.ciphertext FROM provider_credentials c
		JOIN provider_installations i ON i.tenant_id=c.tenant_id AND i.id=c.installation_id
		WHERE c.tenant_id=$1 AND c.installation_id=$2 AND i.status <> 'UNINSTALLED'
	`, tenantID, installationID).Scan(&ciphertext)
	return ciphertext, translateNotFound(err)
}

func (s *Postgres) GetProviderCredentialsByInstallationID(ctx context.Context, installationID string) (string, []byte, error) {
	var tenantID string
	var ciphertext []byte
	err := s.pool.QueryRow(ctx, `
		SELECT c.tenant_id,c.ciphertext FROM provider_credentials c
		JOIN provider_installations i ON i.tenant_id=c.tenant_id AND i.id=c.installation_id
		WHERE c.installation_id=$1 AND i.status <> 'UNINSTALLED'
	`, installationID).Scan(&tenantID, &ciphertext)
	return tenantID, ciphertext, translateNotFound(err)
}

func (s *Postgres) DeleteProviderCredentials(ctx context.Context, tenantID, installationID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM provider_credentials WHERE tenant_id=$1 AND installation_id=$2`, tenantID, installationID)
	return err
}

func (s *Postgres) FailInstallation(ctx context.Context, tenantID, id, message, actor string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE provider_installations SET status='ERROR',last_error=$3,updated_by=$4,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2 AND status <> 'UNINSTALLED'
	`, tenantID, id, truncate(message, 1000), actor)
	return err
}

func (s *Postgres) TransitionInstallation(ctx context.Context, tenantID, id, target, actor, requestID string, expectedVersion int64) (model.Installation, error) {
	allowed := map[string][]string{
		model.InstallationActive:   {model.InstallationReady, model.InstallationInactive},
		model.InstallationInactive: {model.InstallationActive},
	}
	from, ok := allowed[target]
	if !ok {
		return model.Installation{}, ErrInvalidState
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return model.Installation{}, err
	}
	defer tx.Rollback(ctx)
	var current string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT status,version FROM provider_installations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&current, &version); err != nil {
		return model.Installation{}, translateNotFound(err)
	}
	if expectedVersion > 0 && expectedVersion != version {
		return model.Installation{}, ErrVersionConflict
	}
	if !contains(from, current) {
		return model.Installation{}, ErrInvalidState
	}
	if _, err := tx.Exec(ctx, `UPDATE provider_installations SET status=$3,last_error='',updated_by=$4,updated_at=now(),version=version+1 WHERE tenant_id=$1 AND id=$2`, tenantID, id, target, actor); err != nil {
		return model.Installation{}, err
	}
	action := "provider.deactivate"
	if target == model.InstallationActive {
		action = "provider.activate"
	}
	if err := audit(ctx, tx, tenantID, actor, action, "provider_installation", id, requestID, map[string]any{"from": current, "to": target}); err != nil {
		return model.Installation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Installation{}, err
	}
	return s.GetInstallation(ctx, tenantID, id)
}

func (s *Postgres) UpgradeInstallation(ctx context.Context, tenantID, id, providerVersion, actor, requestID string, expectedVersion int64) (model.Installation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return model.Installation{}, err
	}
	defer tx.Rollback(ctx)
	var providerCode, currentVersion, currentStatus string
	var version int64
	if err := tx.QueryRow(ctx, `
		SELECT provider_code,provider_version,status,version
		FROM provider_installations
		WHERE tenant_id=$1 AND id=$2
		FOR UPDATE
	`, tenantID, id).Scan(&providerCode, &currentVersion, &currentStatus, &version); err != nil {
		return model.Installation{}, translateNotFound(err)
	}
	if expectedVersion != version {
		return model.Installation{}, ErrVersionConflict
	}
	if currentStatus != model.InstallationInactive || currentVersion == providerVersion {
		return model.Installation{}, ErrInvalidState
	}
	var released bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM provider_versions
			WHERE provider_code=$1 AND version=$2 AND status='RELEASED'
		)
	`, providerCode, providerVersion).Scan(&released); err != nil {
		return model.Installation{}, err
	}
	if !released {
		return model.Installation{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE provider_installations
		SET provider_version=$3,
		    engine_connector_id=NULL,
		    status='CONFIG_REQUIRED',
		    credential_metadata=(credential_metadata - 'verified_environment' - 'webhook_ready' - 'verification') ||
		        jsonb_build_object(
		            'verification_required',true,
		            'verification_reason','provider_version_upgrade',
		            'previous_provider_version',$5::text
		        ),
		    last_error='',updated_by=$4,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, id, providerVersion, actor, currentVersion); err != nil {
		return model.Installation{}, err
	}
	if err := audit(ctx, tx, tenantID, actor, "provider.upgrade", "provider_installation", id, requestID, map[string]any{
		"from_version":          currentVersion,
		"to_version":            providerVersion,
		"verification_required": true,
	}); err != nil {
		return model.Installation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Installation{}, err
	}
	return s.GetInstallation(ctx, tenantID, id)
}

func (s *Postgres) MarkUninstalled(ctx context.Context, tenantID, id, actor, requestID string) (model.Installation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Installation{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE provider_installations SET status='UNINSTALLED',uninstalled_at=now(),updated_by=$3,
		updated_at=now(),version=version+1 WHERE tenant_id=$1 AND id=$2 AND status <> 'UNINSTALLED'
	`, tenantID, id, actor)
	if err != nil {
		return model.Installation{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.Installation{}, ErrInvalidState
	}
	if err := audit(ctx, tx, tenantID, actor, "provider.uninstall", "provider_installation", id, requestID, nil); err != nil {
		return model.Installation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Installation{}, err
	}
	return s.GetInstallation(ctx, tenantID, id)
}

type UpsertPaymentMethodAssignmentInput struct {
	ID, TenantID, Environment, PaymentMethodCode, PaymentMethod, PaymentMethodType, InstallationID, Label, Actor, RequestID string
	ExpectedVersion                                                                                                         int64
}

func (s *Postgres) ListPaymentMethodAssignments(ctx context.Context, tenantID, environment string) ([]model.PaymentMethodAssignment, error) {
	query := paymentMethodAssignmentSelect + ` WHERE a.tenant_id=$1`
	args := []any{tenantID}
	if environment != "" {
		query += ` AND a.environment=$2`
		args = append(args, environment)
	}
	query += ` ORDER BY a.environment,a.payment_method_code`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.PaymentMethodAssignment, 0)
	for rows.Next() {
		item, scanErr := scanPaymentMethodAssignment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Postgres) ListPaymentOptions(ctx context.Context, tenantID, environment string) ([]model.PaymentOption, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id,a.environment,a.payment_method_code,m.category,a.label
		FROM payment_method_assignments a
		JOIN provider_installations i ON i.tenant_id=a.tenant_id AND i.id=a.installation_id
		JOIN payment_methods m ON m.code=a.payment_method_code
		JOIN provider_payment_method_capabilities c ON c.provider_code=i.provider_code
			AND c.payment_method_code=a.payment_method_code AND c.support_status='CERTIFIED'
		WHERE a.tenant_id=$1 AND a.environment=$2 AND a.status='ACTIVE' AND i.status='ACTIVE'
		ORDER BY m.sort_order,m.name
	`, tenantID, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.PaymentOption, 0)
	for rows.Next() {
		var item model.PaymentOption
		if err = rows.Scan(&item.ID, &item.Environment, &item.PaymentMethodCode, &item.Category, &item.Label); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Postgres) GetActivePaymentOption(ctx context.Context, tenantID, environment, id string) (model.PaymentMethodAssignment, error) {
	item, err := scanPaymentMethodAssignment(s.pool.QueryRow(ctx, paymentMethodAssignmentSelect+`
		WHERE a.tenant_id=$1 AND a.environment=$2 AND a.id=$3
		  AND a.status='ACTIVE' AND i.status='ACTIVE'
		  AND EXISTS (
			SELECT 1 FROM provider_payment_method_capabilities c
			WHERE c.provider_code=i.provider_code AND c.payment_method_code=a.payment_method_code
			  AND c.support_status='CERTIFIED'
		  )
	`, tenantID, environment, id))
	return item, translateNotFound(err)
}

func (s *Postgres) GetPaymentMethodAssignment(ctx context.Context, tenantID, id string) (model.PaymentMethodAssignment, error) {
	item, err := scanPaymentMethodAssignment(s.pool.QueryRow(ctx, paymentMethodAssignmentSelect+` WHERE a.tenant_id=$1 AND a.id=$2`, tenantID, id))
	return item, translateNotFound(err)
}

func (s *Postgres) UpsertPaymentMethodAssignment(ctx context.Context, in UpsertPaymentMethodAssignmentInput) (model.PaymentMethodAssignment, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentMethodAssignment{}, false, err
	}
	defer tx.Rollback(ctx)
	var installationEnvironment, installationStatus string
	if err = tx.QueryRow(ctx, `
		SELECT environment,status FROM provider_installations
		WHERE tenant_id=$1 AND id=$2 FOR UPDATE
	`, in.TenantID, in.InstallationID).Scan(&installationEnvironment, &installationStatus); err != nil {
		return model.PaymentMethodAssignment{}, false, translateNotFound(err)
	}
	if installationEnvironment != in.Environment || installationStatus != model.InstallationActive {
		return model.PaymentMethodAssignment{}, false, ErrInvalidState
	}
	var id string
	var version int64
	err = tx.QueryRow(ctx, `
		SELECT id,version FROM payment_method_assignments
		WHERE tenant_id=$1 AND environment=$2 AND payment_method_code=$3
		FOR UPDATE
	`, in.TenantID, in.Environment, in.PaymentMethodCode).Scan(&id, &version)
	created := false
	action := "payment_method.assign"
	if errors.Is(err, pgx.ErrNoRows) {
		if in.ExpectedVersion != 0 {
			return model.PaymentMethodAssignment{}, false, ErrVersionConflict
		}
		id = in.ID
		_, err = tx.Exec(ctx, `
			INSERT INTO payment_method_assignments
			(id,tenant_id,environment,payment_method_code,payment_method,payment_method_type,installation_id,label,status,created_by,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,'ACTIVE',$9,$9)
		`, id, in.TenantID, in.Environment, in.PaymentMethodCode, in.PaymentMethod, in.PaymentMethodType, in.InstallationID, in.Label, in.Actor)
		created = true
	} else if err != nil {
		return model.PaymentMethodAssignment{}, false, err
	} else {
		if in.ExpectedVersion != version {
			return model.PaymentMethodAssignment{}, false, ErrVersionConflict
		}
		_, err = tx.Exec(ctx, `
			UPDATE payment_method_assignments SET installation_id=$3,payment_method=$4,payment_method_type=$5,label=$6,status='ACTIVE',updated_by=$7,
				updated_at=now(),version=version+1 WHERE tenant_id=$1 AND id=$2
		`, in.TenantID, id, in.InstallationID, in.PaymentMethod, in.PaymentMethodType, in.Label, in.Actor)
		action = "payment_method.reassign"
	}
	if err != nil {
		return model.PaymentMethodAssignment{}, false, translateConstraint(err)
	}
	if err = audit(ctx, tx, in.TenantID, in.Actor, action, "payment_method_assignment", id, in.RequestID, map[string]any{
		"environment": in.Environment, "payment_method_code": in.PaymentMethodCode,
		"provider_method": in.PaymentMethod, "provider_method_type": in.PaymentMethodType,
		"installation_id": in.InstallationID,
	}); err != nil {
		return model.PaymentMethodAssignment{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.PaymentMethodAssignment{}, false, err
	}
	item, err := s.GetPaymentMethodAssignment(ctx, in.TenantID, id)
	return item, created, err
}

func (s *Postgres) DeactivatePaymentMethodAssignment(ctx context.Context, tenantID, id, actor, requestID string, expectedVersion int64) (model.PaymentMethodAssignment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentMethodAssignment{}, err
	}
	defer tx.Rollback(ctx)
	var version int64
	var status string
	if err = tx.QueryRow(ctx, `SELECT version,status FROM payment_method_assignments WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&version, &status); err != nil {
		return model.PaymentMethodAssignment{}, translateNotFound(err)
	}
	if version != expectedVersion {
		return model.PaymentMethodAssignment{}, ErrVersionConflict
	}
	if status != model.PaymentMethodAssignmentActive {
		return model.PaymentMethodAssignment{}, ErrInvalidState
	}
	if _, err = tx.Exec(ctx, `
		UPDATE payment_method_assignments SET status='INACTIVE',updated_by=$3,updated_at=now(),version=version+1
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, id, actor); err != nil {
		return model.PaymentMethodAssignment{}, err
	}
	if err = audit(ctx, tx, tenantID, actor, "payment_method.deactivate", "payment_method_assignment", id, requestID, nil); err != nil {
		return model.PaymentMethodAssignment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.PaymentMethodAssignment{}, err
	}
	return s.GetPaymentMethodAssignment(ctx, tenantID, id)
}

type ReservePaymentInput struct {
	ID, TenantID, InstallationID, PaymentOptionID, PaymentMethodCode, ProviderCode, ProviderVersion, Environment, MerchantReference, IdempotencyKey, Currency, ExecutionEngine string
	RequestHash                                                                                                                                                                []byte
	Amount                                                                                                                                                                     int64
}

type PaymentListFilter struct {
	Environment, Status, Provider, Query string
	Limit, Offset                        int
}

type ReconciliationListFilter struct {
	Kind, Query   string
	Limit, Offset int
}

type IntegrationReadinessFacts struct {
	ActiveInstallation bool
	ActiveAssignment   bool
	PaymentCreated     bool
	IdempotencyReplay  bool
	PaymentStatusRead  bool
	WebhookDelivered   bool
	ResilienceObserved bool
}

func (s *Postgres) RecordIntegrationEvidence(ctx context.Context, tenantID, environment, code string, details any) error {
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO integration_evidence(tenant_id,environment,code,details)
		VALUES($1,$2,$3,$4::jsonb)
		ON CONFLICT(tenant_id,environment,code) DO UPDATE SET
			observation_count=integration_evidence.observation_count+1,
			details=EXCLUDED.details,
			last_observed_at=now()
	`, tenantID, environment, code, string(payload))
	return err
}

func (s *Postgres) GetIntegrationReadinessFacts(ctx context.Context, tenantID, environment string) (IntegrationReadinessFacts, error) {
	var facts IntegrationReadinessFacts
	err := s.pool.QueryRow(ctx, `
		SELECT
			EXISTS(
				SELECT 1 FROM provider_installations
				WHERE tenant_id=$1 AND environment=$2 AND status='ACTIVE'
			),
			EXISTS(
				SELECT 1 FROM payment_method_assignments assignment
				JOIN provider_installations installation
				  ON installation.tenant_id=assignment.tenant_id
				 AND installation.id=assignment.installation_id
				WHERE assignment.tenant_id=$1 AND assignment.environment=$2
				  AND assignment.status='ACTIVE' AND installation.status='ACTIVE'
			),
			EXISTS(
				SELECT 1 FROM payment_sessions
				WHERE tenant_id=$1 AND environment=$2
			),
			EXISTS(
				SELECT 1 FROM integration_evidence
				WHERE tenant_id=$1 AND environment=$2 AND code='idempotency_replay'
			),
			EXISTS(
				SELECT 1 FROM integration_evidence
				WHERE tenant_id=$1 AND environment=$2 AND code='payment_status_read'
			),
			EXISTS(
				SELECT 1 FROM outbox_events delivery
				JOIN payment_sessions payment
				  ON payment.tenant_id=delivery.tenant_id
				 AND payment.id=delivery.aggregate_id
				WHERE delivery.tenant_id=$1 AND payment.environment=$2
				  AND delivery.event_type='payment.updated' AND delivery.status='DELIVERED'
			),
			EXISTS(
				SELECT 1 FROM payment_sessions
				WHERE tenant_id=$1 AND environment=$2
				  AND flags ?| ARRAY['late_payment','provider_delayed_confirmation']
			)
	`, tenantID, environment).Scan(
		&facts.ActiveInstallation, &facts.ActiveAssignment, &facts.PaymentCreated,
		&facts.IdempotencyReplay, &facts.PaymentStatusRead, &facts.WebhookDelivered,
		&facts.ResilienceObserved,
	)
	return facts, err
}

func (s *Postgres) ListPayments(ctx context.Context, tenantID string, filter PaymentListFilter) (model.PaymentList, error) {
	result := model.PaymentList{Items: make([]model.PaymentSession, 0), Limit: filter.Limit, Offset: filter.Offset}
	const where = ` WHERE tenant_id=$1
		AND ($2='' OR environment=$2)
		AND ($3='' OR status=$3)
		AND ($4='' OR provider_code=$4)
		AND ($5='' OR id ILIKE '%' || $5 || '%' OR merchant_reference ILIKE '%' || $5 || '%' OR COALESCE(engine_payment_id,'') ILIKE '%' || $5 || '%')`
	args := []any{tenantID, filter.Environment, filter.Status, filter.Provider, filter.Query}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM payment_sessions`+where, args...).Scan(&result.Total); err != nil {
		return model.PaymentList{}, err
	}
	rows, err := s.pool.Query(ctx, paymentListSelect+where+` ORDER BY updated_at DESC,id DESC LIMIT $6 OFFSET $7`, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return model.PaymentList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, _, scanErr := scanPayment(rows)
		if scanErr != nil {
			return model.PaymentList{}, scanErr
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return model.PaymentList{}, err
	}
	result.HasMore = int64(result.Offset+len(result.Items)) < result.Total
	return result, nil
}

func (s *Postgres) ReservePayment(ctx context.Context, in ReservePaymentInput) (model.PaymentSession, bool, error) {
	if in.ExecutionEngine == "" {
		in.ExecutionEngine = "emisell_native"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentSession{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.TenantID+":"+in.Environment+":"+in.IdempotencyKey); err != nil {
		return model.PaymentSession{}, false, err
	}
	item, hash, err := scanPayment(tx.QueryRow(ctx, paymentSelect+` WHERE tenant_id=$1 AND environment=$2 AND idempotency_key=$3`, in.TenantID, in.Environment, in.IdempotencyKey))
	existing := paymentWithHash{item: item, hash: hash}
	if err == nil {
		if !bytes.Equal(existing.hash, in.RequestHash) {
			return model.PaymentSession{}, false, ErrIdempotencyConflict
		}
		return existing.item, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return model.PaymentSession{}, false, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO payment_sessions
		(id,tenant_id,installation_id,payment_option_id,payment_method_code,provider_code,provider_version,environment,merchant_reference,idempotency_key,request_hash,amount,currency,status,execution_engine)
		VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,'CREATED',$14)
	`, in.ID, in.TenantID, in.InstallationID, in.PaymentOptionID, in.PaymentMethodCode, in.ProviderCode, in.ProviderVersion, in.Environment, in.MerchantReference, in.IdempotencyKey, in.RequestHash, in.Amount, in.Currency, in.ExecutionEngine)
	if err != nil {
		if isUnique(err) {
			return model.PaymentSession{}, false, ErrConflict
		}
		return model.PaymentSession{}, false, err
	}
	if err = insertPaymentHistory(ctx, tx, in.TenantID, in.ID, model.PaymentCreated, "api.create", map[string]any{"merchant_reference": in.MerchantReference}); err != nil {
		return model.PaymentSession{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.PaymentSession{}, false, err
	}
	item, err = s.GetPayment(ctx, in.TenantID, in.ID)
	return item, true, err
}

func (s *Postgres) CompletePayment(ctx context.Context, tenantID, id, engineID, connectorID, status, source string, nextAction []byte) (model.PaymentSession, error) {
	if len(nextAction) == 0 {
		nextAction = []byte("null")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentSession{}, err
	}
	defer tx.Rollback(ctx)
	var previous string
	var existingFlags []string
	if err = tx.QueryRow(ctx, `SELECT status,flags FROM payment_sessions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&previous, &existingFlags); err != nil {
		return model.PaymentSession{}, translateNotFound(err)
	}
	applied := canonicalPaymentStatus(previous, status)
	flags, flagsAdded := canonicalPaymentFlags(existingFlags, previous, applied)
	flagsJSON, err := json.Marshal(flags)
	if err != nil {
		return model.PaymentSession{}, err
	}
	err = tx.QueryRow(ctx, `
		UPDATE payment_sessions SET
		updated_at=CASE WHEN engine_payment_id IS DISTINCT FROM $3
		  OR connector_transaction_id IS DISTINCT FROM $4
		  OR status IS DISTINCT FROM $5
		  OR next_action IS DISTINCT FROM $6::jsonb
		  OR flags IS DISTINCT FROM $7::jsonb
		  OR last_error<>'' THEN now() ELSE updated_at END,
		engine_payment_id=$3,connector_transaction_id=$4,status=$5,
		next_action=$6,flags=$7::jsonb,last_error='' WHERE tenant_id=$1 AND id=$2 RETURNING status
	`, tenantID, id, engineID, connectorID, applied, nextAction, string(flagsJSON)).Scan(&applied)
	if err != nil {
		return model.PaymentSession{}, err
	}
	if previous != applied {
		if err = insertPaymentHistory(ctx, tx, tenantID, id, applied, source, map[string]any{"engine_payment_id": engineID, "flags_added": flagsAdded}); err != nil {
			return model.PaymentSession{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return model.PaymentSession{}, err
	}
	return s.GetPayment(ctx, tenantID, id)
}

func (s *Postgres) FailPayment(ctx context.Context, tenantID, id, status, source, message string) (model.PaymentSession, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentSession{}, err
	}
	defer tx.Rollback(ctx)
	var previous string
	if err = tx.QueryRow(ctx, `SELECT status FROM payment_sessions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&previous); err != nil {
		return model.PaymentSession{}, translateNotFound(err)
	}
	_, err = tx.Exec(ctx, `UPDATE payment_sessions SET status=$3,last_error=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, status, truncate(message, 1000))
	if err != nil {
		return model.PaymentSession{}, err
	}
	if previous != status {
		if err = insertPaymentHistory(ctx, tx, tenantID, id, status, source, map[string]any{"error": truncate(message, 1000)}); err != nil {
			return model.PaymentSession{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return model.PaymentSession{}, err
	}
	return s.GetPayment(ctx, tenantID, id)
}

func (s *Postgres) GetPayment(ctx context.Context, tenantID, id string) (model.PaymentSession, error) {
	item, _, err := scanPayment(s.pool.QueryRow(ctx, paymentSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	return item, translateNotFound(err)
}

func (s *Postgres) ReconcilePayment(ctx context.Context, tenantID, id, actor, requestID, idempotencyKey, engineID, connectorID, status string, nextAction []byte, expectedCount int) (model.PaymentSession, error) {
	if len(nextAction) == 0 {
		nextAction = []byte("null")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.PaymentSession{}, err
	}
	defer tx.Rollback(ctx)
	var current, lastKey string
	var existingFlags []string
	var reconciliationCount int
	if err = tx.QueryRow(ctx, `SELECT status,flags,reconciliation_count,last_reconciliation_key FROM payment_sessions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&current, &existingFlags, &reconciliationCount, &lastKey); err != nil {
		return model.PaymentSession{}, translateNotFound(err)
	}
	if lastKey == idempotencyKey {
		_ = tx.Rollback(ctx)
		return s.GetPayment(ctx, tenantID, id)
	}
	if current != model.PaymentUnknown || reconciliationCount != expectedCount {
		return model.PaymentSession{}, ErrInvalidState
	}
	applied := canonicalPaymentStatus(current, status)
	flags, flagsAdded := canonicalPaymentFlags(existingFlags, current, applied)
	flagsJSON, err := json.Marshal(flags)
	if err != nil {
		return model.PaymentSession{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE payment_sessions SET engine_payment_id=$3,connector_transaction_id=$4,status=$5,next_action=$6,flags=$9::jsonb,
		last_error='',reconciliation_count=reconciliation_count+1,last_reconciled_at=now(),
		last_reconciled_by=$7,last_reconciliation_key=$8,updated_at=now()
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, id, engineID, connectorID, applied, nextAction, actor, idempotencyKey, string(flagsJSON)); err != nil {
		return model.PaymentSession{}, err
	}
	if current != applied {
		if err = insertPaymentHistory(ctx, tx, tenantID, id, applied, "operator.reconcile", map[string]any{"engine_payment_id": engineID, "flags_added": flagsAdded}); err != nil {
			return model.PaymentSession{}, err
		}
	}
	if err = audit(ctx, tx, tenantID, actor, "payment.reconcile", "payment", id, requestID, map[string]any{"previous_status": current, "engine_status": status, "applied_status": applied, "previous_reconciliation_count": reconciliationCount}); err != nil {
		return model.PaymentSession{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.PaymentSession{}, err
	}
	return s.GetPayment(ctx, tenantID, id)
}

func (s *Postgres) ListReconciliationCases(ctx context.Context, tenantID string, filter ReconciliationListFilter) (model.ReconciliationList, error) {
	result := model.ReconciliationList{Items: make([]model.ReconciliationCase, 0), Counts: map[string]int64{}, Limit: filter.Limit, Offset: filter.Offset}
	const cases = ` WITH reconciliation_cases AS (
		SELECT 'payment:' || id AS id,'PAYMENT_UNKNOWN' AS kind,'payment' AS resource_type,id AS resource_id,
			'Payment outcome unknown' AS title,merchant_reference AS reference,provider_code,environment,
			COALESCE(engine_payment_id,'') AS engine_reference,status AS current_status,'HIGH' AS severity,
			'SYNC_ENGINE' AS recommended_action,(COALESCE(engine_payment_id,'')<>'') AS can_resolve,
			reconciliation_count,last_error,updated_at AS detected_at,tenant_id
		FROM payment_sessions WHERE status='UNKNOWN'
		UNION ALL
		SELECT 'refund:' || r.id,'REFUND_UNKNOWN','refund',r.id,'Refund outcome unknown',r.payment_id,p.provider_code,p.environment,
			COALESCE(r.engine_refund_id,''),r.status,'HIGH','SYNC_ENGINE',false,0,r.last_error,r.updated_at,r.tenant_id
		FROM refunds r JOIN payment_sessions p ON p.tenant_id=r.tenant_id AND p.id=r.payment_id WHERE r.status='UNKNOWN'
		UNION ALL
		SELECT 'delivery:' || id,'DELIVERY_DEAD','outbox_event',id,'Emisell delivery exhausted',aggregate_id,'','',
			'',status,'HIGH','REPLAY_DELIVERY',false,replay_count,last_error,updated_at,tenant_id
		FROM outbox_events WHERE status='DEAD'
		UNION ALL
		SELECT 'webhook:' || id,'WEBHOOK_FAILED','webhook_inbox',id,'Incoming webhook failed',external_event_id,'','',
			'',status,'MEDIUM','INSPECT_WEBHOOK',false,0,error_message,received_at,tenant_id
		FROM webhook_inbox WHERE status='FAILED' AND tenant_id<>''
		UNION ALL
		SELECT 'installation:' || id,'INSTALLATION_ERROR','provider_installation',id,'Provider installation needs review',id,provider_code,environment,
			COALESCE(engine_connector_id,''),status,'MEDIUM','REVIEW_CONNECTOR',false,0,last_error,updated_at,tenant_id
		FROM provider_installations WHERE status='ERROR'
	) `
	const where = ` WHERE tenant_id=$1 AND ($2='' OR kind=$2)
		AND ($3='' OR id ILIKE '%' || $3 || '%' OR resource_id ILIKE '%' || $3 || '%' OR reference ILIKE '%' || $3 || '%' OR last_error ILIKE '%' || $3 || '%')`
	if err := s.pool.QueryRow(ctx, cases+`SELECT count(*) FROM reconciliation_cases`+where, tenantID, filter.Kind, filter.Query).Scan(&result.Total); err != nil {
		return model.ReconciliationList{}, err
	}
	countRows, err := s.pool.Query(ctx, cases+`SELECT kind,count(*) FROM reconciliation_cases WHERE tenant_id=$1 GROUP BY kind`, tenantID)
	if err != nil {
		return model.ReconciliationList{}, err
	}
	for countRows.Next() {
		var kind string
		var count int64
		if err = countRows.Scan(&kind, &count); err != nil {
			countRows.Close()
			return model.ReconciliationList{}, err
		}
		result.Counts[kind] = count
	}
	countRows.Close()
	rows, err := s.pool.Query(ctx, cases+`SELECT id,kind,resource_type,resource_id,title,reference,provider_code,environment,engine_reference,current_status,severity,recommended_action,can_resolve,reconciliation_count,last_error,detected_at FROM reconciliation_cases`+where+` ORDER BY CASE severity WHEN 'HIGH' THEN 1 ELSE 2 END,detected_at DESC,id LIMIT $4 OFFSET $5`, tenantID, filter.Kind, filter.Query, filter.Limit, filter.Offset)
	if err != nil {
		return model.ReconciliationList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.ReconciliationCase
		if err = rows.Scan(&item.ID, &item.Kind, &item.ResourceType, &item.ResourceID, &item.Title, &item.Reference, &item.ProviderCode, &item.Environment, &item.EngineReference, &item.CurrentStatus, &item.Severity, &item.RecommendedAction, &item.CanResolve, &item.ReconciliationCount, &item.LastError, &item.DetectedAt); err != nil {
			return model.ReconciliationList{}, err
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return model.ReconciliationList{}, err
	}
	result.HasMore = int64(result.Offset+len(result.Items)) < result.Total
	return result, nil
}

func (s *Postgres) PaymentTimeline(ctx context.Context, tenantID, id string) ([]model.PaymentStatusEvent, error) {
	if _, err := s.GetPayment(ctx, tenantID, id); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,payment_id,status,source,details,created_at FROM payment_status_history WHERE tenant_id=$1 AND payment_id=$2 ORDER BY created_at,id`, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.PaymentStatusEvent, 0)
	for rows.Next() {
		var item model.PaymentStatusEvent
		if err = rows.Scan(&item.ID, &item.PaymentID, &item.Status, &item.Source, &item.Details, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type paymentWithHash struct {
	item model.PaymentSession
	hash []byte
}

func (s *Postgres) findPaymentByIdempotency(ctx context.Context, tenantID, environment, key string) (paymentWithHash, error) {
	item, hash, err := scanPayment(s.pool.QueryRow(ctx, paymentSelect+` WHERE tenant_id=$1 AND environment=$2 AND idempotency_key=$3`, tenantID, environment, key))
	return paymentWithHash{item: item, hash: hash}, translateNotFound(err)
}

func (s *Postgres) FindPaymentByIdempotency(ctx context.Context, tenantID, environment, key string, requestHash []byte) (model.PaymentSession, bool, error) {
	existing, err := s.findPaymentByIdempotency(ctx, tenantID, environment, key)
	if errors.Is(err, ErrNotFound) {
		return model.PaymentSession{}, false, nil
	}
	if err != nil {
		return model.PaymentSession{}, false, err
	}
	if !bytes.Equal(existing.hash, requestHash) {
		return model.PaymentSession{}, false, ErrIdempotencyConflict
	}
	return existing.item, true, nil
}

type ReserveRefundInput struct {
	ID, TenantID, PaymentID, PaymentMethodCode, IdempotencyKey, Currency, ExecutionEngine string
	Reason, RequestedBy, RequestID                                                        string
	RequestHash                                                                           []byte
	Amount                                                                                int64
	AllowPartial, AllowMultiple                                                           bool
}

func (s *Postgres) ReserveRefund(ctx context.Context, in ReserveRefundInput) (model.Refund, bool, error) {
	if in.ExecutionEngine == "" {
		in.ExecutionEngine = "emisell_native"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Refund{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.TenantID+":"+in.IdempotencyKey); err != nil {
		return model.Refund{}, false, err
	}
	item, hash, err := scanRefund(tx.QueryRow(ctx, refundSelect+` WHERE tenant_id=$1 AND idempotency_key=$2`, in.TenantID, in.IdempotencyKey))
	if err == nil {
		if !bytes.Equal(hash, in.RequestHash) {
			return model.Refund{}, false, ErrIdempotencyConflict
		}
		return item, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return model.Refund{}, false, err
	}
	var paymentAmount, reservedAmount int64
	var reservedCount int
	var paymentStatus string
	if err = tx.QueryRow(ctx, `SELECT amount,status FROM payment_sessions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, in.TenantID, in.PaymentID).Scan(&paymentAmount, &paymentStatus); err != nil {
		return model.Refund{}, false, translateNotFound(err)
	}
	if paymentStatus != model.PaymentSucceeded {
		return model.Refund{}, false, ErrInvalidState
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0),count(*) FROM refunds WHERE tenant_id=$1 AND payment_id=$2 AND status <> 'FAILED'`, in.TenantID, in.PaymentID).Scan(&reservedAmount, &reservedCount); err != nil {
		return model.Refund{}, false, err
	}
	if in.Amount <= 0 || reservedAmount+in.Amount > paymentAmount || (!in.AllowPartial && in.Amount != paymentAmount) || (!in.AllowMultiple && reservedCount > 0) {
		return model.Refund{}, false, ErrInvalidState
	}
	_, err = tx.Exec(ctx, `INSERT INTO refunds(id,tenant_id,payment_id,payment_method_code,idempotency_key,request_hash,amount,currency,reason,requested_by,status,execution_engine) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,'CREATED',$11)`, in.ID, in.TenantID, in.PaymentID, in.PaymentMethodCode, in.IdempotencyKey, in.RequestHash, in.Amount, in.Currency, in.Reason, in.RequestedBy, in.ExecutionEngine)
	if err != nil {
		if isUnique(err) {
			return model.Refund{}, false, ErrConflict
		}
		return model.Refund{}, false, err
	}
	if err = audit(ctx, tx, in.TenantID, in.RequestedBy, "refund.create", "refund", in.ID, in.RequestID, map[string]any{
		"payment_id": in.PaymentID, "payment_method_code": in.PaymentMethodCode,
		"amount": in.Amount, "currency": in.Currency, "reason": in.Reason,
	}); err != nil {
		return model.Refund{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.Refund{}, false, err
	}
	item, err = s.GetRefund(ctx, in.TenantID, in.ID)
	return item, true, err
}
func (s *Postgres) CompleteRefund(ctx context.Context, tenantID, id, engineID, status string) (model.Refund, error) {
	_, err := s.pool.Exec(ctx, `UPDATE refunds SET engine_refund_id=$3,status=CASE WHEN refunds.status='SUCCEEDED' AND $4<>'SUCCEEDED' THEN refunds.status WHEN refunds.status='FAILED' AND $4<>'SUCCEEDED' THEN refunds.status ELSE $4 END,last_error='',updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, engineID, status)
	if err != nil {
		return model.Refund{}, err
	}
	return s.GetRefund(ctx, tenantID, id)
}
func (s *Postgres) FailRefund(ctx context.Context, tenantID, id, status, message string) (model.Refund, error) {
	_, err := s.pool.Exec(ctx, `UPDATE refunds SET status=$3,last_error=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, status, truncate(message, 1000))
	if err != nil {
		return model.Refund{}, err
	}
	return s.GetRefund(ctx, tenantID, id)
}
func (s *Postgres) GetRefund(ctx context.Context, tenantID, id string) (model.Refund, error) {
	item, _, err := scanRefund(s.pool.QueryRow(ctx, refundSelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	return item, translateNotFound(err)
}

// HasOpenRefundLiability prevents credential destruction while a successful
// payment is still inside a documented provider refund window or a refund is
// awaiting its provider-confirmed terminal status.
func (s *Postgres) HasOpenRefundLiability(ctx context.Context, tenantID, installationID string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM payment_sessions payment
			JOIN provider_payment_method_capabilities capability
			  ON capability.provider_code=payment.provider_code
			 AND capability.payment_method_code=payment.payment_method_code
			WHERE payment.tenant_id=$1
			  AND payment.installation_id=$2
			  AND payment.status='SUCCEEDED'
			  AND COALESCE((capability.metadata #>> '{refund,supported}')::boolean,false)
			  AND (
				EXISTS (
					SELECT 1 FROM refunds pending
					WHERE pending.tenant_id=payment.tenant_id AND pending.payment_id=payment.id
					  AND pending.status IN ('CREATED','PROCESSING','PENDING','UNKNOWN')
				)
				OR (
					(
						NULLIF(capability.metadata #>> '{refund,window_days}','') IS NULL
						OR payment.updated_at + ((capability.metadata #>> '{refund,window_days}') || ' days')::interval > now()
					)
					AND COALESCE((
						SELECT sum(refund.amount) FROM refunds refund
						WHERE refund.tenant_id=payment.tenant_id AND refund.payment_id=payment.id
						  AND refund.status <> 'FAILED'
					),0) < payment.amount
				)
			  )
		)
	`, tenantID, installationID).Scan(&found)
	return found, err
}
func (s *Postgres) getRefundByIdempotency(ctx context.Context, tenantID, key string) (model.Refund, []byte, error) {
	item, hash, err := scanRefund(s.pool.QueryRow(ctx, refundSelect+` WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, key))
	return item, hash, translateNotFound(err)
}

type WebhookInput struct {
	ID, Source, ExternalEventID, EventType, EnginePaymentID, EngineRefundID, Status string
	PayloadHash, PayloadCiphertext                                                  []byte
}

type WebhookListFilter struct {
	Status, Query string
	Limit, Offset int
}

func (s *Postgres) ListWebhookInbox(ctx context.Context, tenantID string, filter WebhookListFilter) (model.WebhookInboxList, error) {
	result := model.WebhookInboxList{Items: make([]model.WebhookInboxItem, 0), Counts: make(map[string]int64), Limit: filter.Limit, Offset: filter.Offset}
	const baseWhere = ` WHERE tenant_id=$1 AND ($2='' OR id ILIKE '%' || $2 || '%' OR external_event_id ILIKE '%' || $2 || '%' OR event_type ILIKE '%' || $2 || '%' OR aggregate_id ILIKE '%' || $2 || '%')`
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM webhook_inbox`+baseWhere+` AND ($3='' OR status=$3)`, tenantID, filter.Query, filter.Status).Scan(&result.Total); err != nil {
		return model.WebhookInboxList{}, err
	}
	countRows, err := s.pool.Query(ctx, `SELECT status,count(*) FROM webhook_inbox`+baseWhere+` GROUP BY status`, tenantID, filter.Query)
	if err != nil {
		return model.WebhookInboxList{}, err
	}
	for countRows.Next() {
		var status string
		var count int64
		if err = countRows.Scan(&status, &count); err != nil {
			countRows.Close()
			return model.WebhookInboxList{}, err
		}
		result.Counts[status] = count
	}
	if err = countRows.Err(); err != nil {
		countRows.Close()
		return model.WebhookInboxList{}, err
	}
	countRows.Close()
	rows, err := s.pool.Query(ctx, `SELECT id,source,external_event_id,event_type,aggregate_type,aggregate_id,encode(payload_sha256,'hex'),status,error_message,received_at,processed_at FROM webhook_inbox`+baseWhere+` AND ($3='' OR status=$3) ORDER BY received_at DESC,id DESC LIMIT $4 OFFSET $5`, tenantID, filter.Query, filter.Status, filter.Limit, filter.Offset)
	if err != nil {
		return model.WebhookInboxList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.WebhookInboxItem
		if err = rows.Scan(&item.ID, &item.Source, &item.ExternalEventID, &item.EventType, &item.AggregateType, &item.AggregateID, &item.PayloadSHA256, &item.Status, &item.ErrorMessage, &item.ReceivedAt, &item.ProcessedAt); err != nil {
			return model.WebhookInboxList{}, err
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return model.WebhookInboxList{}, err
	}
	result.HasMore = int64(result.Offset+len(result.Items)) < result.Total
	return result, nil
}

func (s *Postgres) ListWebhookDeliveries(ctx context.Context, tenantID string, filter WebhookListFilter) (model.WebhookDeliveryList, error) {
	result := model.WebhookDeliveryList{Items: make([]model.WebhookDelivery, 0), Counts: make(map[string]int64), Limit: filter.Limit, Offset: filter.Offset}
	const baseWhere = ` WHERE tenant_id=$1 AND ($2='' OR id ILIKE '%' || $2 || '%' OR event_type ILIKE '%' || $2 || '%' OR aggregate_id ILIKE '%' || $2 || '%')`
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`+baseWhere+` AND ($3='' OR status=$3)`, tenantID, filter.Query, filter.Status).Scan(&result.Total); err != nil {
		return model.WebhookDeliveryList{}, err
	}
	countRows, err := s.pool.Query(ctx, `SELECT status,count(*) FROM outbox_events`+baseWhere+` GROUP BY status`, tenantID, filter.Query)
	if err != nil {
		return model.WebhookDeliveryList{}, err
	}
	for countRows.Next() {
		var status string
		var count int64
		if err = countRows.Scan(&status, &count); err != nil {
			countRows.Close()
			return model.WebhookDeliveryList{}, err
		}
		result.Counts[status] = count
	}
	if err = countRows.Err(); err != nil {
		countRows.Close()
		return model.WebhookDeliveryList{}, err
	}
	countRows.Close()
	rows, err := s.pool.Query(ctx, webhookDeliverySelect+baseWhere+` AND ($3='' OR status=$3) ORDER BY updated_at DESC,id DESC LIMIT $4 OFFSET $5`, tenantID, filter.Query, filter.Status, filter.Limit, filter.Offset)
	if err != nil {
		return model.WebhookDeliveryList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, scanErr := scanWebhookDelivery(rows)
		if scanErr != nil {
			return model.WebhookDeliveryList{}, scanErr
		}
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return model.WebhookDeliveryList{}, err
	}
	result.HasMore = int64(result.Offset+len(result.Items)) < result.Total
	return result, nil
}

func (s *Postgres) GetWebhookDelivery(ctx context.Context, tenantID, id string) (model.WebhookDelivery, error) {
	item, err := scanWebhookDelivery(s.pool.QueryRow(ctx, webhookDeliverySelect+` WHERE tenant_id=$1 AND id=$2`, tenantID, id))
	return item, translateNotFound(err)
}

func (s *Postgres) ReplayWebhookDelivery(ctx context.Context, tenantID, id, actor, requestID, idempotencyKey string, expectedReplayCount int) (model.WebhookDelivery, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.WebhookDelivery{}, err
	}
	defer tx.Rollback(ctx)
	var status, lastError, lastReplayKey string
	var replayCount int
	if err = tx.QueryRow(ctx, `SELECT status,replay_count,last_error,last_replay_key FROM outbox_events WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&status, &replayCount, &lastError, &lastReplayKey); err != nil {
		return model.WebhookDelivery{}, translateNotFound(err)
	}
	if status != "DEAD" {
		if lastReplayKey == idempotencyKey {
			_ = tx.Rollback(ctx)
			return s.GetWebhookDelivery(ctx, tenantID, id)
		}
		return model.WebhookDelivery{}, ErrInvalidState
	}
	if replayCount != expectedReplayCount {
		return model.WebhookDelivery{}, ErrInvalidState
	}
	if _, err = tx.Exec(ctx, `UPDATE outbox_events SET status='PENDING',attempt_count=0,available_at=now(),locked_at=NULL,locked_by=NULL,last_http_status=NULL,last_error='',delivered_at=NULL,replay_count=replay_count+1,last_replayed_at=now(),last_replayed_by=$3,last_replay_key=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, actor, idempotencyKey); err != nil {
		return model.WebhookDelivery{}, err
	}
	if err = audit(ctx, tx, tenantID, actor, "webhook.delivery.replay", "outbox_event", id, requestID, map[string]any{"previous_error": truncate(lastError, 1000), "previous_replay_count": replayCount}); err != nil {
		return model.WebhookDelivery{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.WebhookDelivery{}, err
	}
	return s.GetWebhookDelivery(ctx, tenantID, id)
}

func (s *Postgres) ProcessWebhook(ctx context.Context, in WebhookInput) (bool, error) {
	if in.Source == "" {
		in.Source = "provider"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var inserted string
	err = tx.QueryRow(ctx, `INSERT INTO webhook_inbox(id,source,external_event_id,event_type,canonical_status,payload_sha256,payload_ciphertext,status) VALUES($1,$2,$3,$4,$5,$6,$7,'RECEIVED') ON CONFLICT(source,external_event_id) DO NOTHING RETURNING id`, in.ID, in.Source, in.ExternalEventID, in.EventType, in.Status, in.PayloadHash, in.PayloadCiphertext).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	processed := false
	matchedTenant, aggregateType, aggregateID := "", "", ""
	// Refund provider events commonly contain both the original payment ID and
	// the refund ID. The refund resource must win, otherwise a refund callback
	// would be consumed as an ordinary payment update and never reach the
	// canonical refund or Emisell Backend.
	if in.EnginePaymentID != "" && in.EngineRefundID == "" {
		var tenantID, localID string
		var amount int64
		var currency, reference, environment string
		var previousStatus, appliedStatus string
		var existingFlags []string
		err = tx.QueryRow(ctx, `SELECT tenant_id,id,amount,currency,merchant_reference,environment,status,flags FROM payment_sessions WHERE engine_payment_id=$1 FOR UPDATE`, in.EnginePaymentID).Scan(&tenantID, &localID, &amount, &currency, &reference, &environment, &previousStatus, &existingFlags)
		if err == nil {
			matchedTenant, aggregateType, aggregateID = tenantID, "payment", localID
			appliedStatus = canonicalPaymentStatus(previousStatus, in.Status)
			flags, flagsAdded := canonicalPaymentFlags(existingFlags, previousStatus, appliedStatus)
			flagsJSON, marshalErr := json.Marshal(flags)
			if marshalErr != nil {
				return false, marshalErr
			}
			eventCreatedAt := time.Now().UTC()
			if _, err = tx.Exec(ctx, `UPDATE payment_sessions SET status=$2,flags=$3::jsonb,updated_at=$4 WHERE engine_payment_id=$1`, in.EnginePaymentID, appliedStatus, string(flagsJSON), eventCreatedAt); err != nil {
				return false, err
			}
			if previousStatus != appliedStatus {
				if err = insertPaymentHistory(ctx, tx, tenantID, localID, appliedStatus, "webhook."+in.Source, map[string]any{"event_id": in.ExternalEventID, "event_type": in.EventType, "flags_added": flagsAdded}); err != nil {
					return false, err
				}
			}
			data := map[string]any{
				"payment": map[string]any{
					"id": localID, "merchant_reference": reference, "amount": amount,
					"currency": currency, "environment": environment, "status": appliedStatus,
					"flags": flags, "updated_at": eventCreatedAt,
				},
				"previous_status": previousStatus,
			}
			if err = insertOutbox(ctx, tx, tenantID, "payment.updated", "payment", localID, in.Source+":"+in.ExternalEventID, eventCreatedAt, data); err != nil {
				return false, err
			}
			processed = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
	}
	if !processed && in.EngineRefundID != "" {
		var tenantID, localID, paymentID string
		var amount int64
		var currency, paymentMethodCode, reason, requestedBy string
		var appliedStatus string
		eventCreatedAt := time.Now().UTC()
		err = tx.QueryRow(ctx, `UPDATE refunds SET status=CASE WHEN refunds.status='SUCCEEDED' AND $2<>'SUCCEEDED' THEN refunds.status WHEN refunds.status='FAILED' AND $2<>'SUCCEEDED' THEN refunds.status ELSE $2 END,updated_at=$3 WHERE engine_refund_id=$1 RETURNING tenant_id,id,payment_id,amount,currency,COALESCE(payment_method_code,''),reason,requested_by,status`, in.EngineRefundID, in.Status, eventCreatedAt).Scan(&tenantID, &localID, &paymentID, &amount, &currency, &paymentMethodCode, &reason, &requestedBy, &appliedStatus)
		if err == nil {
			matchedTenant, aggregateType, aggregateID = tenantID, "refund", localID
			data := map[string]any{"refund": map[string]any{
				"id": localID, "payment_id": paymentID, "amount": amount,
				"currency": currency, "payment_method_code": paymentMethodCode,
				"reason": reason, "requested_by": requestedBy,
				"status": appliedStatus, "updated_at": eventCreatedAt,
			}}
			if err = insertOutbox(ctx, tx, tenantID, "refund.updated", "refund", localID, in.Source+":"+in.ExternalEventID, eventCreatedAt, data); err != nil {
				return false, err
			}
			processed = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
	}
	status := "IGNORED"
	if processed {
		status = "PROCESSED"
	}
	if _, err = tx.Exec(ctx, `UPDATE webhook_inbox SET status=$2,tenant_id=$3,aggregate_type=$4,aggregate_id=$5,processed_at=now() WHERE id=$1`, in.ID, status, matchedTenant, aggregateType, aggregateID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Postgres) HasProcessedPaymentWebhook(ctx context.Context, tenantID, source, paymentID, canonicalStatus string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM webhook_inbox
			WHERE tenant_id=$1 AND source=$2 AND aggregate_type='payment' AND aggregate_id=$3
			  AND status='PROCESSED' AND canonical_status=$4
		)
	`, tenantID, source, paymentID, canonicalStatus).Scan(&found)
	return found, err
}

func (s *Postgres) HasDeliveredPaymentOutbox(ctx context.Context, tenantID, paymentID, canonicalStatus string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM outbox_events
			WHERE tenant_id=$1
			  AND aggregate_type='payment'
			  AND aggregate_id=$2
			  AND event_type='payment.updated'
			  AND status='DELIVERED'
			  AND payload #>> '{data,payment,status}'=$3
		)
	`, tenantID, paymentID, canonicalStatus).Scan(&found)
	return found, err
}

func (s *Postgres) ClaimOutbox(ctx context.Context, worker string) (model.OutboxEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.OutboxEvent{}, err
	}
	defer tx.Rollback(ctx)
	var e model.OutboxEvent
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,event_type,payload,attempt_count,max_attempts,created_at FROM outbox_events WHERE (status='PENDING' AND available_at<=now()) OR (status='PROCESSING' AND locked_at<now()-interval '5 minutes') ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&e.ID, &e.TenantID, &e.EventType, &e.Payload, &e.AttemptCount, &e.MaxAttempts, &e.CreatedAt)
	if err != nil {
		return model.OutboxEvent{}, translateNotFound(err)
	}
	_, err = tx.Exec(ctx, `UPDATE outbox_events SET status='PROCESSING',locked_at=now(),locked_by=$2,attempt_count=attempt_count+1,updated_at=now() WHERE id=$1`, e.ID, worker)
	if err != nil {
		return model.OutboxEvent{}, err
	}
	e.AttemptCount++
	if err = tx.Commit(ctx); err != nil {
		return model.OutboxEvent{}, err
	}
	return e, nil
}
func (s *Postgres) CompleteOutbox(ctx context.Context, id string, httpStatus int) error {
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET status='DELIVERED',delivered_at=now(),last_http_status=$2,last_error='',locked_at=NULL,locked_by=NULL,updated_at=now() WHERE id=$1`, id, httpStatus)
	return err
}
func (s *Postgres) RetryOutbox(ctx context.Context, id string, httpStatus int, message string, availableAt time.Time, dead bool) error {
	status := "PENDING"
	if dead {
		status = "DEAD"
	}
	_, err := s.pool.Exec(ctx, `UPDATE outbox_events SET status=$2,available_at=$3,last_http_status=$4,last_error=$5,locked_at=NULL,locked_by=NULL,updated_at=now() WHERE id=$1`, id, status, availableAt, httpStatus, truncate(message, 1000))
	return err
}

func (s *Postgres) Audit(ctx context.Context, tenant, actor, action, resourceType, resourceID, requestID string, details any) error {
	return audit(ctx, s.pool, tenant, actor, action, resourceType, resourceID, requestID, details)
}

func (s *Postgres) ReceiveEmisellEvent(ctx context.Context, event emisellwebhook.ReceivedEvent) (bool, error) {
	var inserted bool
	err := s.pool.QueryRow(ctx, `
		INSERT INTO emisell_received_events(id,tenant_id,event_type,payload,payload_sha256,source_timestamp)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(id) DO UPDATE SET
			duplicate_count=emisell_received_events.duplicate_count+1,
			last_received_at=now()
		WHERE emisell_received_events.tenant_id=EXCLUDED.tenant_id
		  AND emisell_received_events.event_type=EXCLUDED.event_type
		  AND emisell_received_events.payload_sha256=EXCLUDED.payload_sha256
		RETURNING (xmax=0)
	`, event.ID, event.TenantID, event.EventType, event.Payload, event.PayloadSHA256, event.SourceTimestamp).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, emisellwebhook.ErrEventConflict
	}
	return inserted, err
}

const installationSelect = `SELECT i.id,i.tenant_id,i.provider_code,p.name,i.environment,i.engine_profile_id,COALESCE(i.engine_connector_id,''),i.execution_engine,i.provider_version,i.status,i.credential_metadata,i.payment_methods,i.last_error,i.version,i.created_at,i.updated_at,i.uninstalled_at FROM provider_installations i JOIN providers p ON p.code=i.provider_code`

const paymentMethodAssignmentSelect = `SELECT a.id,a.tenant_id,a.environment,a.payment_method_code,a.payment_method,a.payment_method_type,a.installation_id,p.code,p.name,a.label,a.status,a.version,a.created_at,a.updated_at FROM payment_method_assignments a JOIN provider_installations i ON i.tenant_id=a.tenant_id AND i.id=a.installation_id JOIN providers p ON p.code=i.provider_code`

const webhookDeliverySelect = `SELECT id,event_type,aggregate_type,aggregate_id,payload,status,attempt_count,max_attempts,available_at,last_http_status,last_error,delivered_at,replay_count,last_replayed_at,last_replayed_by,created_at,updated_at FROM outbox_events`

const connectorCertificationRunSelect = `SELECT r.id,r.installation_id,r.provider_code,p.name,r.payment_method_code,m.name,r.environment,r.status,r.checks,COALESCE(r.payment_id,''),r.message,r.initiated_by,r.started_at,r.completed_at FROM connector_certification_runs r JOIN providers p ON p.code=r.provider_code JOIN payment_methods m ON m.code=r.payment_method_code`

type scanner interface{ Scan(...any) error }

func scanInstallation(row scanner) (model.Installation, error) {
	var i model.Installation
	err := row.Scan(&i.ID, &i.TenantID, &i.ProviderCode, &i.ProviderName, &i.Environment, &i.EngineProfileID, &i.EngineConnectorID, &i.ExecutionEngine, &i.ProviderVersion, &i.Status, &i.CredentialMetadata, &i.PaymentMethods, &i.LastError, &i.Version, &i.CreatedAt, &i.UpdatedAt, &i.UninstalledAt)
	return i, err
}

func scanPaymentMethodAssignment(row scanner) (model.PaymentMethodAssignment, error) {
	var item model.PaymentMethodAssignment
	err := row.Scan(&item.ID, &item.TenantID, &item.Environment, &item.PaymentMethodCode, &item.PaymentMethod, &item.PaymentMethodType, &item.InstallationID, &item.ProviderCode, &item.ProviderName, &item.Label, &item.Status, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanWebhookDelivery(row scanner) (model.WebhookDelivery, error) {
	var item model.WebhookDelivery
	err := row.Scan(&item.ID, &item.EventType, &item.AggregateType, &item.AggregateID, &item.Payload, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.AvailableAt, &item.LastHTTPStatus, &item.LastError, &item.DeliveredAt, &item.ReplayCount, &item.LastReplayedAt, &item.LastReplayedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanConnectorCertificationRun(row scanner) (model.ConnectorCertificationRun, error) {
	var item model.ConnectorCertificationRun
	err := row.Scan(&item.ID, &item.InstallationID, &item.ProviderCode, &item.ProviderName, &item.PaymentMethodCode, &item.PaymentMethodName, &item.Environment, &item.Status, &item.Checks, &item.PaymentID, &item.Message, &item.InitiatedBy, &item.StartedAt, &item.CompletedAt)
	return item, err
}

const paymentSelect = `SELECT id,tenant_id,installation_id,COALESCE(payment_option_id,''),COALESCE(payment_method_code,''),provider_code,provider_version,environment,merchant_reference,idempotency_key,request_hash,amount,currency,status,flags,COALESCE(engine_payment_id,''),connector_transaction_id,execution_engine,COALESCE(next_action,'null'::jsonb),last_error,reconciliation_count,last_reconciled_at,last_reconciled_by,last_reconciliation_key,created_at,updated_at FROM payment_sessions`
const paymentListSelect = `SELECT id,tenant_id,installation_id,COALESCE(payment_option_id,''),COALESCE(payment_method_code,''),provider_code,provider_version,environment,merchant_reference,idempotency_key,request_hash,amount,currency,status,flags,COALESCE(engine_payment_id,''),connector_transaction_id,execution_engine,NULL::jsonb,last_error,reconciliation_count,last_reconciled_at,last_reconciled_by,last_reconciliation_key,created_at,updated_at FROM payment_sessions`

func scanPayment(row scanner) (model.PaymentSession, []byte, error) {
	var i model.PaymentSession
	var hash []byte
	err := row.Scan(&i.ID, &i.TenantID, &i.InstallationID, &i.PaymentOptionID, &i.PaymentMethodCode, &i.ProviderCode, &i.ProviderVersion, &i.Environment, &i.MerchantReference, &i.IdempotencyKey, &hash, &i.Amount, &i.Currency, &i.Status, &i.Flags, &i.EnginePaymentID, &i.ConnectorTxnID, &i.ExecutionEngine, &i.NextAction, &i.LastError, &i.ReconciliationCount, &i.LastReconciledAt, &i.LastReconciledBy, &i.LastReconciliationKey, &i.CreatedAt, &i.UpdatedAt)
	return i, hash, err
}

const refundSelect = `SELECT id,tenant_id,payment_id,COALESCE(payment_method_code,''),idempotency_key,request_hash,amount,currency,reason,requested_by,status,COALESCE(engine_refund_id,''),execution_engine,last_error,created_at,updated_at FROM refunds`

func scanRefund(row scanner) (model.Refund, []byte, error) {
	var i model.Refund
	var key string
	var hash []byte
	err := row.Scan(&i.ID, &i.TenantID, &i.PaymentID, &i.PaymentMethodCode, &key, &hash, &i.Amount, &i.Currency, &i.Reason, &i.RequestedBy, &i.Status, &i.EngineRefundID, &i.ExecutionEngine, &i.LastError, &i.CreatedAt, &i.UpdatedAt)
	return i, hash, err
}

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func audit(ctx context.Context, db execer, tenant, actor, action, resourceType, resourceID, requestID string, details any) error {
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO audit_logs(tenant_id,actor,action,resource_type,resource_id,request_id,details) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, actor, action, resourceType, resourceID, requestID, payload)
	return err
}

func insertPaymentHistory(ctx context.Context, db execer, tenantID, paymentID, status, source string, details any) error {
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO payment_status_history(tenant_id,payment_id,status,source,details) VALUES($1,$2,$3,$4,$5)`, tenantID, paymentID, status, source, payload)
	return err
}

func canonicalPaymentStatus(current, incoming string) string {
	if current == model.PaymentSucceeded && incoming != model.PaymentSucceeded {
		return current
	}
	if (current == model.PaymentFailed || current == model.PaymentCancelled || current == model.PaymentExpired) && incoming != model.PaymentSucceeded {
		return current
	}
	return incoming
}

func canonicalPaymentFlags(existing []string, previous, applied string) ([]string, []string) {
	flags := make([]string, len(existing))
	copy(flags, existing)
	added := make([]string, 0, 1)
	add := func(value string) {
		if contains(flags, value) {
			return
		}
		flags = append(flags, value)
		added = append(added, value)
	}
	if applied == model.PaymentSucceeded {
		switch previous {
		case model.PaymentExpired:
			add("late_payment")
		case model.PaymentUnknown:
			add("provider_delayed_confirmation")
		}
	}
	return flags, added
}
func insertOutbox(ctx context.Context, tx pgx.Tx, tenant, eventType, aggregateType, aggregateID, dedup string, createdAt time.Time, data any) error {
	id, err := ids.New("evt")
	if err != nil {
		return err
	}
	payload, err := emisellwebhook.MarshalEnvelope(id, eventType, tenant, aggregateType, aggregateID, createdAt, data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_events(id,tenant_id,event_type,aggregate_type,aggregate_id,deduplication_key,payload) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(deduplication_key) DO NOTHING`, id, tenant, eventType, aggregateType, aggregateID, dedup, payload)
	return err
}
func translateNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func translateConstraint(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503" || pgErr.Code == "23514") {
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	}
	return err
}
func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
