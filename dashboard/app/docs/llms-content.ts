export const llmsContent = `# Emisell Payment Proxy — Backend Integration Contract

Contract version: 2026-08-28
Audience: Emisell Backend service developers and coding assistants.
Canonical documentation: /docs?contract=backend

## Purpose

Payment Proxy exposes one provider-neutral API. Emisell Backend must not call Xendit, Midtrans, DOKU, Duitku, or connector runtime endpoints directly. Provider credentials are accepted only by the installation credential endpoint and are never returned.

## Required headers

Authorization: Bearer <SERVICE_API_KEY>
X-Emisell-Merchant-ID: <merchant_id>
X-Emisell-Execution-Mode: sandbox | live
Content-Type: application/json

Every mutation that represents a business operation must use a stable Idempotency-Key. Retry the same operation with the same key and identical body. Never generate a new key merely because a request timed out.

## Merchant provider lifecycle

1. GET /api/v1/providers
2. POST /api/v1/provider-installations with provider_code and environment.
3. PUT /api/v1/provider-installations/{id}/credentials. This both stores encrypted credentials and verifies them with the provider.
4. POST /api/v1/provider-installations/{id}/activate.
5. PUT /api/v1/payment-method-assignments.
6. GET /api/v1/payment-options and give checkout only the opaque payment_option_id.

Sandbox and Live are separate installation slots with separate merchant credentials. There is no switch-environment mutation and no merchant-owned runtime/container.

## Payment flow

POST /api/v1/payment-sessions
Headers: X-Emisell-Execution-Mode and Idempotency-Key are required.
Preferred request fields: payment_option_id, merchant_reference, amount in minor units, currency, customer, return_url, metadata.

GET /api/v1/payment-sessions/{id}
Returns the canonical payment projection and may synchronize with the same pinned provider installation.

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
READY requires platform evidence for: active provider connection, active payment method, payment creation, idempotency replay, payment status lookup, configured signed backend webhook, and successful webhook delivery. Sandbox and Live are evaluated separately.

## Refunds

POST /api/v1/refunds is conditional. Only expose refund when the original payment channel advertises return-to-original-source policy and the exact connector release supports create_refund. Never infer global refund support from the provider name.

## Safety rules for AI and automation

- Never put service API keys, provider credentials, webhook secrets, card data, OTP, or raw customer secrets in prompts, logs, metadata, generated code, or tool output.
- Do not let an AI tool perform Live mutations without explicit operator approval and narrowly scoped credentials.
- Do not invent provider-specific endpoints or fields. Discover credential_fields from GET /api/v1/providers and payment options from GET /api/v1/payment-options.
- Do not retry UNKNOWN outcomes against another provider.
- Do not mark an order paid from a browser redirect alone; trust the canonical API/webhook status.
- Admin Control Plane and /partner/v1 connector runtime endpoints are outside the Emisell Backend contract.
`;
