# Xendit Provider App for Emisell

This directory is the review submission for the normalized Xendit payment
connector. It describes provider identity, credentials, payment methods,
operations, and the Partner Connector API contract.

The primary checkout flow creates a Xendit Payment Session in `PAYMENT_LINK`
mode and returns `payment_link_url`. The control plane supplies the installation's
eligible `ACTIVE` mappings and the connector sends their exact channel codes as
`allowed_payment_channels`. Xendit owns the payment-method selection page;
Emisell only redirects the customer and tracks canonical status.

The connector contains a fail-closed implementation of asynchronous Unified
Refund for payment methods whose catalog capability declares a return-to-source
policy. The release does not advertise `create_refund` yet: it must first pass
real Xendit sandbox conformance and verified `refund.succeeded`/
`refund.failed` webhook evidence. The connector does not claim an undocumented
provider-side refund lookup operation.

The ZIP built from this directory is not a runtime bundle. It contains the
reviewable Go implementation snapshot under `src/xendit`, but no native
executable and is never started by the Payment Kernel. The same Xendit source is
built separately as an OCI image, scanned, deployed once per provider version,
and reached through the Runtime Dispatcher. Merchant installations only select
a released provider version and supply encrypted credentials.

Lifecycle: upload the submission, validate its static contract, run backend
verification against the exact shared runtime version and digest, then publish
the release. The internal storage status `CERTIFIED` is displayed as `VERIFIED`
in the dashboard. Publishing a new version does not silently upgrade existing
merchant installations.
