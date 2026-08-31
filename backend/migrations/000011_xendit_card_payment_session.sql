UPDATE provider_payment_method_capabilities
SET source_url='https://docs.xendit.co/docs/how-payment-sessions-work',
    metadata=metadata || jsonb_build_object(
        'engine','emisell_native',
        'provider_api','payment_sessions',
        'conformance_profile','xendit-payment-session/card',
        'card_data_scope','xendit_hosted_only'
    ),
    updated_at=now()
WHERE provider_code='xendit' AND payment_method_code='card';
