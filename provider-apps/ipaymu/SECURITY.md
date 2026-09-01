# Security

- Never include VA numbers, API keys, merchant credentials, production callback
  payloads, private keys, or environment files in the review ZIP.
- Credentials stay encrypted in the Payment Kernel vault and are supplied only
  to the selected shared runtime for the current request.
- API requests use iPaymu HMAC-SHA256 request signatures over the exact JSON
  bytes sent to the provider.
- Callback payloads are type-normalized, key-sorted, slash-escaped, and verified
  with `X-Signature` using the merchant VA as the HMAC secret.
- The uploaded ZIP is quarantined and reviewed; it is not compiled or executed.
- Outbound runtime access is restricted to the two declared iPaymu API hosts.
