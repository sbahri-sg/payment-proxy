# Security

- Never include Server Keys, Client Keys, PoP IDs, merchant credentials, private
  keys, webhook payload samples containing secrets, or environment files.
- Merchant credentials are stored by the Payment Kernel vault and provided only
  to the selected runtime while processing a request.
- The uploaded ZIP is quarantined and reviewed; it is not compiled or executed.
- The runtime image is built from the trusted repository, scanned, signed, and
  verified using its immutable executable SHA-256 before release publication.
- Outbound network access is restricted to the declared Midtrans API hosts.
- Webhook notification signatures are verified inside the connector using the
  installation Server Key before a normalized event is accepted.
