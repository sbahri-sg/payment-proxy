# Security

- The ZIP contains no Client ID, Secret Key, merchant data, or executable.
- Credentials are supplied per installation and remain encrypted in the Payment Kernel.
- Requests use DOKU Non-SNAP HMAC-SHA256 with an exact body digest.
- Notifications are accepted only after Client ID, request target, and HMAC verification.
- Provider egress is restricted to `api-sandbox.doku.com` and `api.doku.com`.
- The runtime refuses redirects from the provider API and limits request/response sizes.
