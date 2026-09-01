# Security

- Do not place merchant credentials, API keys, webhook tokens, private keys, or
  environment files in this submission.
- Merchant credential values are stored by the Payment Kernel vault and sent
  only to the selected runtime for the duration of a request.
- The submission ZIP is quarantined and statically reviewed; it is never
  executed.
- The runtime OCI image must be scanned, signed, deployed with outbound network
  restrictions, and expose an immutable executable digest before publication.
- Xendit webhooks are verified inside the connector before a normalized event
  is accepted by the Payment Kernel.
