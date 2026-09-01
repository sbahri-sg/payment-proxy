# iPaymu Provider App for Emisell

This directory is the source-only review submission for the normalized iPaymu
API v2 connector. Provider-hosted sessions call iPaymu Redirect Payment and
return the official `Data.Url`; Emisell does not render another payment page.
Direct mode maps a selected canonical payment method to iPaymu's documented
`paymentMethod` and `paymentChannel` values.

Each merchant environment uses its own `va` and `api_key` from the matching
iPaymu dashboard. Sandbox and live credentials are intentionally not shared.
Request signing uses the API key, while callback `X-Signature` validation uses
the VA number exactly as required by iPaymu.

The trusted runtime is built separately as one shared OCI image for every
installation pinned to `emisell-ipaymu-v2.0.1`. Uploading this ZIP never starts
one container per merchant.
