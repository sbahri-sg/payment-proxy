# DOKU Provider App

Source-only release submission for the shared `emisell-doku-v2.0.1` runtime.
The runtime calls DOKU Checkout, returns DOKU's official hosted payment URL,
checks order status, and verifies Non-SNAP notification signatures.

Merchant credentials are never included in this package. Each installation
supplies its own Client ID and Secret Key for either sandbox or live mode.

For hosted checkout, Emisell omits `payment.payment_method_types` so DOKU shows
all payment methods active for that merchant. For a selected payment method,
the runtime sends exactly one DOKU channel code.

The notification URL must be public HTTPS and configured for the relevant
channel in DOKU Back Office. Extra-data methods such as paylater, BRI Direct
Debit, Jenius, and KKI remain catalog-only until the normalized contract can
provide their required customer, address, and item fields.
