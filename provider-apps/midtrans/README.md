# Midtrans Provider App for Emisell

This directory is the review submission for the normalized Midtrans connector.
It declares provider identity, merchant credential fields, supported payment
methods, canonical operations, and the Partner Connector API contract.

Merchant authentication uses the Midtrans Server Key. The key prefix determines
whether the installation is sandbox or live. The optional PoP ID is sent only
when the merchant's Core API account requires it. Client Key and Merchant ID are
not requested because both Core API and Snap transaction creation use the
Server Key. The provider-hosted flow returns the Midtrans Snap `redirect_url`;
Emisell sends the installation's eligible `ACTIVE` assignments as the exact
Snap `enabled_payments` list and does not embed or reproduce the checkout UI.

The ZIP built from this directory is a source-only review package. It contains a
reviewable Go implementation snapshot under `src/midtrans`, but no executable.
The trusted Midtrans runtime is built separately as an OCI image and shared by
all merchant installations using the same provider version. Credentials remain
encrypted and scoped to each merchant installation and environment.

Lifecycle: upload, validate the static contract, verify it against the exact
shared runtime version and executable digest, then publish. Publishing a new
release does not automatically upgrade existing merchant installations.
