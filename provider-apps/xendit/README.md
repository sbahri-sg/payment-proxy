# Xendit Provider App for Emisell

This directory is the review submission for the normalized Xendit payment
connector. It describes provider identity, credentials, payment methods,
operations, and the Partner Connector API contract.

The connector contains a fail-closed implementation of asynchronous Unified
Refund for payment methods whose catalog capability declares a return-to-source
policy. The release does not advertise `create_refund` yet: it must first pass
real Xendit sandbox conformance and verified `refund.succeeded`/
`refund.failed` webhook evidence. The connector does not claim an undocumented
provider-side refund lookup operation.

The ZIP built from this directory is not a runtime bundle. It contains no
native executable and is never started by the Payment Kernel. The Xendit
connector is built as a separate OCI image, scanned, deployed once per provider
version, and reached through the Runtime Dispatcher. Merchant installations
only select a released provider version and supply encrypted credentials.

Lifecycle: upload and validate the submission, certify it, deploy the matching
runtime version, then publish the release. Publishing a new version does not
silently upgrade existing merchant installations.
