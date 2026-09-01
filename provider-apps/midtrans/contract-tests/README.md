# Contract tests

The canonical operations declared in `openapi.yaml` are exercised by the shared
connector runner tests in `backend/internal/connectorrunner`. Provider-specific
payment mapping, credential detection, API authentication, payment status, and
webhook signature behavior are exercised by `backend/internal/midtrans` tests.

This review package is intentionally source-only. Runtime binaries and merchant
credentials must never be copied into the ZIP.

During release verification, Payment Proxy compares this submission with the
loaded shared Midtrans runtime: provider and version identity, operations,
credential schema, automated profiles, and payment-method scope must all match.
