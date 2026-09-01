# DOKU Provider App

Source-only release submission for the shared `emisell-doku-v2.0.2` runtime.
The runtime calls DOKU Checkout, returns DOKU's official hosted payment URL,
checks order status, and verifies Non-SNAP notification signatures.

Merchant credentials are never included in this package. Each installation
supplies its own Client ID and Secret Key for either sandbox or live mode.

For hosted checkout, Emisell fills `payment.payment_method_types` with the exact
eligible `ACTIVE` assignments for that installation. For direct checkout, the
runtime sends exactly one DOKU channel code.

The notification URL must be public HTTPS and configured for the relevant
channel in DOKU Back Office. Extra-data methods such as paylater, BRI Direct
Debit, Jenius, and KKI remain catalog-only until the normalized contract can
provide their required customer, address, and item fields.
