# Contract tests

The canonical operations in `openapi.yaml` are exercised by the connector
runner tests in `backend/internal/connectorrunner`. Provider-specific payment
and webhook behavior is exercised by `backend/internal/xendit` tests.

Refund coverage verifies the original `payment_request_id`, provider
idempotency key, canonical reason, pending acknowledgement, webhook signature,
and final refund status normalization.

This directory is intentionally source-only. Runtime binaries and credentials
must not be copied into the submission ZIP.
