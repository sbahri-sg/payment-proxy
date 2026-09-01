import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { EmisellWebhookContractGuide } from "../components/webhooks/emisell-contract-guide";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

type Endpoint = {
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  title: string;
  description: string;
  headers?: string[];
  body?: string;
  response?: string;
  note?: string;
  examples?: EndpointExample[];
};

type EndpointExample = {
  title: string;
  description?: string;
  headers?: string[];
  body?: string;
  response?: string;
  note?: string;
};

type Group = { id: string; title: string; summary: string; endpoints: Endpoint[] };

type ContractID = "backend" | "admin" | "partner";

type Contract = {
  id: ContractID;
  label: string;
  classification: string;
  status: string;
  audience: string;
  basePath: string;
  authentication: string;
  description: string;
};

const contracts: Contract[] = [
  {
    id: "backend",
    label: "Emisell Backend API",
    classification: "SERVICE",
    status: "ACTIVE",
    audience: "Emisell Backend → Payment Proxy",
    basePath: "/api/v1/*",
    authentication: "Service Bearer key + Merchant ID",
    description: "Kontrak minimal untuk katalog provider, koneksi merchant, metode checkout, payment, refund bersyarat, dan event canonical. Detail runtime serta operasi admin sengaja tidak diekspos sebagai kewajiban integrasi.",
  },
  {
    id: "admin",
    label: "Admin Control Plane",
    classification: "OPERATOR",
    status: "INTERNAL",
    audience: "Dashboard/operator → Payment Proxy",
    basePath: "/api/v1/admin/* + operational routes",
    authentication: "Admin key; service key untuk tenant operations",
    description: "Operasi release provider, API key, observability, certification, delivery, dan reconciliation. Emisell Backend tidak perlu mengimplementasikan alur ini.",
  },
  {
    id: "partner",
    label: "Connector Runtime Contract",
    classification: "PRIVATE",
    status: "ACTIVE",
    audience: "Payment Proxy → Provider Connector",
    basePath: "/partner/v1/*",
    authentication: "Private per-runtime bearer token",
    description: "Kontrak universal yang harus diimplementasikan connector vendor tanpa membocorkan bentuk API native provider ke Payment Kernel.",
  },
];

type Readiness = { status: string; checks?: Record<string, string> };

async function getReadiness(): Promise<Readiness> {
  const base = process.env.BACKEND_API_URL ?? "http://127.0.0.1:8080";
  try {
    const response = await fetch(`${base}/health/ready`, {
      cache: "no-store",
      signal: AbortSignal.timeout(2500),
    });
    return (await response.json()) as Readiness;
  } catch {
    return { status: "unreachable", checks: { api: "unavailable" } };
  }
}

const installationBase = `{
  "id": "ins_01k3...",
  "merchant_id": "merchant_123",
  "provider_code": "xendit",
  "provider_name": "Xendit",
  "environment": "sandbox",
  "public_webhook_url": "https://payments.example.com/webhooks/v1/providers/xendit/ins_01k3...",
  "execution_engine": "emisell_native",
  "provider_version": "emisell-xendit-v1",
  "status": "CONFIG_REQUIRED",
  "credential_metadata": {},
  "payment_methods": [],
  "version": 1,
  "created_at": "2026-08-28T03:00:00Z",
  "updated_at": "2026-08-28T03:00:00Z"
}`;

const assignmentBase = `{
  "id": "pmo_01k3...",
  "environment": "sandbox",
  "payment_method_code": "qris",
  "payment_method": "real_time_payment",
  "payment_method_type": "qris",
  "installation_id": "ins_01k3...",
  "provider_code": "xendit",
  "provider_name": "Xendit",
  "label": "QRIS",
  "status": "ACTIVE",
  "version": 1,
  "created_at": "2026-08-28T03:09:30Z",
  "updated_at": "2026-08-28T03:09:30Z"
}`;

const certificationBase = `{
  "id": "cert_01k3...",
  "installation_id": "ins_01k3...",
  "provider_code": "xendit",
  "provider_name": "Xendit",
  "payment_method_code": "qris",
  "payment_method_name": "QRIS",
  "environment": "sandbox",
  "status": "PASSED",
  "checks": [
    { "code": "installation", "label": "Active sandbox installation", "status": "PASSED" },
    { "code": "payment_create", "label": "Sandbox payment create", "status": "PASSED", "detail": "PENDING" },
    { "code": "next_action", "label": "Customer next action", "status": "PASSED" },
    { "code": "payment_retrieve", "label": "Provider payment retrieval", "status": "PASSED" },
    { "code": "webhook_delivery", "label": "Direct provider webhook received", "status": "PASSED" },
    { "code": "emisell_delivery", "label": "Signed event delivered to Emisell Backend", "status": "PASSED" }
  ],
  "payment_id": "pay_01k3...",
  "message": "Provider webhook and Emisell delivery passed.",
  "initiated_by": "emisell-backend",
  "completed_at": "2026-08-28T06:00:00Z"
}`;

const paymentBase = `{
  "id": "pay_01k3...",
  "installation_id": "ins_01k3...",
  "payment_option_id": "pmo_01k3...",
  "payment_method_code": "qris",
  "provider_code": "xendit",
  "provider_version": "emisell-xendit-v1",
  "environment": "sandbox",
  "merchant_reference": "order_2026_0001",
  "amount": 1000000,
  "currency": "IDR",
  "status": "PENDING",
  "flags": [],
  "provider_payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
  "execution_engine": "emisell_native",
  "next_action": { "type": "qr_code_information", "raw_qr_data": "00020101..." },
  "created_at": "2026-08-28T03:10:00Z",
  "updated_at": "2026-08-28T03:10:01Z"
}`;

const paymentListBase = `{
  "id": "pay_01k3...",
  "installation_id": "ins_01k3...",
  "provider_code": "xendit",
  "environment": "sandbox",
  "merchant_reference": "order_2026_0001",
  "amount": 1000000,
  "currency": "IDR",
  "status": "PENDING",
  "provider_payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
  "created_at": "2026-08-28T03:10:00Z",
  "updated_at": "2026-08-28T03:10:01Z"
}`;

const refundBase = `{
  "id": "ref_01k3...",
  "payment_id": "pay_01k3...",
  "payment_method_code": "qris",
  "amount": 1000000,
  "currency": "IDR",
  "reason": "REQUESTED_BY_CUSTOMER",
  "requested_by": "emisell-backend",
  "status": "PENDING",
  "provider_refund_id": "rfd-6f4a...",
  "created_at": "2026-08-28T03:20:00Z",
  "updated_at": "2026-08-28T03:20:01Z"
}`;

const providerAppBase = `{
  "id": "papp_01k3...",
  "provider_code": "midtrans",
  "provider_name": "Midtrans",
  "version": "0.1.0",
  "status": "UPLOADED",
  "runtime": "isolated_container",
  "sdk_version": "v1",
  "file_name": "midtrans-connector-0.1.0.zip",
  "artifact_size": 1843200,
  "artifact_sha256": "a791f4...9d2c",
  "manifest": {
    "contract_version": "v1",
    "entrypoint": "connector",
    "operations": ["verify_installation", "create_payment", "get_payment", "handle_webhook"],
    "credential_fields": [{ "code": "server_key", "label": "Server key", "input_type": "password", "secret": true, "required": true }],
    "environments": ["sandbox", "live"],
    "payment_methods": ["qris"],
    "outbound_hosts": ["api.midtrans.com"]
  },
  "scan_report": {
    "passed": true,
    "file_count": 3,
    "uncompressed_size": 3921000,
    "checks": [
      { "code": "archive_safety", "status": "PASSED", "detail": "ZIP paths, file count, and expanded size are within limits" },
      { "code": "manifest_contract", "status": "PASSED", "detail": "Connector SDK v1 manifest is valid" },
      { "code": "artifact_checksums", "status": "PASSED", "detail": "Every packaged file is covered by SHA-256" }
    ]
  },
  "submitted_by": "payment-proxy-admin",
  "created_at": "2026-08-28T14:00:00Z",
  "updated_at": "2026-08-28T14:00:00Z"
}`;

function installationStateResponse(status: string, version: number, uninstalledAt?: string) {
  return `{
  "data": {
    "id": "ins_01k3...",
    "provider_code": "xendit",
    "provider_name": "Xendit",
    "environment": "sandbox",
    "connector_id": "xendit:ins_01k3...",
    "execution_engine": "emisell_native",
    "provider_version": "emisell-xendit-v1",
    "status": "${status}",
    "credential_metadata": {
      "configured_fields": [
        { "code": "api_key", "configured": true }
      ],
      "configured_at": "2026-08-28T03:05:00Z"
    },
    "payment_methods": [],
    "version": ${version},
    "created_at": "2026-08-28T03:00:00Z",
    "updated_at": "2026-08-28T03:09:00Z"${uninstalledAt ? `,
    "uninstalled_at": "${uninstalledAt}"` : ""}
  }
}`;
}

const groups: Group[] = [
  {
    id: "observability",
    title: "Engine Observability & SLO",
    summary: "Process-level HTTP latency, availability, connector ambiguity, dan provider webhook counters untuk dashboard serta Prometheus scraper internal.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/admin/observability", title: "Get live SLO snapshot", description: "Mengembalikan telemetry process API sejak startup. Snapshot ini tidak menggantikan long-term metrics storage lintas replica.",
        response: `{
  "data": {
    "started_at": "2026-08-28T13:00:00Z",
    "uptime_seconds": 3600,
    "in_flight": 2,
    "requests_total": 10000,
    "responses": { "status_2xx": 9995, "status_3xx": 0, "status_4xx": 4, "status_5xx": 1 },
    "latency": { "average_ms": 18.42, "p95_ms": 50 },
    "connector_outcomes": { "unknown_outcome": 0, "not_supported": 0, "rejected": 2 },
    "provider_webhooks": { "accepted": 120, "duplicate": 3, "invalid": 0 },
    "slo": {
      "status": "MEETING",
      "availability_target_percent": 99.9,
      "availability_percent": 99.99,
      "latency_p95_target_ms": 500,
      "latency_p95_ms": 50
    }
  }
}`,
        note: "Availability process dihitung sebagai response non-5xx / seluruh request. Counter reset ketika process restart; production wajib scrape ke penyimpanan Prometheus-compatible.",
      },
      {
        method: "GET", path: "/api/v1/admin/metrics", title: "Prometheus metrics", description: "Format Prometheus untuk HTTP histogram, connector outcomes, dan provider webhook results. Endpoint menggunakan admin authentication yang sama.",
        response: `emisell_http_requests_total{class="2xx"} 9995
emisell_http_request_duration_seconds_bucket{le="0.5"} 9990
emisell_connector_outcomes_total{outcome="unknown"} 0
emisell_provider_webhooks_total{outcome="duplicate"} 3`,
        note: "Konfigurasikan X-Admin-API-Key pada scraper melalui secret manager. Jangan menaruh admin key pada query string.",
      },
      {
        method: "GET", path: "/api/v1/admin/engine/readiness", title: "Production readiness report", description: "Memeriksa database, connector registry, runtime configuration, dan menampilkan request guard efektif tanpa membocorkan secret.",
        response: `{
  "data": {
    "status": "ready",
    "environment": "production",
    "contract_version": "2026-08-28",
    "connector_count": 1,
    "checks": {
      "database": "ok",
      "connector_registry": "ok",
      "runtime_configuration": "ok"
    },
    "request_guards": {
      "max_body_bytes": 1048576,
      "timeout_seconds": 25,
      "rate_limit_rps": 300,
      "rate_limit_burst": 600,
      "max_in_flight": 500,
      "rate_limit_scope": "per_replica_per_merchant"
    }
  }
}`,
        note: "Rate limit ini melindungi satu replica. Production tetap wajib memakai ingress atau WAF untuk limit gabungan seluruh replica.",
      },
    ],
  },
  {
    id: "provider-apps",
    title: "Provider Apps",
    summary: "Provider-first control plane: daftarkan identitas provider, lalu upload, validasi, sertifikasi, dan publish versi connector miliknya.",
    endpoints: [
      {
        method: "POST", path: "/api/v1/admin/provider-app-providers", title: "Create provider identity", description: "Mendaftarkan provider terlebih dahulu. Provider code menjadi identitas permanen dan nama/code di setiap manifest ZIP berikutnya wajib sama.",
        body: `{
  "provider_code": "midtrans",
  "provider_name": "Midtrans",
  "description": "Midtrans payment connector for Emisell merchants.",
  "website_url": "https://midtrans.com",
  "documentation_url": "https://docs.midtrans.com",
  "support_email": "support@midtrans.com"
}`,
        response: `{ "data": {
  "provider_code": "midtrans",
  "provider_name": "Midtrans",
  "status": "DRAFT",
  "version_count": 0,
  "created_by": "payment-proxy-admin"
} }`,
        note: "Credential merchant/API key tidak dibuat di tahap ini. Credential tetap dimasukkan per installation dan disimpan terenkripsi.",
      },
      {
        method: "GET", path: "/api/v1/admin/provider-app-providers", title: "List registered providers", description: "Menampilkan satu record per provider beserta active version, latest version/status, dan jumlah immutable version. Riwayat versi tidak lagi terlihat sebagai provider duplikat.",
        response: `{ "data": [{ "provider_code": "midtrans", "provider_name": "Midtrans", "status": "ACTIVE", "version_count": 2, "active_version": "emisell-midtrans-v1.1.0", "latest_version": "emisell-midtrans-v1.1.0", "latest_status": "PUBLISHED" }] }`,
      },
      {
        method: "POST", path: "/api/v1/admin/provider-app-providers/{providerCode}/versions", title: "Upload provider version", description: "Menerima submission ZIP maksimum 25 MB. Root emisell-extension.yaml, openapi.yaml, safe paths, secret scan, SDK contract, credential schema, payment methods, dan outbound host divalidasi; code/name manifest harus cocok dengan provider pada URL. Native runtime binary ditolak.",
        headers: ["Content-Type: multipart/form-data"],
        body: `Postman → Body → form-data
KEY       TYPE    VALUE
bundle    File    xendit-provider-app-emisell-v1.zip`,
        response: `{ "data": ${providerAppBase} }`,
        note: "Submission hanya membawa kontrak dan bahan review. API key merchant tidak boleh berada di manifest, source, dokumentasi, atau nama file. Bundle binary lama masih dibaca sementara untuk kompatibilitas.",
      },
      {
        method: "GET", path: "/api/v1/admin/provider-app-providers/{providerCode}/versions", title: "List provider submission history", description: "Menampilkan seluruh versi untuk satu provider, termasuk release lama yang deprecated, tanpa mengembalikan binary ZIP.",
        response: `{ "data": [${providerAppBase}] }`,
      },
      {
        method: "POST", path: "/api/v1/admin/provider-apps/{id}/transition", title: "Validate Provider App", description: "Membaca ulang artifact immutable dari database, memverifikasi digest, keamanan ZIP, manifest, OpenAPI, dan secret scan, lalu memindahkan UPLOADED menjadi VALIDATED.",
        body: `{ "expected_status": "UPLOADED", "status": "VALIDATED", "review_note": "" }`,
        response: `{ "data": { "id": "papp_01k3...", "provider_code": "midtrans", "version": "0.1.0", "status": "VALIDATED" } }`,
      },
      {
        method: "POST", path: "/api/v1/admin/provider-apps/{id}/transition", title: "Certify Provider App", description: "Mencatat approval operator setelah conformance/security evidence diperiksa. Review note minimum delapan karakter dan seluruh perubahan diaudit.",
        body: `{ "expected_status": "VALIDATED", "status": "CERTIFIED", "review_note": "Sandbox conformance and security review passed." }`,
        response: `{ "data": { "id": "papp_01k3...", "provider_code": "midtrans", "version": "0.1.0", "status": "CERTIFIED" } }`,
      },
      {
        method: "POST", path: "/api/v1/admin/provider-apps/{id}/transition", title: "Publish Provider App", description: "Mempromosikan CERTIFIED menjadi PUBLISHED hanya ketika shared runtime memuat provider code dan exact manifest version yang sama. Digest executable runtime disimpan terpisah dari digest ZIP submission.",
        body: `{ "expected_status": "CERTIFIED", "status": "PUBLISHED", "review_note": "Isolated runtime 0.1.0 deployed and health checked." }`,
        response: `{ "data": { "id": "papp_01k3...", "provider_code": "midtrans", "version": "0.1.0", "status": "PUBLISHED", "published_at": "2026-08-28T15:00:00Z" } }`,
        note: "Jika runtime belum tersedia, response 409 CONNECTOR_RUNTIME_NOT_READY. Guard ini mencegah merchant meng-install connector yang belum dapat mengeksekusi transaksi.",
      },
    ],
  },
  {
    id: "engine-capabilities",
    title: "Engine Capabilities",
    summary: "Kontrak machine-readable untuk mengetahui connector, operation, credential field, dan profil sertifikasi yang benar-benar tersedia pada runtime.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/engine/capabilities", title: "Get runtime connector capabilities", description: "Dipakai Emisell Backend untuk memvalidasi kompatibilitas sebelum mengaktifkan provider atau fitur baru.",
        response: `{
  "data": {
    "engine": "emisell_payment_engine",
    "contract_version": "2026-08-28",
    "connector_contract": "v1",
    "selection_mode": "merchant_assignment",
    "unknown_policy": "reconcile_same_provider",
    "connectors": [
      {
        "code": "xendit",
        "name": "Xendit",
        "version": "emisell-xendit-v1",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "disable_installation", "create_payment", "get_payment", "simulate_payment", "handle_webhook"],
        "credential_fields": [{ "code": "api_key", "label": "Secret API key", "input_type": "password", "secret": true, "required": true }]
      },
      {
        "code": "midtrans",
        "name": "Midtrans",
        "version": "emisell-midtrans-v1.1.0",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "disable_installation", "create_payment", "get_payment", "handle_webhook"],
        "credential_fields": [
          { "code": "server_key", "label": "Server key", "input_type": "password", "secret": true, "required": true },
          { "code": "pop_id", "label": "PoP ID (Core API)", "input_type": "password", "secret": true, "required": false }
        ]
      }
    ],
    "integration_invariants": [
      "checkout_uses_opaque_payment_option_id",
      "payment_is_pinned_to_one_installation",
      "unknown_outcome_never_fails_over",
      "provider_credentials_never_leave_payment_platform"
    ]
  }
}`,
        note: "Katalog database menentukan metode yang tampil di checkout. Manifest runtime menentukan operasi yang benar-benar dapat dieksekusi. Keduanya harus sama-sama siap.",
      },
    ],
  },
  {
    id: "integration-readiness",
    title: "Integration Readiness",
    summary: "Bukti kesiapan integrasi Emisell Backend per merchant dan environment, dihitung otomatis dari aktivitas nyata.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/integration-readiness", title: "Get merchant integration readiness", description: "Mengembalikan checklist evidence-based untuk connection, payment method, create/read payment, idempotency, dan delivery webhook. Tidak sama dengan certification connector milik admin.",
        headers: ["X-Emisell-Execution-Mode: sandbox"],
        response: `{
  "data": {
    "environment": "sandbox",
    "status": "READY",
    "passed": 7,
    "total": 7,
    "resilience_evidence": false,
    "checks": [
      { "code": "provider_connection", "label": "Active provider connection", "status": "PASSED", "detail": "Verified from platform evidence." },
      { "code": "payment_method", "label": "Active payment method", "status": "PASSED", "detail": "Verified from platform evidence." },
      { "code": "payment_create", "label": "Payment creation", "status": "PASSED", "detail": "Verified from platform evidence." },
      { "code": "idempotency_replay", "label": "Idempotency replay", "status": "PASSED", "detail": "Verified from platform evidence." },
      { "code": "payment_status", "label": "Payment status lookup", "status": "PASSED", "detail": "Verified from platform evidence." },
      { "code": "backend_webhook", "label": "Emisell Backend webhook", "status": "PASSED", "detail": "Verified from platform evidence." },
      { "code": "webhook_delivery", "label": "Successful webhook delivery", "status": "PASSED", "detail": "Verified from platform evidence." }
    ]
  }
}`,
        note: "Sandbox dan Live dinilai terpisah. resilience_evidence menjadi true setelah platform pernah menangani late_payment atau provider_delayed_confirmation; indikator ini informatif dan tidak memblokir READY.",
      },
    ],
  },
  {
    id: "service-api-keys",
    title: "Emisell Backend API Keys",
    summary: "Dashboard admin menerbitkan, melihat metadata, dan mencabut credential full-access untuk Main Service Emisell. Plaintext hanya tampil satu kali.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/admin/service-api-keys", title: "List service API keys", description: "Menampilkan key aktif dan revoked dalam bentuk fingerprint tersamarkan. Endpoint admin tidak pernah mengembalikan secret asli.",
        response: `{
  "data": [{
    "id": "sak_01k3...",
    "name": "Emisell Backend Production",
    "key_hint": "epk_AbC123xy••••••••9Xyz",
    "scopes": ["gateway:full"],
    "status": "ACTIVE",
    "created_by": "payment-proxy-admin",
    "created_at": "2026-08-28T10:00:00Z"
  }]
}`,
      },
      {
        method: "POST", path: "/api/v1/admin/service-api-keys", title: "Generate full-access API key", description: "Membuat random 256-bit Bearer credential. Database hanya menyimpan SHA-256 hash; field secret hanya ada pada response pertama.",
        body: `{ "name": "Emisell Backend Production" }`,
        response: `{
  "data": {
    "api_key": {
      "id": "sak_01k3...",
      "name": "Emisell Backend Production",
      "key_hint": "epk_AbC123xy••••••••9Xyz",
      "scopes": ["gateway:full"],
      "status": "ACTIVE"
    },
    "secret": "epk_<one-time-plaintext>"
  }
}`,
        note: "Salin secret langsung ke secret manager Emisell Backend. Secret tidak dapat ditampilkan ulang dan tidak boleh ditempatkan pada browser, log, Postman export, atau source code.",
      },
      {
        method: "POST", path: "/api/v1/admin/service-api-keys/{id}/revoke", title: "Revoke service API key", description: "Mencabut key secara langsung tanpa restart API. Record dan audit tetap dipertahankan.",
        response: `{
  "data": {
    "id": "sak_01k3...",
    "name": "Emisell Backend Production",
    "key_hint": "epk_AbC123xy••••••••9Xyz",
    "scopes": ["gateway:full"],
    "status": "REVOKED",
    "revoked_by": "payment-proxy-admin",
    "revoked_at": "2026-08-28T12:00:00Z"
  }
}`,
        note: "Untuk rotasi tanpa downtime: generate key baru, verifikasi pada Emisell Backend, lalu revoke key lama.",
      },
    ],
  },
  {
    id: "providers",
    title: "Provider Registry",
    summary: "Membaca katalog provider dan dynamic credential schema.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/providers", title: "List providers", description: "Mengembalikan availability, environment, payment method, dan schema credential setiap provider.",
        response: `{
  "data": [
    {
      "code": "xendit",
      "name": "Xendit",
      "description": "Native Xendit connector for Emisell Payment Engine",
      "available": true,
      "connector_code": "xendit",
      "credential_schema": [
        { "code": "api_key", "label": "Secret API Key", "secret": true, "required": true }
      ],
      "environments": ["sandbox", "live"],
      "payment_methods": ["qris"]
    },
    {
      "code": "midtrans",
      "name": "Midtrans",
      "description": "Isolated Midtrans Core API connector for Emisell Payment Engine",
      "available": true,
      "connector_code": "midtrans",
      "credential_schema": [
        { "code": "server_key", "label": "Server key", "secret": true, "required": true },
        { "code": "pop_id", "label": "PoP ID (Core API)", "secret": true, "required": false }
      ],
      "environments": ["sandbox", "live"],
      "payment_methods": ["qris", "va_bca", "va_mandiri", "va_bni", "va_bri", "va_permata", "va_cimb", "ewallet_gopay", "ewallet_shopeepay"]
    }
  ]
}`,
      },
    ],
  },
  {
    id: "installations",
    title: "Provider Installations",
    summary: "Lifecycle install, konfigurasi, aktivasi, dan uninstall provider per merchant.",
    endpoints: [
      {
        method: "POST", path: "/api/v1/provider-installations", title: "Install provider", description: "Membuat installation baru dalam status CONFIG_REQUIRED. Versi dipilih otomatis dari runtime connector yang sedang berjalan.",
        body: `{
  "provider_code": "xendit",
  "environment": "sandbox"
}`,
        response: `{ "data": ${installationBase} }`,
        note: "Switch Sandbox/Live di Dashboard Merchant memilih slot installation yang terpisah. Credential yang dimasukkan akan dideteksi connector dan ditolak bila mode key tidak sesuai dengan slot. provider_version boleh dikirim untuk explicit version pin, tetapi integrasi normal sebaiknya menghilangkannya.",
      },
      {
        method: "GET", path: "/api/v1/provider-installations", title: "List installations", description: "List installation milik Merchant ID beserta metadata credential tersamarkan. Execution mode header dapat digunakan sebagai filter.",
        headers: ["X-Emisell-Execution-Mode: sandbox (optional)"],
        response: `{ "data": [${installationBase}] }`,
      },
      {
        method: "GET", path: "/api/v1/provider-installations/{id}", title: "Get installation", description: "Membaca Merchant ID, state, version, connector reference, dan metadata credential tersamarkan. Secret tidak pernah dikembalikan.",
        response: `{ "data": ${installationBase} }`,
      },
      {
        method: "PUT", path: "/api/v1/provider-installations/{id}/credentials", title: "Configure credentials", description: "Memverifikasi credential langsung ke provider, menyimpannya terenkripsi AES-GCM, dan menyiapkan webhook ingress milik Payment Proxy. public_webhook_url dikembalikan ke Emisell Backend untuk ditampilkan pada alur koneksi di Dashboard Emisell.",
        body: `{
  "credentials": {
    "api_key": "xnd_development_...",
    "webhook_verification_token": "token-from-xendit-dashboard"
  },
  "payment_methods": []
}`,
        response: `{
  "data": {
    "id": "ins_01k3...",
    "provider_code": "xendit",
    "environment": "sandbox",
    "public_webhook_url": "https://payments.example.com/webhooks/v1/providers/xendit/ins_01k3...",
    "connector_id": "xendit:ins_01k3...",
    "execution_engine": "emisell_native",
    "provider_version": "emisell-xendit-v1",
    "status": "READY",
    "credential_metadata": {
      "configured_fields": [
        { "code": "api_key", "configured": true }
      ],
      "configured_at": "2026-08-28T03:05:00Z",
      "verified_environment": "sandbox",
      "webhook_ready": true,
      "public_webhook_url": "https://payments.example.com/webhooks/v1/providers/xendit/ins_01k3..."
    },
    "payment_methods": [],
    "version": 4,
    "created_at": "2026-08-28T03:00:00Z",
    "updated_at": "2026-08-28T03:05:00Z"
  }
}`,
        note: "Dashboard Emisell adalah pemilik alur setup seller dan menampilkan public_webhook_url. Dashboard Payment Proxy hanya menampilkan status operasional ingress. Xendit membutuhkan setup manual satu kali melalui Dashboard Emisell; Midtrans memakai X-Override-Notification otomatis per transaksi. PAYMENT_PROXY_PUBLIC_BASE_URL wajib HTTPS publik.",
      },
      {
        method: "PATCH", path: "/api/v1/provider-installations/{id}/credentials", title: "Edit credentials", description: "Merotasi sebagian credential tanpa meminta merchant mengetik ulang seluruh secret. Field yang tidak dikirim tetap memakai nilai terenkripsi sebelumnya, lalu seluruh credential diverifikasi ulang ke provider.",
        body: `{
  "credentials": {
    "api_key": "xnd_development_ROTATED_..."
  },
  "clear_fields": []
}`,
        response: `{
  "data": {
    "id": "ins_01k3...",
    "provider_code": "xendit",
    "environment": "sandbox",
    "status": "READY",
    "credential_metadata": {
      "configured_fields": [
        { "code": "api_key", "configured": true },
        { "code": "webhook_verification_token", "configured": true }
      ],
      "verified_environment": "sandbox"
    },
    "version": 5
  }
}`,
        note: "Secret lama tidak pernah dikembalikan. Nilai kosong tidak menghapus secret; gunakan clear_fields hanya untuk field opsional. Installation ACTIVE harus dideaktivasi sebelum credential diedit.",
      },
      {
        method: "POST", path: "/api/v1/provider-installations/{id}/activate", title: "Activate", description: "Mengubah READY atau INACTIVE menjadi ACTIVE.", body: `{ "version": 4 }`,
        response: installationStateResponse("ACTIVE", 5),
      },
      {
        method: "POST", path: "/api/v1/provider-installations/{id}/deactivate", title: "Deactivate", description: "Menghentikan penggunaan installation tanpa menghapus connector account.", body: `{ "version": 5 }`,
        response: installationStateResponse("INACTIVE", 6),
      },
      {
        method: "POST", path: "/api/v1/provider-installations/{id}/upgrade", title: "Upgrade Provider App", description: "Memindahkan installation INACTIVE ke release Provider App yang sudah dimuat oleh runtime.",
        body: `{ "version": 6, "provider_version": "emisell-midtrans-v1.1.0" }`,
        response: `{ "data": { "id": "ins_01k3...", "provider_code": "midtrans", "provider_version": "emisell-midtrans-v1.1.0", "status": "CONFIG_REQUIRED", "credential_metadata": { "verification_required": true, "verification_reason": "provider_version_upgrade" }, "version": 7 } }`,
        note: "Target harus RELEASED dan tersedia pada shared runtime. Credential tetap terenkripsi, tetapi versi baru wajib melewati Save & verify hingga READY sebelum dapat diaktifkan kembali.",
      },
      {
        method: "DELETE", path: "/api/v1/provider-installations/{id}", title: "Uninstall", description: "Menghapus credential terenkripsi lalu melakukan soft-uninstall installation.",
        response: installationStateResponse("UNINSTALLED", 7, "2026-08-28T03:09:00Z"),
        note: "Installation ACTIVE harus dideaktivasi terlebih dahulu. Riwayat transaksi dan audit tetap dipertahankan.",
      },
    ],
  },
  {
    id: "certifications",
    title: "Connector Certification",
    summary: "Menjalankan release-gate sandbox dan menyimpan bukti capability tanpa pernah menguji live credential.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/connector-certifications?provider=xendit&limit=25", title: "List certification runs", description: "Mengembalikan bukti run tenant-scoped, hasil check, payment sandbox terkait, dan blocker connector.",
        headers: ["X-Emisell-Execution-Mode: sandbox (optional)"],
        response: `{ "data": [${certificationBase}] }`,
      },
      {
        method: "POST", path: "/api/v1/connector-certifications/run", title: "Run or resume sandbox certification", description: "Menjalankan create, next-action, retrieve, simulator atau customer authorization, dan pemeriksaan webhook pada connector sandbox aktif. Kirim payment_id untuk memverifikasi ulang payment redirect/mobile yang sama.",
        headers: ["X-Emisell-Execution-Mode: sandbox"],
        body: `{
  "installation_id": "ins_01k3...",
  "payment_method_code": "qris"
}`,
        response: `{ "data": ${certificationBase} }`,
        note: "Endpoint hanya menerima sandbox. PASS mempromosikan DOCUMENTED menjadi CERTIFIED dengan audit evidence. E-wallet mengembalikan BLOCKED sampai aksi pelanggan selesai, lalu di-resume memakai payment_id yang sama.",
      },
    ],
  },
  {
    id: "payment-options",
    title: "Checkout Payment Methods",
    summary: "Master canonical menyatukan nama metode lintas gateway; merchant memetakan satu metode ke satu installation aktif per environment.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/payment-methods", title: "Master payment-method catalog", description: "Mengembalikan metode canonical Emisell dan matriks dukungan Xendit, Midtrans, Duitku, serta DOKU.",
        response: `{
  "data": [{
    "code": "qris",
    "category": "QR_CODE",
    "name": "QRIS",
    "description": "Pembayaran QR nasional melalui mobile banking atau e-wallet.",
    "countries": ["ID"],
    "currencies": ["IDR"],
    "providers": [
      { "provider_code": "xendit", "provider_name": "Xendit", "support_status": "CERTIFIED", "provider_channel_code": "QRIS" },
      { "provider_code": "midtrans", "provider_name": "Midtrans", "support_status": "DOCUMENTED", "provider_channel_code": "other_qris" }
    ]
  }]
}`,
        note: "DOCUMENTED berarti tersedia di gateway, tetapi belum boleh dipakai sampai Connector SDK Emisell berstatus CERTIFIED.",
      },
      {
        method: "GET", path: "/api/v1/payment-method-assignments", title: "List method assignments", description: "Mengembalikan assignment tenant termasuk yang inactive. Execution mode dapat digunakan sebagai filter.",
        headers: ["X-Emisell-Execution-Mode: sandbox (optional)"],
        response: `{ "data": [${assignmentBase}] }`,
      },
      {
        method: "PUT", path: "/api/v1/payment-method-assignments", title: "Assign gateway", description: "Mengikat payment method ke installation ACTIVE pada environment yang sama. Version 0 membuat assignment baru; update wajib memakai version terakhir.",
        headers: ["X-Emisell-Execution-Mode: sandbox"],
        body: `{
  "installation_id": "ins_...",
  "payment_method_code": "qris",
  "label": "QRIS",
  "version": 0
}`,
        response: `{ "data": ${assignmentBase} }`,
        note: "Mengganti assignment hanya memengaruhi payment berikutnya. Payment existing tetap menyimpan installation dan provider binding awal.",
      },
      {
        method: "POST", path: "/api/v1/payment-method-assignments/{id}/deactivate", title: "Deactivate checkout method", description: "Menyembunyikan option dari checkout baru menggunakan optimistic version.",
        body: `{ "version": 1 }`,
        response: `{ "data": ${assignmentBase.replace('"status": "ACTIVE"', '"status": "INACTIVE"').replace('"version": 1', '"version": 2')} }`,
      },
      {
        method: "GET", path: "/api/v1/payment-options", title: "List checkout options", description: "Mengembalikan opaque option ID yang assignment dan installation-nya sama-sama aktif.",
        headers: ["X-Emisell-Execution-Mode: sandbox"],
        response: `{
  "data": [{
    "id": "pmo_01k3...",
    "environment": "sandbox",
    "payment_method_code": "qris",
    "category": "QR_CODE",
    "label": "QRIS"
  }]
}`,
        note: "Checkout tidak menerima provider_code, installation_id, atau credential. Hanya Payment Kernel yang me-resolve opaque option ID ke gateway.",
      },
    ],
  },
  {
    id: "payments",
    title: "Payments",
    summary: "Kontrak canonical untuk create, retrieve, dan cancel pembayaran.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/payment-sessions?status=PENDING&provider=xendit&q=order_2026&limit=25&offset=0", title: "List payments", description: "Menampilkan payment milik tenant dengan filter status, provider, reference/ID, environment header, dan pagination berbasis offset.",
        headers: ["X-Emisell-Execution-Mode: sandbox (optional)"],
        response: `{
  "data": {
    "items": [${paymentListBase}],
    "total": 1,
    "limit": 25,
    "offset": 0,
    "has_more": false
  }
}`,
        note: "`next_action` tidak disertakan pada item list agar QR base64 tidak memperbesar payload. Ambil detail payment untuk menampilkan QRIS.",
      },
      {
        method: "POST", path: "/api/v1/payment-sessions", title: "Create payment", description: "Membuat payment melalui payment option merchant dan mengunci binding ke installation tersebut. Endpoint yang sama menangani QRIS, Virtual Account, e-wallet, dan hosted card berdasarkan payment_option_id.",
        examples: [
          {
            title: "QRIS, Virtual Account, atau e-wallet",
            description: "Gunakan payment_option_id aktif dari checkout options. Bentuk next_action mengikuti metode yang dipilih, misalnya QR, nomor VA, redirect, atau mobile authorization.",
            headers: ["X-Emisell-Execution-Mode: sandbox", "Idempotency-Key: checkout-order-123-attempt-1"],
            body: `{
  "payment_option_id": "pmo_...",
  "merchant_reference": "order_2026_0001",
  "amount": 1000000,
  "currency": "IDR",
  "confirm": true,
  "capture_method": "automatic",
  "customer": { "name": "Budi Santoso", "email": "budi@example.com" },
  "return_url": "https://shop.example/payments/return",
  "metadata": { "order_id": "order_2026_0001" }
}`,
            response: `{
  "data": {
    "payment": ${paymentBase}
  }
}`,
            note: "payment_option_id direkomendasikan. installation_id + method lama tetap diterima selama migrasi. Retry dengan idempotency key dan payload yang sama tetap mengembalikan payment existing meskipun assignment berubah. Amount menggunakan minor unit: 1.000.000 berarti Rp10.000.",
          },
          {
            title: "Hosted card",
            description: "Payment option card membuat hosted Xendit Payment Session dan mengembalikan redirect URL. Data kartu tidak pernah melewati Emisell.",
            headers: ["X-Emisell-Execution-Mode: sandbox", "Idempotency-Key: checkout-card-order-123-attempt-1"],
            body: `{
  "payment_option_id": "pmo_card_...",
  "merchant_reference": "order_card_2026_0001",
  "amount": 1000000,
  "currency": "IDR",
  "description": "Order card #2026-0001",
  "customer": { "name": "Budi Santoso", "email": "budi@example.com" },
  "return_url": "https://shop.example/payments/return",
  "metadata": { "order_id": "order_card_2026_0001" }
}`,
            response: `{
  "data": {
    "payment": {
      "id": "pay_01k3...",
      "provider_code": "xendit",
      "status": "PENDING",
      "provider_payment_id": "ps-6a915387...",
      "next_action": {
        "type": "redirect",
        "redirect_url": "https://dev.xen.to/..."
      }
    }
  }
}`,
            note: "Buka redirect_url pada browser pelanggan. PAN, expiry, CVV/CVN, dan OTP hanya boleh dimasukkan pada halaman hosted Xendit; jangan kirimkan field tersebut ke Payment Proxy, metadata, atau log.",
          },
        ],
      },
      {
        method: "GET", path: "/api/v1/payment-sessions/{id}", title: "Get payment", description: "Membaca local projection dan mencoba sinkronisasi langsung ke provider jika provider payment ID tersedia.",
        response: `{ "data": ${paymentBase} }`,
      },
      {
        method: "GET", path: "/api/v1/payment-sessions/{id}/timeline", title: "Payment timeline", description: "Membaca riwayat status durable dari create, provider sync, operator cancel, dan webhook provider.",
        response: `{
  "data": [
    { "id": 101, "payment_id": "pay_01k3...", "status": "CREATED", "source": "api.create", "details": { "merchant_reference": "order_2026_0001" }, "created_at": "2026-08-28T03:10:00Z" },
    { "id": 102, "payment_id": "pay_01k3...", "status": "PENDING", "source": "engine.create", "details": { "provider_payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5" }, "created_at": "2026-08-28T03:10:01Z" }
  ]
}`,
      },
      {
        method: "POST", path: "/api/v1/payment-sessions/{id}/cancel", title: "Cancel payment", description: "Membatalkan payment PENDING atau PROCESSING bila connector dan channel mendukung operasi cancel.", headers: ["Idempotency-Key: cancel-order-123-attempt-1"], body: `{ "reason": "requested_by_customer" }`,
        response: `{
  "data": {
    "id": "pay_01k3...",
    "installation_id": "ins_01k3...",
    "provider_code": "xendit",
    "environment": "sandbox",
    "merchant_reference": "order_2026_0001",
    "amount": 1000000,
    "currency": "IDR",
    "status": "CANCELLED",
    "provider_payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
    "updated_at": "2026-08-28T03:12:00Z"
  }
}`,
        note: "Payment UNKNOWN harus disinkronkan terlebih dahulu dan tidak boleh dibatalkan atau difailover otomatis.",
      },
    ],
  },
  {
    id: "refunds",
    title: "Refunds",
    summary: "Kontrak refund return-to-source untuk Emisell Backend. Eksekusi harus lolos policy payment channel dan operation release connector.",
    endpoints: [
      {
        method: "POST", path: "/api/v1/refunds", title: "Create refund", description: "Membuat refund memakai provider, environment, installation, dan credential transaksi asal. Implementasi Xendit QRIS sudah siap, tetapi release gate tetap tertutup sampai sandbox dan webhook evidence tersertifikasi.",
        headers: ["Idempotency-Key: refund-order-123-part-1"],
        body: `{
  "payment_id": "pay_...",
  "amount": 1000000,
  "reason": "REQUESTED_BY_CUSTOMER",
  "metadata": { "case_id": "case_123" }
}`,
        response: `{ "data": ${refundBase} }`,
        note: "Saat ini Xendit mengembalikan REFUND_NOT_SUPPORTED sebelum credential dibuka. Setelah capability tersertifikasi, respons PENDING tetap bukan bukti dana sudah terlihat pada pelanggan; status final harus berasal dari webhook provider.",
      },
      {
        method: "GET", path: "/api/v1/refunds/{id}", title: "Get refund", description: "Membaca projection refund canonical. Xendit Unified Refund diselesaikan melalui webhook; connector tidak mengarang endpoint lookup provider yang tidak terdokumentasi.",
        response: `{ "data": ${refundBase} }`,
      },
    ],
  },
  {
    id: "webhooks",
    title: "Webhooks",
    summary: "Ingress event langsung dari provider dan durable delivery menuju Emisell.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/admin/emisell-webhook", title: "Read outbound webhook settings", description: "Membaca Callback URL, status aktif, masked secret, sumber konfigurasi, dan hasil connection test terakhir. Plaintext secret tidak pernah dikembalikan.",
        response: `{
  "data": {
    "configured": true,
    "callback_url": "https://api.emisell.com/webhooks/v1/payment-proxy",
    "enabled": false,
    "secret_configured": true,
    "secret_hint": "whsec_Abc••••••••9Xyz",
    "source": "database",
    "last_test_success": true,
    "last_test_http_status": 202,
    "updated_by": "payment-proxy-admin"
  }
}`,
        note: "Endpoint admin memakai X-Admin-API-Key dan hanya boleh dipanggil dashboard server, bukan browser langsung.",
      },
      {
        method: "PUT", path: "/api/v1/admin/emisell-webhook", title: "Save Callback URL and delivery state", description: "Callback URL diisi manual. Worker membaca perubahan database pada poll berikutnya tanpa rebuild atau restart container.",
        body: `{
  "callback_url": "https://api.emisell.com/webhooks/v1/payment-proxy",
  "enabled": true
}`,
        response: `{
  "data": {
    "configured": true,
    "callback_url": "https://api.emisell.com/webhooks/v1/payment-proxy",
    "enabled": true,
    "secret_configured": true,
    "secret_hint": "whsec_Abc••••••••9Xyz",
    "source": "database"
  }
}`,
        note: "Production hanya menerima HTTPS publik. Delivery tidak dapat diaktifkan sebelum secret dibuat.",
      },
      {
        method: "POST", path: "/api/v1/admin/emisell-webhook/secret", title: "Generate or rotate webhook secret", description: "Membuat 256-bit random secret berprefix whsec_, mengenkripsinya dengan AES-GCM, dan menampilkan plaintext satu kali pada response.",
        response: `{
  "data": {
    "settings": {
      "callback_url": "https://api.emisell.com/webhooks/v1/payment-proxy",
      "enabled": false,
      "secret_configured": true,
      "secret_hint": "whsec_Abc••••••••9Xyz",
      "source": "database"
    },
    "secret": "whsec_<one-time-plaintext>"
  }
}`,
        note: "Generate/rotate selalu fail-closed: delivery dinonaktifkan sampai secret baru dipasang di Emisell Backend, connection test berhasil, dan operator mengaktifkannya kembali. Secret tidak dapat dilihat lagi.",
      },
      {
        method: "POST", path: "/api/v1/admin/emisell-webhook/test", title: "Send signed connection test", description: "Mengirim event canonical webhook.test menggunakan Callback URL dan secret tersimpan, tanpa customer, order, payment, atau credential provider.",
        response: `{
  "data": {
    "success": true,
    "http_status": 202,
    "event_id": "evt_test_01k3...",
    "tested_at": "2026-08-28T10:30:00Z",
    "message": "Signed webhook test was accepted by Emisell Backend."
  }
}`,
      },
      {
        method: "POST", path: "https://api.emisell.com/webhooks/v1/payment-proxy", title: "Emisell Backend receiver contract", description: "Endpoint ini dimiliki Emisell Backend. Payment Proxy mengirim event canonical dari durable outbox; event provider mentah tidak pernah diteruskan langsung.",
        headers: [
          "X-Emisell-Webhook-ID: evt_01k3...",
          "X-Emisell-Webhook-Timestamp: 1787907600",
          "X-Emisell-Webhook-Signature: v1=<hmac-sha256>",
          "X-Emisell-Webhook-Version: 1",
          "X-Emisell-Event-Type: payment.updated",
          "X-Emisell-Merchant-ID: merchant_123",
          "Idempotency-Key: evt_01k3...",
        ],
        body: `{
  "id": "evt_01k3...",
  "object": "event",
  "api_version": "2026-08-28",
  "type": "payment.updated",
  "created_at": "2026-08-28T09:30:00Z",
  "merchant_id": "merchant_123",
  "resource": { "type": "payment", "id": "pay_01k3..." },
  "data": {
    "payment": {
      "id": "pay_01k3...",
      "merchant_reference": "order_2026_0001",
      "amount": 1000000,
      "currency": "IDR",
      "environment": "sandbox",
      "status": "SUCCEEDED",
      "updated_at": "2026-08-28T09:30:00Z"
    },
    "previous_status": "PENDING"
  }
}`,
        response: `{ "accepted": true, "duplicate": false, "event_id": "evt_01k3..." }`,
        note: "Emisell Backend wajib menghitung HMAC-SHA256 atas timestamp + '.' + raw body, membandingkan ID/type/merchant pada header dan body, menolak timestamp kedaluwarsa, lalu deduplicate berdasarkan event id. Balas 2xx hanya setelah event tersimpan durable.",
      },
      {
        method: "GET", path: "/api/v1/webhook-inbox?status=PROCESSED&q=payment&limit=25&offset=0", title: "List webhook inbox", description: "Menampilkan metadata aman webhook provider yang berhasil dipetakan ke tenant. Raw payload tetap terenkripsi dan tidak pernah dikirim ke dashboard.",
        response: `{
  "data": {
    "items": [{
      "id": "wh_01k3...",
      "source": "xendit",
      "external_event_id": "webhook-unique-id",
      "event_type": "payment.capture",
      "aggregate_type": "payment",
      "aggregate_id": "pay_01k3...",
      "payload_sha256": "90f4...a281",
      "status": "PROCESSED",
      "received_at": "2026-08-28T05:26:11Z",
      "processed_at": "2026-08-28T05:26:11Z"
    }],
    "counts": { "PROCESSED": 3 },
    "total": 1,
    "limit": 25,
    "offset": 0,
    "has_more": false
  }
}`,
        note: "Event yang tidak dapat dipetakan ke tenant tidak tersedia melalui service API tenant-scoped.",
      },
      {
        method: "GET", path: "/api/v1/webhook-deliveries?status=DEAD&limit=25&offset=0", title: "List Emisell deliveries", description: "Menampilkan canonical outbox, attempt counter, HTTP response, error terakhir, dan metadata replay menuju Emisell Backend.",
        response: `{
  "data": {
    "items": [{
      "id": "evt_01k3...",
      "event_type": "payment.updated",
      "aggregate_type": "payment",
      "aggregate_id": "pay_01k3...",
      "payload": {
        "id": "evt_01k3...",
        "object": "event",
        "api_version": "2026-08-28",
        "type": "payment.updated",
        "merchant_id": "merchant_123",
        "resource": { "type": "payment", "id": "pay_01k3..." },
        "data": { "payment": { "id": "pay_01k3...", "status": "SUCCEEDED" }, "previous_status": "PENDING" }
      },
      "status": "DEAD",
      "attempt_count": 8,
      "max_attempts": 8,
      "last_http_status": 503,
      "last_error": "Emisell returned HTTP 503",
      "replay_count": 0,
      "created_at": "2026-08-28T05:26:11Z",
      "updated_at": "2026-08-28T05:31:11Z"
    }],
    "counts": { "DEAD": 1 },
    "total": 1,
    "limit": 25,
    "offset": 0,
    "has_more": false
  }
}`,
      },
      {
        method: "POST", path: "/api/v1/webhook-deliveries/{id}/replay", title: "Replay dead delivery", description: "Membuka attempt window baru untuk delivery DEAD dengan optimistic replay count dan audit operator.",
        headers: ["Idempotency-Key: replay-event-123-attempt-1"],
        body: `{ "expected_replay_count": 0 }`,
        response: `{
  "data": {
    "id": "evt_01k3...",
    "event_type": "payment.updated",
    "aggregate_type": "payment",
    "aggregate_id": "pay_01k3...",
    "status": "PENDING",
    "attempt_count": 0,
    "max_attempts": 8,
    "replay_count": 1,
    "last_replayed_by": "emisell-backend"
  }
}`,
        note: "Hanya status DEAD yang bisa direplay. Emisell consumer wajib deduplicate karena delivery bersifat at-least-once.",
      },
    ],
  },
  {
    id: "reconciliation",
    title: "Reconciliation",
    summary: "Tenant-scoped exception queue dan audited resolution untuk outcome engine yang ambigu.",
    endpoints: [
      {
        method: "GET", path: "/api/v1/reconciliation/cases?kind=PAYMENT_UNKNOWN&q=order_2026&limit=25&offset=0", title: "List reconciliation cases", description: "Menggabungkan payment/refund UNKNOWN, delivery DEAD, webhook FAILED, dan installation ERROR menjadi satu operational queue.",
        response: `{
  "data": {
    "items": [{
      "id": "payment:pay_01k3...",
      "kind": "PAYMENT_UNKNOWN",
      "resource_type": "payment",
      "resource_id": "pay_01k3...",
      "title": "Payment outcome unknown",
      "reference": "order_2026_0001",
      "provider_code": "xendit",
      "environment": "sandbox",
      "engine_reference": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
      "current_status": "UNKNOWN",
      "severity": "HIGH",
      "recommended_action": "SYNC_ENGINE",
      "can_resolve": true,
      "reconciliation_count": 0,
      "detected_at": "2026-08-28T06:00:00Z"
    }],
    "counts": { "PAYMENT_UNKNOWN": 1 },
    "total": 1,
    "limit": 25,
    "offset": 0,
    "has_more": false
  }
}`,
      },
      {
        method: "POST", path: "/api/v1/reconciliation/payments/{id}/resolve", title: "Resolve unknown payment", description: "Mengambil resource payment yang sama langsung dari provider lalu menerapkan status canonical secara atomic dengan timeline dan audit operator.",
        headers: ["Idempotency-Key: reconcile-payment-123-attempt-1"],
        body: `{ "expected_reconciliation_count": 0 }`,
        response: `{
  "data": {
    "id": "pay_01k3...",
    "provider_code": "xendit",
    "environment": "sandbox",
    "merchant_reference": "order_2026_0001",
    "status": "PENDING",
    "provider_payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
    "reconciliation_count": 1,
    "last_reconciled_by": "emisell-backend",
    "last_reconciled_at": "2026-08-28T06:02:00Z"
  }
}`,
        note: "Endpoint hanya menerima payment UNKNOWN yang sudah memiliki provider payment ID. Endpoint tidak membuat payment baru dan tidak memicu provider failover.",
      },
    ],
  },
  {
    id: "health",
    title: "Health",
    summary: "Endpoint operasional tanpa authentication.",
    endpoints: [
      { method: "GET", path: "/health/live", title: "Liveness", description: "Memastikan proses API hidup. Tidak memanggil dependency.", response: `{ "service": "payment-proxy", "status": "ok" }` },
      { method: "GET", path: "/health/ready", title: "Readiness", description: "Memastikan PostgreSQL dan registry Emisell connector siap.", response: `{
  "status": "ready",
  "checks": { "database": "ok", "emisell_engine": "ok" }
}` },
    ],
  },
];

function scopedGroup(
  id: string,
  options: {
    includeTitles?: string[];
    excludeTitles?: string[];
    title?: string;
    summary?: string;
  } = {},
): Group {
  const source = groups.find((group) => group.id === id);
  if (!source) throw new Error(`documentation group ${id} is not defined`);
  const included = options.includeTitles
    ? options.includeTitles.map((title) => {
        const endpoint = source.endpoints.find((item) => item.title === title);
        if (!endpoint) throw new Error(`documentation endpoint ${id}/${title} is not defined`);
        return endpoint;
      })
    : source.endpoints.filter((endpoint) => !(options.excludeTitles ?? []).includes(endpoint.title));
  return {
    ...source,
    title: options.title ?? source.title,
    summary: options.summary ?? source.summary,
    endpoints: included,
  };
}

const backendGroups: Group[] = [
  scopedGroup("integration-readiness"),
  scopedGroup("providers", {
    title: "Available Providers",
    summary: "Katalog provider aktif dan schema field credential yang harus dirender Dashboard Merchant.",
  }),
  scopedGroup("installations", {
    excludeTitles: ["Upgrade Provider App"],
    title: "Merchant Provider Connections",
    summary: "Lifecycle koneksi merchant: Install → Configure/Verify → Activate. Sandbox dan Live adalah dua slot connection yang terpisah.",
  }),
  scopedGroup("payment-options", {
    summary: "Konfigurasi metode merchant dan daftar opaque payment_option_id yang aman dipakai checkout.",
  }),
  scopedGroup("payments", {
    includeTitles: ["Create payment", "Get payment", "Cancel payment"],
    summary: "Kontrak pembayaran normalized. Satu endpoint create menangani seluruh provider dan metode melalui payment_option_id.",
  }),
  scopedGroup("webhooks", {
    includeTitles: ["Emisell Backend receiver contract"],
    title: "Events to Emisell Backend",
    summary: "Satu receiver canonical yang dimiliki Emisell Backend. Raw webhook dan signature provider ditangani Payment Proxy.",
  }),
];

const adminGroups: Group[] = [
  scopedGroup("provider-apps"),
  scopedGroup("service-api-keys"),
  scopedGroup("observability"),
  scopedGroup("engine-capabilities", {
    title: "Runtime Diagnostics",
    summary: "Metadata runtime untuk operator dan Payment Kernel; bukan kontrak yang perlu dipanggil aplikasi merchant.",
  }),
  scopedGroup("installations", {
    includeTitles: ["Upgrade Provider App"],
    title: "Installation Maintenance",
    summary: "Upgrade versi connector adalah pekerjaan release/operations, bukan bagian dari alur merchant sehari-hari.",
  }),
  scopedGroup("certifications"),
  scopedGroup("payments", {
    includeTitles: ["List payments", "Payment timeline"],
    title: "Payment Operations",
    summary: "Read model untuk dashboard operasional. Emisell Backend tetap menjadi pemilik daftar order dan tidak wajib memanggil endpoint ini.",
  }),
  scopedGroup("webhooks", {
    includeTitles: [
      "Read outbound webhook settings",
      "Save Callback URL and delivery state",
      "Generate or rotate webhook secret",
      "Send signed connection test",
      "List webhook inbox",
      "List Emisell deliveries",
      "Replay dead delivery",
    ],
    title: "Webhook Operations",
    summary: "Konfigurasi dan troubleshooting delivery untuk operator. Provider ingress tidak menjadi request dari Emisell Backend.",
  }),
  scopedGroup("reconciliation"),
  scopedGroup("health"),
];

const omittedEndpointDecisions = [
  ["Tidak ada endpoint verify terpisah", "PUT/PATCH credential sudah sekaligus memverifikasi akses ke provider."],
  ["Tidak ada switch-environment endpoint", "Sandbox dan Live adalah dua connection slot; UI hanya berpindah slot."],
  ["Tidak ada /xendit atau /midtrans API", "Semua provider memakai normalized provider, installation, payment option, dan payment API."],
  ["Tidak ada merchant runtime/container API", "Runtime Dispatcher memilih shared runtime berdasarkan provider + version secara internal."],
  ["Tidak ada endpoint membaca secret", "API hanya mengembalikan configured_fields dan metadata verifikasi."],
  ["Tidak ada create endpoint per metode", "QRIS, VA, e-wallet, dan card memakai POST /payment-sessions dengan payment_option_id."],
  ["Refund merupakan kontrak inti bersyarat", "Backend hanya memanggilnya untuk channel dengan return-to-source policy dan connector release yang mendukung create_refund."],
] as const;

const partnerGroups: Group[] = [
  {
    id: "partner-discovery",
    title: "Discovery & Capabilities",
    summary: "Setiap connector runtime menyatakan kesiapan dan manifest yang benar-benar dimuat. Payment Kernel menemukan provider tanpa mengimpor implementasi vendor.",
    endpoints: [
      {
        method: "GET",
        path: "/partner/v1/health",
        title: "Connector health",
        description: "Probe ringan untuk memastikan connector vendor siap menerima request tanpa memanggil API pembayaran upstream.",
        response: `{
  "status": "ready",
  "connector_count": 1
}`,
      },
      {
        method: "GET",
        path: "/partner/v1/capabilities",
        title: "List runtime capabilities",
        description: "Mengembalikan manifest operation, schema credential, dan certification profile connector yang dimuat runtime tersebut.",
        response: `{
  "data": {
    "contract_version": "v1",
    "connectors": [
      {
        "code": "xendit",
        "name": "Xendit",
        "version": "emisell-xendit-v1",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "disable_installation", "create_payment", "get_payment", "simulate_payment", "handle_webhook"]
      }
    ]
  }
}`,
        note: "Xendit dan Midtrans memakai base URL serta bearer token terpisah. Capability yang diumumkan tetap harus lulus Certification Emisell sebelum dapat dipetakan ke checkout.",
      },
    ],
  },
  {
    id: "partner-installations",
    title: "Installation Lifecycle",
    summary: "Payment Proxy mengelola tenant dan lifecycle; connector hanya memverifikasi account provider dan mengembalikan reference opaque.",
    endpoints: [
      {
        method: "POST",
        path: "/partner/v1/installations/verify",
        title: "Verify installation",
        description: "Memverifikasi credential provider untuk environment tertentu dan mendaftarkan callback bila connector mendukungnya.",
        body: `{
  "installation_id": "ins_...",
  "provider_code": "xendit",
  "environment": "sandbox",
  "credentials": {
    "api_key": "<injected-secret>",
    "webhook_verification_token": "<injected-secret>"
  },
  "public_webhook_url": "https://payments.example.com/webhooks/v1/providers/xendit/ins_..."
}`,
        response: `{
  "data": {
    "connector_id": "xendit:ins_01k3...",
    "environment": "sandbox",
    "webhook_ready": true
  }
}`,
        note: "Connector mendeteksi mode dari credential provider dan mengembalikannya untuk mencegah key Sandbox dipasang ke slot Live atau sebaliknya. Credential hanya melewati jaringan privat Payment Kernel → Connector Runner, tidak dicatat, dan tidak dikembalikan pada response.",
      },
      {
        method: "POST",
        path: "/partner/v1/installations/disable",
        title: "Disable installation",
        description: "Menonaktifkan account connector dan subscription webhook tanpa menghapus ledger Payment Proxy.",
        body: `{
  "installation_id": "ins_...",
  "provider_code": "xendit",
  "environment": "sandbox",
  "credentials": {}
}`,
        response: `{ "data": { "disabled": true } }`,
      },
    ],
  },
  {
    id: "partner-payments",
    title: "Payment Operations",
    summary: "Bentuk request dan response selalu canonical. Mapping Xendit, Midtrans, DOKU, atau Duitku adalah tanggung jawab connector.",
    endpoints: [
      {
        method: "POST",
        path: "/partner/v1/payments/create",
        title: "CreatePayment",
        description: "Membuat payment pada provider dengan idempotency key dari Payment Kernel.",
        body: `{
  "provider_code": "xendit",
  "installation_id": "ins_...",
  "local_payment_id": "pay_...",
  "merchant_reference": "order_2026_0001",
  "environment": "sandbox",
  "credentials": { "api_key": "<injected-secret>" },
  "idempotency_key": "payment-order-123-attempt-1",
  "amount": 1000000,
  "currency": "IDR",
  "payment_method_code": "qris",
  "channel_code": "QRIS",
  "customer": { "name": "Budi Santoso", "email": "budi@example.com" },
  "return_url": "https://shop.example/payments/return",
  "metadata": { "order_id": "order_2026_0001" }
}`,
        response: `{
  "data": {
    "id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
    "status": "PENDING",
    "next_action": { "type": "qr_code_information", "raw_qr_data": "00020101..." }
  }
}`,
        note: "Timeout setelah request mungkin diterima provider harus dikembalikan sebagai OUTCOME_UNKNOWN. Connector tidak boleh membuat payment kedua atau memilih provider lain.",
      },
      {
        method: "POST",
        path: "/partner/v1/payments/get",
        title: "GetStatus",
        description: "Membaca status payment yang sama langsung dari provider untuk sinkronisasi atau recovery outcome UNKNOWN.",
        body: `{
  "provider_code": "xendit",
  "environment": "sandbox",
  "credentials": { "api_key": "<injected-secret>" },
  "payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5"
}`,
        response: `{
  "data": {
    "id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
    "status": "SUCCEEDED",
    "connector_transaction_id": "txn-provider-001"
  }
}`,
      },
      {
        method: "POST",
        path: "/partner/v1/payments/capture",
        title: "Capture",
        description: "Melakukan capture untuk channel manual-capture yang telah disertifikasi.",
        body: `{ "provider_code": "xendit", "environment": "sandbox", "credentials": { "api_key": "<injected-secret>" }, "payment_id": "pay_provider_123", "idempotency_key": "capture-payment-123-attempt-1", "amount": 1000000, "currency": "IDR" }`,
        response: `{ "data": { "id": "pay_provider_123", "status": "SUCCEEDED" } }`,
      },
      {
        method: "POST",
        path: "/partner/v1/payments/cancel",
        title: "Cancel",
        description: "Membatalkan payment yang belum final bila channel provider mendukung cancel.",
        body: `{
  "input": { "provider_code": "xendit", "environment": "sandbox", "credentials": { "api_key": "<injected-secret>" }, "payment_id": "pay_provider_123" },
  "idempotency_key": "cancel-payment-123-attempt-1",
  "reason": "requested_by_customer"
}`,
        response: `{ "data": { "id": "pay_provider_123", "status": "CANCELLED" } }`,
      },
    ],
  },
  {
    id: "partner-refunds",
    title: "Refund Operations",
    summary: "Refund dipisahkan dari payment dan hanya dibuka untuk capability provider yang telah disertifikasi.",
    endpoints: [
      {
        method: "POST",
        path: "/partner/v1/refunds/create",
        title: "Refund",
        description: "Membuat refund return-to-source terhadap provider payment ID yang sama hanya bila operation connector sudah lulus certification dan dipublikasikan.",
        body: `{
  "provider_code": "xendit",
  "environment": "sandbox",
  "credentials": { "api_key": "<injected-secret>" },
  "payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
  "idempotency_key": "refund-payment-123-part-1",
  "amount": 50000,
  "currency": "IDR",
  "reason": "REQUESTED_BY_CUSTOMER",
  "metadata": { "case_id": "case_123" }
}`,
        response: `{ "data": { "id": "ref_provider_123", "status": "PENDING" } }`,
      },
      {
        method: "POST",
        path: "/partner/v1/refunds/get",
        title: "Get refund status",
        description: "Kontrak opsional untuk connector yang mendokumentasikan lookup refund. Xendit tidak mengiklankan get_refund; status final Xendit masuk melalui verified webhook dan dibaca dari projection canonical.",
        body: `{ "provider_code": "provider_with_get_refund", "environment": "sandbox", "credentials": { "secret": "<injected-secret>" }, "refund_id": "ref_provider_123" }`,
        response: `{ "data": { "id": "ref_provider_123", "status": "SUCCEEDED" } }`,
      },
    ],
  },
  {
    id: "partner-webhooks",
    title: "Provider Webhook Contract",
    summary: "Provider mengirim event ke ingress stabil milik Payment Proxy; connector memverifikasi dan menormalisasi event untuk Payment Kernel.",
    endpoints: [
      {
        method: "POST",
        path: "/webhooks/v1/providers/{provider_code}/{installation_id}",
        title: "Provider webhook ingress (active)",
        description: "URL publik yang ditempel di dashboard provider. Payment Proxy melakukan deduplication dan meneruskan payload ke HandleWebhook connector.",
        headers: ["x-callback-token: <provider-callback-token>", "webhook-id: webhook-unique-id"],
        body: `{
  "event": "payment.capture",
  "data": {
    "payment_request_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
    "status": "SUCCEEDED"
  }
}`,
        response: `{ "accepted": true }`,
        note: "Route ingress ini sudah aktif. Nama header dan bentuk payload mengikuti provider dan tidak menjadi kontrak untuk Emisell Backend.",
      },
      {
        method: "POST",
        path: "/partner/v1/webhooks/normalize",
        title: "HandleWebhook",
        description: "Boundary connector menerima payload provider yang belum diubah, memverifikasi signature/token, lalu mengembalikan event minimal canonical.",
        body: `{
  "provider_code": "xendit",
  "credentials": { "webhook_verification_token": "<injected-secret>" },
  "headers": { "webhook-id": ["webhook-unique-id"], "x-callback-token": ["<injected-secret>"] },
  "body": "eyJldmVudCI6InBheW1lbnQuY2FwdHVyZSJ9"
}`,
        response: `{
  "data": {
    "id": "webhook-unique-id",
    "type": "payment.updated",
    "payment_id": "pr-8877c08a-740d-4153-9816-3d744ed197a5",
    "status": "SUCCEEDED"
  }
}`,
        note: "Connector tidak boleh mengirim raw payload ke Emisell Backend. Payment Proxy hanya membuat canonical payment.updated atau refund.updated dari hasil normalisasi.",
      },
    ],
  },
];

const commonHeaders = [
  "Authorization: Bearer <SERVICE_API_KEY>",
  "X-Emisell-Merchant-ID: merchant_123",
  "Content-Type: application/json",
];

const adminHeaders = [
  "X-Admin-API-Key: <ADMIN_API_KEY>            # /api/v1/admin/*",
  "Authorization: Bearer <SERVICE_API_KEY>     # tenant operational routes",
  "X-Emisell-Merchant-ID: merchant_123         # tenant operational routes",
  "Content-Type: application/json",
];

const partnerHeaders = [
  "Authorization: Bearer <CONNECTOR_RUNNER_TOKEN>",
  "Content-Type: application/json",
];

function Method({ value }: { value: Endpoint["method"] }) {
  return <span className={`method method-${value.toLowerCase()}`}>{value}</span>;
}

function Code({ children }: { children: string }) {
  return <pre><code>{children}</code></pre>;
}

function postmanPath(path: string) {
  if (path.startsWith("/api/v1/admin/service-api-keys/{id}")) return path.replace("{id}", "{{service_api_key_id}}");
  if (path.startsWith("/webhooks/v1/providers/{provider_code}/{installation_id}")) return path.replace("{provider_code}", "{{provider_code}}").replace("{installation_id}", "{{installation_id}}");
  if (path.startsWith("/api/v1/provider-installations/{id}")) return path.replace("{id}", "{{installation_id}}");
  if (path.startsWith("/api/v1/payment-method-assignments/{id}")) return path.replace("{id}", "{{payment_option_id}}");
  if (path.startsWith("/api/v1/payment-sessions/{id}")) return path.replace("{id}", "{{payment_id}}");
  if (path.startsWith("/api/v1/refunds/{id}")) return path.replace("{id}", "{{refund_id}}");
  if (path.startsWith("/api/v1/webhook-deliveries/{id}")) return path.replace("{id}", "{{delivery_id}}");
  if (path.startsWith("/api/v1/reconciliation/payments/{id}")) return path.replace("{id}", "{{payment_id}}");
  if (path.includes("{provider_payment_id}")) return path.replace("{provider_payment_id}", "{{provider_payment_id}}");
  if (path.includes("{provider_refund_id}")) return path.replace("{provider_refund_id}", "{{provider_refund_id}}");
  return path;
}

function postmanBody(body: string) {
  return body
    .replaceAll("ins_...", "{{installation_id}}")
    .replaceAll("pmo_...", "{{payment_option_id}}")
    .replaceAll("pay_...", "{{payment_id}}")
    .replaceAll("<injected-secret>", "{{provider_secret}}")
    .replaceAll("xnd_development_...", "{{xendit_api_key}}");
}

function postmanRequest(endpoint: Endpoint) {
  const root = endpoint.path.startsWith("/partner/v1/") ? "{{partner_base_url}}" : "{{base_url}}";
  const requestURL = endpoint.path.startsWith("http") ? endpoint.path : `${root}${postmanPath(endpoint.path)}`;
  const lines = [`${endpoint.method} ${requestURL}`];
  if (endpoint.path.startsWith("/api/v1/admin/")) {
    lines.push("X-Admin-API-Key: {{admin_api_key}}");
  } else if (endpoint.path.startsWith("/api/v1/")) {
    lines.push("Authorization: Bearer {{service_api_key}}", "X-Emisell-Merchant-ID: {{merchant_id}}");
  }
  if (endpoint.path.startsWith("/partner/v1/")) {
    lines.push("Authorization: Bearer {{connector_runner_token}}");
  }
  for (const header of endpoint.headers ?? []) {
    lines.push(header
      .replace("sandbox (optional)", "{{execution_mode}} (optional)")
      .replace("sandbox", "{{execution_mode}}")
      .replace("<xendit-callback-token>", "{{xendit_callback_token}}"));
  }
  if (endpoint.body) {
    const multipart = endpoint.headers?.some((header) => header.toLowerCase().startsWith("content-type: multipart/form-data"));
    if (multipart) lines.push("", postmanBody(endpoint.body));
    else lines.push("Content-Type: application/json", "", "Body · raw · JSON", postmanBody(endpoint.body));
  }
  return lines.join("\n");
}

function postmanExampleRequest(endpoint: Endpoint, example: EndpointExample) {
  return postmanRequest({
    method: endpoint.method,
    path: endpoint.path,
    title: example.title,
    description: example.description ?? endpoint.description,
    headers: example.headers ?? endpoint.headers,
    body: example.body,
    response: example.response,
    note: example.note,
  });
}

export default async function DocsPage({
  searchParams,
}: {
  searchParams: Promise<{ contract?: string }>;
}) {
  const query = await searchParams;
  const contractID: ContractID = query.contract === "partner" ? "partner" : query.contract === "admin" ? "admin" : "backend";
  const activeContract = contracts.find((item) => item.id === contractID) ?? contracts[0];
  const activeGroups = contractID === "partner" ? partnerGroups : contractID === "admin" ? adminGroups : backendGroups;
  const endpointCount = activeGroups.reduce((total, group) => total + group.endpoints.length, 0);
  await requireDashboardSession(`/docs?contract=${contractID}`);
  const baseURL = process.env.PAYMENT_PROXY_PUBLIC_URL ?? "http://localhost:18080";
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const contractVersion = "v1";
  const activeHeaders = contractID === "partner" ? partnerHeaders : contractID === "admin" ? adminHeaders : commonHeaders;
  const collectionVariables = contractID === "partner" ? `partner_base_url = http://127.0.0.1:18082
connector_runner_token = <CONNECTOR_RUNNER_TOKEN>
installation_id = ins_...
provider_payment_id = <ID dari provider>
provider_refund_id = <ID refund dari provider>
provider_secret = <secret injected only in isolated runtime>` : contractID === "admin" ? `base_url = ${baseURL}
admin_api_key = <isi dari .env ADMIN_API_KEY>
service_api_key = <secret epk_ untuk tenant operational routes>
merchant_id = merchant_postman_demo
service_api_key_id = <ID metadata sak_ untuk revoke>
delivery_id = <outbox delivery ID untuk replay>` : `base_url = ${baseURL}
service_api_key = <secret epk_ dari Payment Proxy>
merchant_id = merchant_123
execution_mode = sandbox
installation_id = ins_...
payment_option_id = pmo_...
payment_id = pay_...
xendit_api_key = <Xendit development Secret API Key>
xendit_webhook_token = <Xendit webhook verification token>
midtrans_server_key = <Midtrans Sandbox Server Key>`;
  return (
    <div className="dashboard-app docs-dashboard">
      <AppSidebar active="docs" healthy={healthy} engineStatus={health.status}/>

      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search API documentation..."/>

        <main className="dashboard-content docs-dashboard-content">
          <section className="dashboard-heading docs-dashboard-heading">
            <div>
              <p className="breadcrumb">Developers / API documentation</p>
              <h1>Payment API Contracts</h1>
              <p>Dokumentasi dipisah berdasarkan pemanggil: Emisell Backend, operator Payment Proxy, dan runtime connector memiliki kontrak yang berbeda.</p>
            </div>
            <a className="dashboard-primary-action" href="/postman/Emisell-Payment-Proxy.postman_collection.json" download>Download Postman <Icon name="arrow" size={16}/></a>
          </section>

          <section className="docs-contract-selector" aria-label="Pilih kontrak API">
            {contracts.map((contract) => (
              <a
                className={contract.id === contractID ? "docs-contract-card docs-contract-active" : "docs-contract-card"}
                href={`/docs?contract=${contract.id}`}
                key={contract.id}
              >
                <span>{contract.classification} · {contract.status}</span>
                <strong>{contract.label}</strong>
                <small>{contract.audience}</small>
              </a>
            ))}
          </section>

          <section className="docs-summary-panel">
            <div><span>STATUS</span><strong>{activeContract.status} · {contractVersion}</strong></div>
            <div><span>AUDIENCE</span><strong>{activeContract.audience}</strong></div>
            <div className="docs-base-url"><span>BASE PATH</span><code>{activeContract.basePath}</code></div>
            <div><span>ENDPOINT</span><strong>{endpointCount} documented</strong></div>
          </section>

          <section className="docs-contract-overview">
            <div>
              <p className="label">{activeContract.classification} CONTRACT</p>
              <h2>{activeContract.label}</h2>
              <p>{activeContract.description}</p>
            </div>
            <div className="docs-contract-flow" aria-label={`Alur ${activeContract.label}`}>
              {contractID === "backend" ? (
                <><span>Emisell Backend</span><i>→</i><strong>Payment Proxy</strong><i>→</i><span>Canonical response</span></>
              ) : contractID === "admin" ? (
                <><span>Operator</span><i>→</i><strong>Control Plane</strong><i>→</i><span>Runtime & operations</span></>
              ) : (
                <><span>Payment Kernel</span><i>→</i><strong>Connector SDK</strong><i>→</i><span>Provider API</span></>
              )}
            </div>
          </section>

          <div className="docs-layout">
            <aside className="docs-sidebar">
              <p className="label">CONTENTS</p>
              <a href="#engine-contract">Engine Contract</a>
              {contractID === "backend" && <a href="#quickstart">10-minute flow</a>}
              <a href="#authentication">Authentication</a>
              <a href="#postman">Postman Collection</a>
              {contractID === "backend" && <a href="#scope-decisions">Endpoint Decisions</a>}
              {activeGroups.map((group) => <a key={group.id} href={`#${group.id}`}>{group.title}</a>)}
              {contractID === "admin" && <a href="#conformance">Provider Conformance</a>}
              <a href="#errors">Error contract</a>
            </aside>

            <div className="docs-content">
              {contractID === "backend" && <section className="doc-section" id="quickstart">
                <p className="label">10-MINUTE SANDBOX FLOW</p>
                <h2>Satu jalur integrasi, tanpa endpoint per provider</h2>
                <p>Mulai dari sandbox. Emisell Backend memakai kontrak normalized yang sama untuk Xendit, Midtrans, dan provider berikutnya; perbedaan credential dan payload native tetap berada di connector.</p>
                <Code>{`1. GET  /providers
2. POST /provider-installations
3. PUT  /provider-installations/{id}/credentials
4. POST /provider-installations/{id}/activate
5. PUT  /payment-method-assignments → GET /payment-options
6. POST /payment-sessions (ulang request yang sama dengan Idempotency-Key yang sama)
7. GET  /payment-sessions/{id} → terima signed payment.updated
8. GET  /integration-readiness → READY`}</Code>
                <div className="callout">Selalu kirim Authorization, X-Emisell-Merchant-ID, dan X-Emisell-Execution-Mode. Mutasi payment wajib memakai Idempotency-Key yang stabil per operasi.</div>
                <div className="postman-card"><div><strong>AI-readable contract</strong><span>Gunakan kontrak ringkas ini untuk coding assistant. Isinya hanya API Emisell Backend dan guardrail keamanan, bukan endpoint admin atau secret provider.</span></div><a className="download-link" href="/docs/llms.txt">Open llms.txt <span>→</span></a></div>
              </section>}
              <section className="doc-section" id="engine-contract">
                <p className="label">EMISELL PAYMENT ENGINE</p>
                <h2>Kernel universal, connector milik provider</h2>
                <p>Payment Kernel hanya mengelola lifecycle canonical, idempotency, status, webhook inbox, dan outbox. URL, authentication, payload, channel, limit nominal, serta signature webhook provider berada di connector.</p>
                <Code>{`Emisell Backend / Checkout
        ↓
Unified API /api/v1/*
        ↓
Payment Kernel (Go)
        ↓
Remote Connector Registry
        ↓  private /partner/v1/*
Isolated Connector Runner
        ↓
Xendit · Midtrans · DOKU · Duitku`}</Code>
                <h3>Kontrak connector</h3>
                <Code>{`Manifest()
ValidatePaymentMethod()
ValidatePayment()
VerifyInstallation()
CreatePayment()
GetPayment()
CapturePayment()
CancelPayment()
CreateRefund()
GetRefund()
HandleWebhook()`}</Code>
                <div className="callout">Connector hanya boleh menawarkan operation yang tercantum pada manifest dan sudah lulus conformance. Penambahan provider tidak boleh menambah branching provider-specific pada API atau Payment Kernel.</div>
                <div className="postman-card">
                  <div>
                    <strong>Xendit · reference connector</strong>
                    <span>Create/get payment, sandbox simulation, installation verification, webhook normalization, dan create-refund asynchronous aktif. Refund tetap fail-closed per channel.</span>
                  </div>
                  <span className="version-pill">emisell-xendit-v1</span>
                </div>
                <div className="postman-card">
                  <div>
                    <strong>Midtrans · second connector</strong>
                    <span>QRIS, enam VA/Mandiri Bill, GoPay, ShopeePay, credential verification, dan webhook normalization tersedia. BCA, BNI, dan Permata VA sudah lulus sandbox end-to-end; channel lain tetap fail-closed sampai diaktifkan untuk merchant oleh Midtrans.</span>
                  </div>
                  <span className="version-pill">emisell-midtrans-v1.1.0</span>
                </div>
              </section>

              {contractID === "backend" && <section className="doc-section" id="scope-decisions">
                <p className="label">API SCOPE</p>
                <h2>Endpoint yang sengaja tidak dibuat</h2>
                <p>Kontrak backend dijaga kecil. Perbedaan provider, mode credential, runtime, dan webhook native diselesaikan di dalam Payment Proxy, bukan ditambahkan sebagai endpoint baru.</p>
                <div className="callout">Mencari Provider Apps, certification, observability, atau webhook delivery? Buka <a href="/docs?contract=admin">Admin Control Plane</a>; endpoint tersebut bukan kontrak Emisell Backend.</div>
                <div className="docs-scope-grid">
                  {omittedEndpointDecisions.map(([title, reason]) => (
                    <article key={title}>
                      <strong>{title}</strong>
                      <p>{reason}</p>
                    </article>
                  ))}
                </div>
              </section>}

              <section className="doc-section" id="authentication">
                <p className="label">AUTHENTICATION</p>
                <h2>{contractID === "backend" ? "Service-to-service headers" : contractID === "admin" ? "Operator authentication" : "Private runner authentication"}</h2>
                <p>{contractID === "backend"
                  ? "Endpoint merchant hanya dipanggil Emisell Backend. Service key dan credential provider tidak boleh dikirim ke checkout atau browser."
                  : contractID === "admin"
                    ? "Endpoint /api/v1/admin/* memakai admin key. Read model tenant untuk certification dan troubleshooting tetap memakai service key serta Merchant ID."
                    : "Partner API adalah southbound contract. Browser, checkout, dan Emisell Backend tidak memanggil connector vendor secara langsung."}</p>
                <Code>{activeHeaders.join("\n")}</Code>
                <div className="callout">{contractID === "backend"
                  ? "Setiap resource dibatasi oleh Merchant ID dan kontraknya mengikuti base path /api/v1. Mutation pembayaran wajib memakai Idempotency-Key yang sama ketika retry."
                  : contractID === "admin"
                    ? "Admin key tidak boleh dipakai sebagai service key merchant. Endpoint operasional tenant tetap mengikuti isolasi Merchant ID."
                    : "Payment Kernel menemukan manifest saat startup lalu memanggil Connector Runner memakai bearer token khusus service pada jaringan privat."}</div>
                {contractID === "backend" && <div className="callout warning">Response 429 berarti limit merchant tercapai; ikuti Retry-After. Response 503 API_BUSY berarti kapasitas concurrent request penuh.</div>}
              </section>

              <section className="doc-section" id="postman">
                <p className="label">POSTMAN</p>
                <h2>Collection dipisah per kontrak</h2>
                <p>Folder Backend hanya berisi integrasi yang diperlukan Main Service. Operasi platform berada di folder Admin, sedangkan folder Partner hanya untuk Connector Runner pada jaringan privat.</p>
                <div className="postman-card">
                  <div>
                    <strong>Emisell Payment Platform API v1</strong>
                    <span>3 contract folders · Backend, Admin, dan Connector</span>
                  </div>
                  <a className="download-link" href="/postman/Emisell-Payment-Proxy.postman_collection.json" download>Download <span>↓</span></a>
                </div>
                <h3>Collection variables</h3>
                <Code>{collectionVariables}</Code>
                <div className="callout warning">Simpan semua API key sebagai secret variable. Jangan export current value credential. Gunakan admin key hanya untuk control plane dan service key hanya untuk resource merchant.</div>
              </section>

              {activeGroups.map((group) => (
                <section className="doc-section" id={group.id} key={group.id}>
                  <p className="label">{group.title.toUpperCase()}</p>
                  <h2>{group.title}</h2>
                  <p>{group.summary}</p>
                  {contractID === "backend" && group.id === "webhooks" && (
                    <EmisellWebhookContractGuide />
                  )}
                  <div className="endpoint-list">
                    {group.endpoints.map((endpoint) => (
                      <details className="endpoint-accordion" key={`${endpoint.method}-${endpoint.path}`}>
                        <summary>
                          <span className="endpoint-summary-main"><Method value={endpoint.method}/><code>{endpoint.path}</code></span>
                          <span className="endpoint-summary-title">{endpoint.title}</span>
                          <span className="accordion-chevron"><Icon name="arrow" size={15}/></span>
                        </summary>
                        <div className="endpoint-accordion-body">
                          <p>{endpoint.description}</p>
                          {endpoint.examples?.length ? (
                            <div className="endpoint-example-variants">
                              {endpoint.examples.map((example) => (
                                <section className="endpoint-example-variant" key={example.title}>
                                  <div className="endpoint-example-heading">
                                    <span>VARIASI REQUEST</span>
                                    <h3>{example.title}</h3>
                                    {example.description && <p>{example.description}</p>}
                                  </div>
                                  <div className="api-example-grid">
                                    <section>
                                      <h4>Contoh request Postman</h4>
                                      <Code>{postmanExampleRequest(endpoint, example)}</Code>
                                    </section>
                                    {example.response && <section><h4>Contoh response</h4><Code>{example.response}</Code></section>}
                                  </div>
                                  {example.note && <div className="callout warning">{example.note}</div>}
                                </section>
                              ))}
                            </div>
                          ) : (
                            <>
                              <div className="api-example-grid">
                                <section>
                                  <h4>Contoh request Postman</h4>
                                  <Code>{postmanRequest(endpoint)}</Code>
                                </section>
                                {endpoint.response && <section><h4>Contoh response</h4><Code>{endpoint.response}</Code></section>}
                              </div>
                              {endpoint.note && <div className="callout warning">{endpoint.note}</div>}
                            </>
                          )}
                        </div>
                      </details>
                    ))}
                  </div>
                </section>
              ))}

              {contractID === "admin" && <section className="doc-section" id="conformance">
                <p className="label">SANDBOX CONFORMANCE</p>
                <h2>Provider end-to-end test</h2>
                <p>Gunakan tab Certification pada detail Xendit atau Midtrans untuk menjalankan create, retrieve, simulator/customer action, provider webhook, dan delivery ke Emisell Backend melalui Connector Runner terisolasi.</p>
                <h3>Run certification</h3>
                <Code>{`POST /api/v1/connector-certifications/run
{
  "installation_id": "{{installation_id}}",
  "payment_method_code": "qris"
}`}</Code>
                <h3>Configure Midtrans sandbox</h3>
                <Code>{`PUT /api/v1/provider-installations/{{midtrans_installation_id}}/credentials
{
  "credentials": {
    "server_key": "{{midtrans_server_key}}",
    "pop_id": "{{optional_midtrans_pop_id}}"
  },
  "payment_methods": []
}

Response 200
{
  "data": {
    "provider_code": "midtrans",
    "provider_version": "emisell-midtrans-v1.1.0",
    "status": "READY",
    "credential_metadata": {
      "configured_fields": [{ "code": "server_key", "configured": true }],
      "webhook_ready": true
    }
  }
}`}</Code>
                <h3>Resume setelah sandbox customer authorization</h3>
                <Code>{`POST /api/v1/connector-certifications/run
{
  "installation_id": "{{installation_id}}",
  "payment_method_code": "qris",
  "payment_id": "{{payment_id}}"
}`}</Code>
                <div className="callout warning">Gunakan development Secret API Key Xendit. Saat ini 15 dari 20 capability Xendit telah certified: QRIS, 8 VA, 5 e-wallet, dan card melalui hosted Payment Session. Card telah lulus jalur frictionless dan 3DS challenge. Danamon VA, Kredivo, Akulaku, Indodana, dan Jenius Pay tetap tertutup sampai channel provider tersedia. PAN/CVV hanya dimasukkan pada halaman Xendit dan tidak pernah dikirim ke Emisell.</div>
                <div className="callout warning">Midtrans sudah tersedia sebagai connector kedua. BCA, BNI, dan Permata VA telah lulus create, retrieve, simulator, webhook terminal settlement, dan delivery ke Emisell Backend. QRIS/GoPay pada merchant sandbox ini masih memerlukan PoP/channel provisioning; BRI VA, CIMB VA, dan ShopeePay ditolak Midtrans sebagai channel yang belum aktif. Connector tetap fail-closed dan tidak membuat payment palsu. Metode tetap DOCUMENTED sampai payment yang sama menghasilkan webhook terminal dan delivery Emisell. Cancel dan refund tetap ditutup pada manifest sampai sandbox evidence terpisah tersedia.</div>
              </section>}

              <section className="doc-section" id="errors">
                <p className="label">ERROR CONTRACT</p>
                <h2>Stable error envelope</h2>
                <Code>{`{
  "error": {
    "code": "INVALID_STATE",
    "message": "resource state does not allow this operation"
  }
}`}</Code>
                <div className="error-grid">
                  <span>400</span><p>Invalid tenant, execution mode, JSON, atau idempotency key.</p>
                  <span>401</span><p>Service credential atau webhook signature tidak valid.</p>
                  <span>404</span><p>Resource tidak ditemukan untuk tenant tersebut.</p>
                  <span>409</span><p>Conflict, invalid lifecycle state, stale version, atau idempotency conflict.</p>
                  <span>422</span><p>Business input tidak valid.</p>
                  <span>202</span><p>Mutation mempunyai hasil ambigu; resource menjadi UNKNOWN dan wajib disinkronkan menggunakan ID yang sama.</p>
                  <span>502</span><p>Payment engine menolak atau tidak dapat melayani request.</p>
                </div>
              </section>
            </div>
          </div>
          <footer className="docs-footer">{activeContract.label} · {activeContract.status} · Never expose service or provider credentials</footer>
        </main>
      </div>
    </div>
  );
}
