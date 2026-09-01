# Contract tests

Provider-specific tests in `backend/internal/ipaymu` cover request signing,
credential verification through Payment Channels, official redirect checkout,
direct channel mapping, reference-based transaction lookup, and signed JSON or
form callback normalization.

During release verification, Payment Proxy compares the submitted provider
identity, version, operations, credential schema, certification profiles, and
payment-method scope with the already loaded shared iPaymu runtime.
