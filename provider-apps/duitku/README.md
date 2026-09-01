# Duitku Provider App for Emisell

This directory is the review submission for the normalized Duitku connector.
It uses Duitku POP Create Invoice for both direct-channel payments and
provider-hosted checkout. In hosted mode `paymentMethod` is intentionally empty,
so the merchant's active methods are selected on the official Duitku page.

Merchant authentication requires `merchant_code` and `api_key`. Current Duitku
authentication and callback verification use HMAC-SHA256. Emisell never asks
for, stores, or renders Duitku dashboard credentials outside the merchant's
encrypted installation scope.

The ZIP is a source-only review package. The trusted runtime is built separately
as an OCI image and shared by all merchant installations pinned to
`emisell-duitku-v1.0.0`; an upload never creates one container per merchant.

Lifecycle: upload, validate the static contract, verify it against the exact
shared runtime version and executable digest, then publish. Publishing a release
does not automatically upgrade existing merchant installations.
