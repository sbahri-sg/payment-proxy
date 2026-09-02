ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_sessions_metadata_object'
          AND conrelid = 'payment_sessions'::regclass
    ) THEN
        ALTER TABLE payment_sessions
            ADD CONSTRAINT payment_sessions_metadata_object
            CHECK (jsonb_typeof(metadata) = 'object');
    END IF;
END $$;

COMMENT ON COLUMN payment_sessions.metadata IS
    'Merchant-supplied payment metadata persisted for canonical API responses and Emisell webhook correlation.';
