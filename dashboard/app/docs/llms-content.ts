export const llmsContent = `# Emisell Payment Proxy — Backend Integration Contract

Contract version: 2026-08-28
Audience: Emisell Backend service developers and coding assistants.
Canonical documentation: /docs?contract=backend

## Purpose

Payment Proxy exposes one provider-neutral API. Emisell Backend must not call Xendit, Midtrans, DOKU, Duitku, iPaymu, or connector runtime endpoints directly. Provider credentials are accepted only by the installation credential endpoint and are never returned.

## Common headers

Authorization: Bearer <SERVICE_API_KEY>
X-Emisell-Merchant-ID: <merchant_id>
Content-Type: application/json

X-Emisell-Execution-Mode: sandbox | live is required only by endpoints that explicitly document it, such as integration-readiness and internal diagnostics. Payment Methods and Payment Sessions do not use this header: mutations derive environment from installation_id or payment_option_id, while list endpoints use the environment query when filtering is needed.

Every mutation that represents a business operation must use a stable Idempotency-Key. Retry the same operation with the same key and identical body. Never generate a new key merely because a request timed out.

## Merchant provider lifecycle

1. GET /api/v1/providers?q=<keyword>. q is optional, case-insensitive, and searches provider code or name. Debounce merchant-dashboard input by 300-500 ms; omit q to list the full catalog.
2. POST /api/v1/provider-installations with provider_code and environment.
3. PUT /api/v1/provider-installations/{id}/credentials. This both stores encrypted credentials and verifies them with the provider.
4. POST /api/v1/provider-installations/{id}/activate.
5. Keep the active installation_id in the merchant configuration used by Emisell Backend.

Sandbox and Live are separate installation slots with separate merchant credentials. There is no switch-environment mutation and no merchant-owned runtime/container.

## Payment flow

POST /api/v1/payment-sessions
Header: Idempotency-Key is required. Do not send X-Emisell-Execution-Mode; Payment Proxy derives environment from installation_id or payment_option_id.
Preferred request fields: installation_id, checkout_mode=provider_hosted, merchant_reference, amount as whole rupiah for IDR, currency, customer, return_url, metadata. IDR 10000 means exactly Rp10.000.

For provider_hosted checkout, do not send payment_option_id or payment_method_code. Payment Proxy creates the provider's official hosted checkout (Xendit Payment Session, Midtrans Snap, Duitku POP, DOKU Checkout, or iPaymu Redirect Payment) and returns payment.checkout_url plus next_action.redirect_url. Redirect the customer to that provider-owned URL. Emisell must not render its own payment-method page or collect PAN, CVV, OTP, VA, QR, or wallet authorization details.

GET /api/v1/payment-methods?q=<keyword> is the merchant discovery catalog. q is optional, case-insensitive, and searches canonical method code, name, category, and description. Debounce merchant-dashboard input by 300-500 ms. payment-options and payment-method-assignments remain available only for the optional direct-channel flow; they are not prerequisites for provider-hosted checkout. GET /api/v1/payment-method-assignments returns both ACTIVE and INACTIVE records. PUT /api/v1/payment-method-assignments accepts {"assignments":[...]} with 1-50 items, applies the batch atomically, derives environment from each installation_id, and must not send X-Emisell-Execution-Mode. Use GET /api/v1/payment-options?environment=sandbox|live for active checkout options only.

GET /api/v1/payment-sessions/{id}
Returns the canonical payment projection and may synchronize with the same pinned provider installation.

GET /api/v1/payment-sessions?environment=sandbox|live
Lists canonical payments. The environment query is optional; without it, both environments are returned.

POST /api/v1/payment-sessions/{id}/cancel
Allowed only for PENDING or PROCESSING payments when the connector supports cancellation. Requires Idempotency-Key.

Canonical statuses: CREATED, PROCESSING, PENDING, SUCCEEDED, FAILED, CANCELLED, EXPIRED, UNKNOWN.
Canonical flags: late_payment, provider_delayed_confirmation.

UNKNOWN is not permission to create a replacement payment or fail over to another provider. Reconcile the same payment with the same pinned provider. A payment may move from EXPIRED to SUCCEEDED; when it does, flags includes late_payment and order handling must follow an explicit late-payment policy.

## Webhooks to Emisell Backend

Payment Proxy sends normalized payment.updated and refund.updated envelopes from a durable outbox. Emisell Backend does not verify provider-native signatures; Payment Proxy and the connector do that at ingress.

Verify the Payment Proxy HMAC over timestamp + "." + the exact raw body. Reject stale timestamps. Confirm header and body event identifiers/type/merchant match. Persist and deduplicate by event id before returning HTTP 2xx. Delivery is retried and may arrive more than once or out of order.

## Readiness

GET /api/v1/integration-readiness with X-Emisell-Execution-Mode.
READY requires platform evidence for: active provider connection, payment creation, idempotency replay, payment status lookup, configured signed backend webhook, and successful webhook delivery. Sandbox and Live are evaluated separately.

## Refunds

POST /api/v1/refunds is conditional. Only expose refund when the original payment channel advertises return-to-original-source policy and the exact connector release supports create_refund. Never infer global refund support from the provider name.

## Safety rules for AI and automation

- Never put service API keys, provider credentials, webhook secrets, card data, OTP, or raw customer secrets in prompts, logs, metadata, generated code, or tool output.
- Do not let an AI tool perform Live mutations without explicit operator approval and narrowly scoped credentials.
- Do not invent provider-specific endpoints or fields. Discover credential_fields from GET /api/v1/providers. Use GET /api/v1/payment-options only for the optional direct-channel flow, never for provider_hosted checkout.
- Do not retry UNKNOWN outcomes against another provider.
- Do not mark an order paid from a browser redirect alone; trust the canonical API/webhook status.
- Admin Control Plane and /partner/v1 connector runtime endpoints are outside the Emisell Backend contract.
`;
