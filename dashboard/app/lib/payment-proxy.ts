import "server-only";

export type CredentialField = {
  code: string;
  label: string;
  input_type: string;
  secret: boolean;
  required: boolean;
};

export type Provider = {
  code: string;
  name: string;
  description: string;
  available: boolean;
  connector_code: string;
  credential_schema: CredentialField[];
  environments: string[];
  payment_methods: string[];
  created_at: string;
  updated_at: string;
};

export type ProviderAppManifest = {
  contract_version: "v1";
  code: string;
  name: string;
  version: string;
  runtime: "isolated_container" | "remote_http";
  sdk_version: "v1";
  entrypoint: string;
  operations: string[];
  credential_fields: CredentialField[];
  certification_profiles: Record<string, { code: string; automated: boolean; webhook_setup_hint?: string }>;
  environments: ("sandbox" | "live")[];
  payment_methods: string[];
  outbound_hosts: string[];
};

export type ProviderAppScanReport = {
  passed: boolean;
  file_count: number;
  uncompressed_size: number;
  entrypoint_sha256?: string;
  checks: { code: string; status: "PASSED" | "FAILED"; detail: string }[];
  warnings: string[];
};

export type ProviderAppVersion = {
  id: string;
  provider_code: string;
  provider_name: string;
  version: string;
  status: "UPLOADED" | "VALIDATED" | "CERTIFIED" | "PUBLISHED" | "DEPRECATED" | "DISABLED";
  runtime: "isolated_container" | "remote_http";
  sdk_version: string;
  file_name: string;
  content_type: string;
  artifact_size: number;
  artifact_sha256: string;
  manifest: ProviderAppManifest;
  scan_report: ProviderAppScanReport;
  review_note?: string;
  submitted_by: string;
  reviewed_by?: string;
  created_at: string;
  updated_at: string;
  published_at?: string;
};

export type ProviderAppProvider = {
  provider_code: string;
  provider_name: string;
  description: string;
  website_url?: string;
  documentation_url?: string;
  support_email?: string;
  status: "DRAFT" | "ACTIVE" | "DISABLED";
  version_count: number;
  active_version?: string;
  latest_version?: string;
  latest_status?: ProviderAppVersion["status"];
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
};

export type ConfiguredCredential = { code: string; configured: boolean };

export type Installation = {
  id: string;
  merchant_id: string;
  provider_code: string;
  provider_name: string;
  environment: "sandbox" | "live";
  public_webhook_url?: string;
  connector_id?: string;
  execution_engine: "emisell_native" | "legacy_external";
  provider_version: string;
  status: "CONFIG_REQUIRED" | "VERIFYING" | "READY" | "ACTIVE" | "INACTIVE" | "ERROR" | "UNINSTALLED";
  credential_metadata: {
    configured_fields?: ConfiguredCredential[];
    configured_at?: string;
    webhook_ready?: boolean;
    public_webhook_url?: string;
  };
  payment_methods: unknown[];
  last_error?: string;
  version: number;
  created_at: string;
  updated_at: string;
  uninstalled_at?: string;
};

export type PaymentMethodAssignment = {
  id: string;
  environment: "sandbox" | "live";
  payment_method_code: string;
  payment_method: string;
  payment_method_type: string;
  installation_id: string;
  provider_code: string;
  provider_name: string;
  label: string;
  status: "ACTIVE" | "INACTIVE";
  version: number;
  created_at: string;
  updated_at: string;
};

export type PaymentOption = {
  id: string;
  environment: "sandbox" | "live";
  payment_method_code: string;
  category: PaymentMethodCategory;
  label: string;
};

export type PaymentMethodCategory = "QR_CODE" | "CARD" | "VIRTUAL_ACCOUNT" | "E_WALLET" | "RETAIL" | "PAYLATER" | "DIRECT_DEBIT" | "DIGITAL_BANKING";

export type PaymentMethodProviderCapability = {
  provider_code: string;
  provider_name: string;
  provider_available: boolean;
  provider_method: string;
  provider_method_type: string;
  provider_channel_code: string;
  support_status: "DOCUMENTED" | "CERTIFIED" | "DISABLED";
  source_url: string;
  metadata: {
    engine?: string;
    engine_support?: "SUPPORTED" | "UNSUPPORTED";
    blocker_code?: string;
    recommended_path?: string;
    conformance_profile?: string;
    evidence?: string;
    provider_api?: string;
  };
};

export type PaymentMethodCatalogItem = {
  code: string;
  category: PaymentMethodCategory;
  name: string;
  description: string;
  countries: string[];
  currencies: string[];
  active: boolean;
  sort_order: number;
  providers: PaymentMethodProviderCapability[];
};

export type PaymentStatus = "CREATED" | "PROCESSING" | "PENDING" | "SUCCEEDED" | "FAILED" | "CANCELLED" | "EXPIRED" | "UNKNOWN";

export type PaymentNextAction = {
  type?: string;
  qr_code_url?: string | null;
  raw_qr_data?: string | null;
  image_data_url?: string | null;
  display_text?: string | null;
  display_to_timestamp?: string | null;
  redirect_url?: string | null;
  actions?: Record<string, unknown>[];
};

export type PaymentSession = {
  id: string;
  installation_id: string;
  payment_option_id?: string;
  provider_code: string;
  environment: "sandbox" | "live";
  merchant_reference: string;
  amount: number;
  currency: string;
  status: PaymentStatus;
  provider_payment_id?: string;
  connector_transaction_id?: string;
  execution_engine: "emisell_native" | "legacy_external";
  next_action?: PaymentNextAction | null;
  last_error?: string;
  reconciliation_count: number;
  last_reconciled_at?: string;
  last_reconciled_by?: string;
  created_at: string;
  updated_at: string;
};

export type PaymentList = {
  items: PaymentSession[];
  total: number;
  limit: number;
  offset: number;
  has_more: boolean;
};

export type ConnectorCertificationCheck = {
  code: string;
  label: string;
  status: "PASSED" | "FAILED" | "BLOCKED";
  detail?: string;
};

export type ConnectorCertificationRun = {
  id: string;
  installation_id: string;
  provider_code: string;
  provider_name: string;
  payment_method_code: string;
  payment_method_name: string;
  environment: "sandbox" | "live";
  status: "PASSED" | "FAILED" | "BLOCKED";
  checks: ConnectorCertificationCheck[];
  payment_id?: string;
  message?: string;
  initiated_by: string;
  started_at: string;
  completed_at: string;
};

export type PaymentStatusEvent = {
  id: number;
  payment_id: string;
  status: PaymentStatus;
  source: string;
  details: Record<string, unknown>;
  created_at: string;
};

export type WebhookInboxStatus = "RECEIVED" | "PROCESSED" | "IGNORED" | "FAILED";
export type WebhookDeliveryStatus = "PENDING" | "PROCESSING" | "DELIVERED" | "DEAD";

export type WebhookInboxItem = {
  id: string;
  source: string;
  external_event_id: string;
  event_type: string;
  aggregate_type: string;
  aggregate_id: string;
  payload_sha256: string;
  status: WebhookInboxStatus;
  error_message?: string;
  received_at: string;
  processed_at?: string;
};

export type WebhookDelivery = {
  id: string;
  event_type: string;
  aggregate_type: string;
  aggregate_id: string;
  payload: Record<string, unknown>;
  status: WebhookDeliveryStatus;
  attempt_count: number;
  max_attempts: number;
  available_at: string;
  last_http_status?: number;
  last_error?: string;
  delivered_at?: string;
  replay_count: number;
  last_replayed_at?: string;
  last_replayed_by?: string;
  created_at: string;
  updated_at: string;
};

export type WebhookInboxList = { items: WebhookInboxItem[]; counts: Partial<Record<WebhookInboxStatus, number>>; total: number; limit: number; offset: number; has_more: boolean };
export type WebhookDeliveryList = { items: WebhookDelivery[]; counts: Partial<Record<WebhookDeliveryStatus, number>>; total: number; limit: number; offset: number; has_more: boolean };

export type EmisellWebhookSettings = {
  configured: boolean;
  callback_url: string;
  enabled: boolean;
  secret_configured: boolean;
  secret_hint: string;
  source: "database" | "environment";
  last_test_at?: string;
  last_test_success?: boolean;
  last_test_http_status?: number;
  last_test_error?: string;
  updated_by?: string;
  updated_at?: string;
};

export type GeneratedEmisellWebhookSecret = {
  settings: EmisellWebhookSettings;
  secret: string;
};

export type EmisellWebhookTestResult = {
  success: boolean;
  http_status: number;
  event_id: string;
  tested_at: string;
  message: string;
};

export type ServiceAPIKey = {
  id: string;
  name: string;
  key_hint: string;
  scopes: string[];
  status: "ACTIVE" | "REVOKED";
  created_by: string;
  created_at: string;
  revoked_by?: string;
  revoked_at?: string;
};

export type GeneratedServiceAPIKey = {
  api_key: ServiceAPIKey;
  secret: string;
};

export class PaymentProxyError extends Error {
  constructor(public code: string, message: string, public status: number) {
    super(message);
  }
}

function configuration() {
  const baseURL = process.env.BACKEND_API_URL?.trim();
  const serviceKey = process.env.SERVICE_API_KEY?.trim();
  const merchantID = process.env.DASHBOARD_MERCHANT_ID?.trim();
  if (!baseURL || !serviceKey || !merchantID) throw new Error("dashboard payment proxy configuration is incomplete");
  return { baseURL: baseURL.replace(/\/$/, ""), serviceKey, merchantID };
}

async function proxyRequest<T>(path: string, actor: string, init?: RequestInit): Promise<T> {
  const { baseURL, serviceKey, merchantID } = configuration();
  const response = await fetch(`${baseURL}${path}`, {
    ...init,
    cache: "no-store",
    headers: {
      Authorization: `Bearer ${serviceKey}`,
      "X-Emisell-Merchant-ID": merchantID,
      "X-Emisell-Actor": `dashboard:${actor}`,
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
    signal: AbortSignal.timeout(20_000),
  });
  const payload = await response.json().catch(() => null) as { data?: T; error?: { code?: string; message?: string } } | null;
  if (!response.ok || !payload?.data) {
    throw new PaymentProxyError(payload?.error?.code ?? "UPSTREAM_ERROR", payload?.error?.message ?? "Payment Proxy tidak dapat memproses permintaan", response.status);
  }
  return payload.data;
}

async function adminRequest<T>(path: string, actor: string, init?: RequestInit): Promise<T> {
  const baseURL = process.env.BACKEND_API_URL?.trim();
  const adminKey = process.env.ADMIN_API_KEY?.trim();
  if (!baseURL || !adminKey) throw new Error("dashboard admin API configuration is incomplete");
  const response = await fetch(`${baseURL.replace(/\/$/, "")}${path}`, {
    ...init,
    cache: "no-store",
    headers: {
      "X-Admin-API-Key": adminKey,
      "X-Emisell-Actor": `dashboard:${actor}`,
      ...(init?.body && !(init.body instanceof FormData) ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
    signal: AbortSignal.timeout(20_000),
  });
  const payload = await response.json().catch(() => null) as { data?: T; error?: { code?: string; message?: string } } | null;
  if (!response.ok || !payload?.data) {
    throw new PaymentProxyError(payload?.error?.code ?? "UPSTREAM_ERROR", payload?.error?.message ?? "Payment Proxy tidak dapat memproses permintaan admin", response.status);
  }
  return payload.data;
}

export function getEmisellWebhookSettings(actor: string) {
  return adminRequest<EmisellWebhookSettings>("/internal/v1/emisell-webhook", actor);
}

export function updateEmisellWebhookSettings(actor: string, callbackURL: string, enabled: boolean) {
  return adminRequest<EmisellWebhookSettings>("/internal/v1/emisell-webhook", actor, {
    method: "PUT",
    body: JSON.stringify({ callback_url: callbackURL, enabled }),
  });
}

export function generateEmisellWebhookSecret(actor: string) {
  return adminRequest<GeneratedEmisellWebhookSecret>("/internal/v1/emisell-webhook/secret", actor, { method: "POST" });
}

export function testEmisellWebhook(actor: string) {
  return adminRequest<EmisellWebhookTestResult>("/internal/v1/emisell-webhook/test", actor, { method: "POST" });
}

export function listServiceAPIKeys(actor: string) {
  return adminRequest<ServiceAPIKey[]>("/internal/v1/service-api-keys", actor);
}

export function generateServiceAPIKey(actor: string, name: string) {
  return adminRequest<GeneratedServiceAPIKey>("/internal/v1/service-api-keys", actor, {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export function revokeServiceAPIKey(actor: string, id: string) {
  return adminRequest<ServiceAPIKey>(`/internal/v1/service-api-keys/${encodeURIComponent(id)}/revoke`, actor, { method: "POST" });
}

export function listProviderApps(actor: string) {
  return adminRequest<ProviderAppVersion[]>("/internal/v1/provider-apps", actor);
}

export function listProviderAppProviders(actor: string) {
  return adminRequest<ProviderAppProvider[]>("/internal/v1/provider-app-providers", actor);
}

export function getProviderAppProvider(actor: string, providerCode: string) {
  return adminRequest<ProviderAppProvider>(`/internal/v1/provider-app-providers/${encodeURIComponent(providerCode)}`, actor);
}

export function createProviderAppProvider(actor: string, input: {
  provider_code: string;
  provider_name: string;
  description: string;
  website_url: string;
  documentation_url: string;
  support_email: string;
}) {
  return adminRequest<ProviderAppProvider>("/internal/v1/provider-app-providers", actor, {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function listProviderAppVersions(actor: string, providerCode: string) {
  return adminRequest<ProviderAppVersion[]>(`/internal/v1/provider-app-providers/${encodeURIComponent(providerCode)}/versions`, actor);
}

export function uploadProviderApp(actor: string, providerCode: string, bundle: File) {
  const form = new FormData();
  form.set("bundle", bundle, bundle.name);
  return adminRequest<ProviderAppVersion>(`/internal/v1/provider-app-providers/${encodeURIComponent(providerCode)}/versions`, actor, { method: "POST", body: form });
}

export function transitionProviderApp(actor: string, id: string, expectedStatus: ProviderAppVersion["status"], status: ProviderAppVersion["status"], reviewNote: string) {
  return adminRequest<ProviderAppVersion>(`/internal/v1/provider-apps/${encodeURIComponent(id)}/transition`, actor, {
    method: "POST",
    body: JSON.stringify({ expected_status: expectedStatus, status, review_note: reviewNote }),
  });
}

export function listProviders(actor: string) {
  return proxyRequest<Provider[]>("/api/v1/providers", actor);
}

export function listInstallations(actor: string, environment?: string) {
  return proxyRequest<Installation[]>("/api/v1/provider-installations", actor, environment ? { headers: { "X-Emisell-Execution-Mode": environment } } : undefined);
}

export function createInstallation(actor: string, input: { provider_code: string; provider_version: string; environment: string }) {
  return proxyRequest<Installation>("/api/v1/provider-installations", actor, { method: "POST", body: JSON.stringify(input) });
}

export function configureInstallation(actor: string, id: string, credentials: Record<string, string>) {
  return proxyRequest<Installation>(`/api/v1/provider-installations/${encodeURIComponent(id)}/credentials`, actor, {
    method: "PUT",
    body: JSON.stringify({ credentials, payment_methods: [] }),
  });
}

export function transitionInstallation(actor: string, id: string, operation: "activate" | "deactivate", version: number) {
  return proxyRequest<Installation>(`/api/v1/provider-installations/${encodeURIComponent(id)}/${operation}`, actor, {
    method: "POST",
    body: JSON.stringify({ version }),
  });
}

export function uninstallInstallation(actor: string, id: string) {
  return proxyRequest<Installation>(`/api/v1/provider-installations/${encodeURIComponent(id)}`, actor, { method: "DELETE" });
}

export function listPaymentMethodAssignments(actor: string, environment?: string) {
  return proxyRequest<PaymentMethodAssignment[]>("/api/v1/payment-method-assignments", actor, environment ? { headers: { "X-Emisell-Execution-Mode": environment } } : undefined);
}

export function listPaymentMethods(actor: string) {
  return proxyRequest<PaymentMethodCatalogItem[]>("/api/v1/payment-methods", actor);
}

export function listPaymentOptions(actor: string, environment: "sandbox" | "live") {
  return proxyRequest<PaymentOption[]>("/api/v1/payment-options", actor, { headers: { "X-Emisell-Execution-Mode": environment } });
}

export function upsertPaymentMethodAssignment(actor: string, environment: "sandbox" | "live", input: { installation_id: string; payment_method_code: string; label: string; version: number }) {
  return proxyRequest<PaymentMethodAssignment>("/api/v1/payment-method-assignments", actor, {
    method: "PUT",
    headers: { "X-Emisell-Execution-Mode": environment },
    body: JSON.stringify(input),
  });
}

export function deactivatePaymentMethodAssignment(actor: string, id: string, version: number) {
  return proxyRequest<PaymentMethodAssignment>(`/api/v1/payment-method-assignments/${encodeURIComponent(id)}/deactivate`, actor, {
    method: "POST",
    body: JSON.stringify({ version }),
  });
}

export function listConnectorCertifications(actor: string, filters: { environment?: string; provider?: string; limit?: number } = {}) {
  const query = new URLSearchParams();
  if (filters.provider) query.set("provider", filters.provider);
  if (filters.limit) query.set("limit", String(filters.limit));
  return proxyRequest<ConnectorCertificationRun[]>(`/api/v1/connector-certifications${query.size ? `?${query}` : ""}`, actor, filters.environment ? { headers: { "X-Emisell-Execution-Mode": filters.environment } } : undefined);
}

export function runConnectorCertification(actor: string, environment: "sandbox" | "live", input: { installation_id: string; payment_method_code: string; payment_id?: string }) {
  return proxyRequest<ConnectorCertificationRun>("/api/v1/connector-certifications/run", actor, {
    method: "POST",
    headers: { "X-Emisell-Execution-Mode": environment },
    body: JSON.stringify(input),
  });
}

export function listPayments(actor: string, filters: { environment?: string; status?: string; provider?: string; q?: string; limit?: number; offset?: number } = {}) {
  const query = new URLSearchParams();
  if (filters.status) query.set("status", filters.status);
  if (filters.provider) query.set("provider", filters.provider);
  if (filters.q) query.set("q", filters.q);
  if (filters.limit) query.set("limit", String(filters.limit));
  if (filters.offset) query.set("offset", String(filters.offset));
  const suffix = query.size ? `?${query}` : "";
  return proxyRequest<PaymentList>(`/api/v1/payment-sessions${suffix}`, actor, filters.environment ? { headers: { "X-Emisell-Execution-Mode": filters.environment } } : undefined);
}

export function getPayment(actor: string, id: string) {
  return proxyRequest<PaymentSession>(`/api/v1/payment-sessions/${encodeURIComponent(id)}`, actor);
}

export function getPaymentTimeline(actor: string, id: string) {
  return proxyRequest<PaymentStatusEvent[]>(`/api/v1/payment-sessions/${encodeURIComponent(id)}/timeline`, actor);
}

export function cancelPayment(actor: string, id: string, reason: string, idempotencyKey: string) {
  return proxyRequest<PaymentSession>(`/api/v1/payment-sessions/${encodeURIComponent(id)}/cancel`, actor, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ reason }),
  });
}

function webhookQuery(filters: { status?: string; q?: string; limit?: number; offset?: number }) {
  const query = new URLSearchParams();
  if (filters.status) query.set("status", filters.status);
  if (filters.q) query.set("q", filters.q);
  if (filters.limit) query.set("limit", String(filters.limit));
  if (filters.offset) query.set("offset", String(filters.offset));
  return query.size ? `?${query}` : "";
}

export function listWebhookInbox(actor: string, filters: { status?: string; q?: string; limit?: number; offset?: number } = {}) {
  return proxyRequest<WebhookInboxList>(`/api/v1/webhook-inbox${webhookQuery(filters)}`, actor);
}

export function listWebhookDeliveries(actor: string, filters: { status?: string; q?: string; limit?: number; offset?: number } = {}) {
  return proxyRequest<WebhookDeliveryList>(`/api/v1/webhook-deliveries${webhookQuery(filters)}`, actor);
}

export function replayWebhookDelivery(actor: string, id: string, expectedReplayCount: number, idempotencyKey: string) {
  return proxyRequest<WebhookDelivery>(`/api/v1/webhook-deliveries/${encodeURIComponent(id)}/replay`, actor, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ expected_replay_count: expectedReplayCount }),
  });
}
