# Security

- Never include API keys, merchant credentials, private keys, production
  callback payloads, or environment files in the review ZIP.
- Merchant credentials stay encrypted in the Payment Kernel vault and are
  supplied only to the selected shared runtime during a request.
- POP request, payment-method verification, transaction status, and callbacks
  use the current Duitku HMAC-SHA256 signatures.
- The uploaded ZIP is quarantined and reviewed; it is not compiled or executed.
- The runtime image is built, scanned, signed, and bound to the release through
  its immutable executable SHA-256.
- Outbound runtime access is restricted to the four declared Duitku API hosts.
