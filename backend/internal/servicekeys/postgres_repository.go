package servicekeys

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) List(ctx context.Context) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,name,key_prefix,key_last_four,scopes,status,created_by,created_at,
		       COALESCE(revoked_by,''),revoked_at
		FROM service_api_keys
		ORDER BY CASE status WHEN 'ACTIVE' THEN 0 ELSE 1 END,created_at DESC,id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list service API keys: %w", err)
	}
	defer rows.Close()
	items := make([]APIKey, 0)
	for rows.Next() {
		item, scanErr := scanAPIKey(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list service API keys: %w", err)
	}
	return items, nil
}

func (r *PostgresRepository) Create(ctx context.Context, input CreateInput) (APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var item APIKey
	var prefix, lastFour string
	err = tx.QueryRow(ctx, `
		INSERT INTO service_api_keys(id,name,key_prefix,key_last_four,key_hash,scopes,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,name,key_prefix,key_last_four,scopes,status,created_by,created_at,
		          COALESCE(revoked_by,''),revoked_at
	`, input.ID, input.Name, input.KeyPrefix, input.KeyLast4, input.KeyHash, input.Scopes, input.Actor).Scan(
		&item.ID, &item.Name, &prefix, &lastFour, &item.Scopes, &item.Status,
		&item.CreatedBy, &item.CreatedAt, &item.RevokedBy, &item.RevokedAt,
	)
	if err != nil {
		return APIKey{}, fmt.Errorf("create service API key: %w", err)
	}
	item.KeyHint = keyHint(prefix, lastFour)
	if _, err = tx.Exec(ctx, `
		INSERT INTO audit_logs(tenant_id,actor,action,resource_type,resource_id,request_id,details)
		VALUES('platform',$1,'service_api_key.create','service_api_key',$2,$3,jsonb_build_object('name',$4::text,'scopes',$5::text[]))
	`, input.Actor, input.ID, input.RequestID, input.Name, input.Scopes); err != nil {
		return APIKey{}, fmt.Errorf("audit service API key create: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return APIKey{}, err
	}
	return item, nil
}

func (r *PostgresRepository) Revoke(ctx context.Context, id, actor, requestID string) (APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var item APIKey
	var prefix, lastFour string
	err = tx.QueryRow(ctx, `
		UPDATE service_api_keys
		SET status='REVOKED',revoked_by=$2,revoked_at=now()
		WHERE id=$1 AND status='ACTIVE'
		RETURNING id,name,key_prefix,key_last_four,scopes,status,created_by,created_at,
		          COALESCE(revoked_by,''),revoked_at
	`, id, actor).Scan(
		&item.ID, &item.Name, &prefix, &lastFour, &item.Scopes, &item.Status,
		&item.CreatedBy, &item.CreatedAt, &item.RevokedBy, &item.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("revoke service API key: %w", err)
	}
	item.KeyHint = keyHint(prefix, lastFour)
	if _, err = tx.Exec(ctx, `
		INSERT INTO audit_logs(tenant_id,actor,action,resource_type,resource_id,request_id,details)
		VALUES('platform',$1,'service_api_key.revoke','service_api_key',$2,$3,jsonb_build_object('name',$4::text))
	`, actor, id, requestID, item.Name); err != nil {
		return APIKey{}, fmt.Errorf("audit service API key revoke: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return APIKey{}, err
	}
	return item, nil
}

func (r *PostgresRepository) Authenticate(ctx context.Context, hash []byte) (bool, error) {
	var valid bool
	if err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM service_api_keys
			WHERE key_hash=$1 AND status='ACTIVE' AND scopes @> ARRAY['gateway:full']::text[]
		)
	`, hash).Scan(&valid); err != nil {
		return false, fmt.Errorf("authenticate service API key: %w", err)
	}
	return valid, nil
}

type rowScanner interface{ Scan(...any) error }

func scanAPIKey(row rowScanner) (APIKey, error) {
	var item APIKey
	var prefix, lastFour string
	if err := row.Scan(
		&item.ID, &item.Name, &prefix, &lastFour, &item.Scopes, &item.Status,
		&item.CreatedBy, &item.CreatedAt, &item.RevokedBy, &item.RevokedAt,
	); err != nil {
		return APIKey{}, fmt.Errorf("scan service API key: %w", err)
	}
	item.KeyHint = keyHint(prefix, lastFour)
	return item, nil
}
