CREATE TABLE provider_app_versions (
    id text PRIMARY KEY CHECK (id ~ '^papp_[A-Za-z0-9]+$'),
    provider_code text NOT NULL CHECK (provider_code ~ '^[a-z0-9_-]{2,48}$'),
    provider_name text NOT NULL CHECK (length(provider_name) BETWEEN 2 AND 120),
    version text NOT NULL CHECK (version ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
    status text NOT NULL CHECK (status IN (
        'UPLOADED','VALIDATED','CERTIFIED','PUBLISHED','DEPRECATED','DISABLED'
    )),
    runtime text NOT NULL CHECK (runtime IN ('isolated_container','remote_http')),
    sdk_version text NOT NULL,
    file_name text NOT NULL CHECK (length(file_name) BETWEEN 5 AND 255),
    content_type text NOT NULL DEFAULT 'application/zip',
    artifact_size bigint NOT NULL CHECK (artifact_size BETWEEN 1 AND 26214400),
    artifact_sha256 char(64) NOT NULL,
    artifact bytea NOT NULL,
    manifest jsonb NOT NULL,
    scan_report jsonb NOT NULL,
    review_note text NOT NULL DEFAULT '' CHECK (length(review_note) <= 2000),
    submitted_by text NOT NULL,
    reviewed_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    UNIQUE (provider_code, version)
);

CREATE INDEX provider_app_versions_status_created_idx
    ON provider_app_versions (status, created_at DESC);

CREATE INDEX provider_app_versions_provider_created_idx
    ON provider_app_versions (provider_code, created_at DESC);

CREATE UNIQUE INDEX provider_app_versions_one_published_idx
    ON provider_app_versions (provider_code)
    WHERE status = 'PUBLISHED';
