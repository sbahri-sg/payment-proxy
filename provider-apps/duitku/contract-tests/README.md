# Contract tests

The shared connector runner exercises every canonical operation declared in
`openapi.yaml`. Provider-specific tests in `backend/internal/duitku` cover the
current HMAC-SHA256 authentication, credential verification, hosted and direct
POP checkout, canonical payment-method mapping, transaction status, and
callback signature verification.

The release package is intentionally source-only. During verification, Payment
Proxy compares its provider identity, version, operations, credential schema,
certification profiles, and method scope with the loaded shared Duitku runtime.
