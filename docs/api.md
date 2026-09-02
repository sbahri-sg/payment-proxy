# Emisell Payment Platform API Contracts v1

## Mulai dari audience yang benar

Emisell Backend hanya mengintegrasikan kontrak canonical berikut:

| Domain | Endpoint inti |
|---|---|
| Provider catalog | `GET /api/v1/providers?q=<keyword>` |
| Payment-method discovery | `GET /api/v1/payment-methods?q=<keyword>` |
| Merchant connection | `POST/GET /api/v1/provider-installations`, `GET/DELETE /api/v1/provider-installations/{id}` |
| Credential | `PUT/PATCH /api/v1/provider-installations/{id}/credentials` |
| Lifecycle | `POST .../{id}/activate`, `POST .../{id}/deactivate` |
| Payment | `POST /api/v1/payment-sessions`, `GET /api/v1/payment-sessions/{id}`, `POST .../{id}/cancel` |
| Integration readiness | `GET /api/v1/integration-readiness` |
| Refund | `POST /api/v1/refunds`, `GET /api/v1/refunds/{id}` |
| Status event | satu receiver canonical milik Emisell Backend |

Endpoint observability, Provider App release/verification, service API key, sandbox diagnostics,
payment timeline, webhook inbox/delivery, reconciliation, health, dan Partner
API bukan kewajiban integrasi Emisell Backend. Semuanya merupakan control
plane, operasi internal, infrastructure probe, atau southbound connector
contract.

Decision matrix lengkap tersedia di
[`docs/backend-api-scope.md`](backend-api-scope.md).

Endpoint yang sengaja **tidak dibuat**:

- tidak ada endpoint verify terpisah; konfigurasi credential langsung verify;
- tidak ada endpoint switch Sandbox/Live; keduanya connection slot terpisah;
- tidak ada endpoint `/xendit/*` atau `/midtrans/*`;
- tidak ada endpoint merchant untuk memilih container/runtime;
- tidak ada endpoint untuk membaca kembali secret;
- tidak ada create-payment endpoint per metode;
- refund adalah kontrak inti bersyarat; hanya original payment channel dengan
  return-to-source policy dan released connector `create_refund` yang dapat
  menjalankannya. Capture tetap belum masuk kontrak inti.

## Contract map

Dokumentasi dibagi berdasarkan pemanggil dan arah komunikasi. Kontrak tidak
boleh dicampur walaupun seluruhnya berada di platform yang sama.

| Contract | Arah | Base path | Status |
|---|---|---|---|
| Emisell Backend API | Emisell Backend → Payment Proxy | `/api/v1/*` | Active; kontrak integrasi utama |
| Admin control plane | Dashboard/operator → Payment Proxy | `/api/v1/admin/*` dan operational routes | Internal only |
| Emisell status webhook | Payment Proxy → Emisell Backend | callback URL milik Emisell | Active |
| Provider webhook ingress | Provider → Payment Proxy | `/webhooks/v1/providers/{provider}/{installation_id}` | Internal implementation detail |
| Partner API Contract | Payment Proxy → isolated Connector Runner | `/partner/v1/*` | Private service contract |

Detail payload Xendit, Midtrans, DOKU, Duitku, atau iPaymu tidak boleh bocor ke
Emisell Backend atau checkout. Dashboard `/docs` dan collection Postman memakai
pembagian audience yang sama.

## Provider Apps (admin control plane; bukan Backend API)

Connector baru dimulai dengan membuat identitas provider melalui
`POST /api/v1/admin/provider-app-providers`. Setelah itu versi ZIP dikirim ke
`POST /api/v1/admin/provider-app-providers/{providerCode}/versions`, kemudian
melewati lifecycle storage `UPLOADED → VALIDATED → CERTIFIED → PUBLISHED`.
Status `CERTIFIED` dihasilkan verifikasi backend otomatis dan ditampilkan
sebagai `VERIFIED` di dashboard. Pemeriksaan mencocokkan bundle dengan shared
runtime exact version, digest, operations, credential schema, automated test
profile, dan canonical method mapping; tidak memakai credential merchant.
Request menggunakan `multipart/form-data` dengan field file `bundle` dan header
`X-Admin-API-Key`. Publish ditolak dengan `CONNECTOR_RUNTIME_NOT_READY` sampai
isolated runtime memuat provider code dan exact manifest version yang sama.

Kontrak bundle, contoh request Postman, contoh response, dan security gate
lengkap tersedia di [`docs/provider-apps.md`](provider-apps.md).

## Emisell Backend API

Base path: `/api/v1`. API ini adalah kontrak canonical antara Emisell Backend dan Payment Proxy.

## Authentication dan headers

| Header | Required | Fungsi |
|---|---:|---|
| `Authorization: Bearer <SERVICE_API_KEY>` | endpoint merchant/service | service authentication |
| `X-Emisell-Merchant-ID` | endpoint merchant/service | tenant isolation |
| `X-Emisell-Execution-Mode` | readiness, diagnostics, dan filter installation | `sandbox` atau `live`; tidak dipakai oleh Payment Methods atau Payment Sessions |
| `Idempotency-Key` | create/cancel payment dan create refund | ID logical operation, 8–128 karakter |

Error selalu stabil:

```json
{"error":{"code":"INVALID_STATE","message":"resource state does not allow this operation"}}
```

Hosted checkout memakai `409 HOSTED_PAYMENT_METHOD_RESTRICTION_UNSUPPORTED`
bila API provider tidak dapat membatasi halaman pembayaran ke assignment
`ACTIVE` secara exact. Client dapat menawarkan direct checkout provider yang
bersangkutan; jangan mengulang hosted request tanpa mengubah konfigurasi.

Semua response membawa `X-Request-ID` dan `Cache-Control: no-store`. Jika
traffic guard aktif, response juga membawa
`X-RateLimit-Limit` dan `X-RateLimit-Remaining`. Ikuti `Retry-After` pada
`429 RATE_LIMIT_EXCEEDED` atau `503 API_BUSY`; mutation hanya boleh diulang
dengan `Idempotency-Key` yang sama.

Kontrak ringkas untuk coding assistant tersedia sebagai plain text di
`GET /docs/llms.txt`. File tersebut sengaja hanya memuat kontrak Emisell
Backend dan guardrail keamanan; endpoint admin, Partner API, serta secret
provider tidak dimasukkan sebagai instruksi integrasi.

## Merchant integration readiness

`GET /api/v1/integration-readiness` menilai kesiapan per Merchant ID dan
`X-Emisell-Execution-Mode`. Status `READY` hanya diberikan setelah platform
melihat bukti connection aktif, payment method aktif, create payment, replay
idempotency, status lookup, webhook backend yang aktif, dan delivery webhook
HTTP 2xx. Checklist ini berbeda dari connector certification milik operator.

```http
GET /api/v1/integration-readiness
Authorization: Bearer <service-key>
X-Emisell-Merchant-ID: merchant_123
X-Emisell-Execution-Mode: sandbox
```

Sandbox dan Live dinilai secara terpisah. `resilience_evidence=true` berarti
platform pernah memproses `late_payment` atau
`provider_delayed_confirmation`; indikator tersebut informatif dan tidak
memblokir status `READY`.

## Runtime diagnostic (operator/internal)

`GET /api/v1/engine/capabilities` adalah sumber machine-readable untuk runtime
yang benar-benar berjalan. Endpoint ini dipakai Payment Kernel, deployment
check, dan operator; Emisell Backend tidak perlu memanggilnya. Katalog yang
dibutuhkan Dashboard Merchant sudah tersedia melalui `GET /api/v1/providers`.

```http
GET /api/v1/engine/capabilities
Authorization: Bearer <service-key>
X-Emisell-Merchant-ID: merchant_123
```

```json
{
  "data": {
    "engine": "emisell_payment_engine",
    "contract_version": "2026-08-28",
    "connector_contract": "v1",
    "selection_mode": "merchant_installation",
    "unknown_policy": "reconcile_same_provider",
    "connectors": [
      {
        "code": "xendit",
        "version": "emisell-xendit-v2.0.2",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "create_payment", "create_hosted_checkout", "get_payment", "simulate_payment", "handle_webhook"]
      },
      {
        "code": "midtrans",
        "version": "emisell-midtrans-v2.0.3",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "create_payment", "create_hosted_checkout", "get_payment", "handle_webhook"]
      }
    ]
  }
}
```

Katalog database menentukan payment method yang boleh tampil di checkout.
Manifest capability menentukan operation yang mampu dijalankan binary. Keduanya
harus siap; capability endpoint tidak menggantikan assignment merchant.

## Admin: Emisell Backend service API keys

Menu Dashboard **API Keys** menerbitkan Bearer credential untuk Main Service
Emisell. Key hasil generate mempunyai scope `gateway:full` dan dapat memanggil
seluruh endpoint `/api/v1/*` dengan merchant context yang valid.

| Method | Endpoint admin | Fungsi |
|---|---|---|
| `GET` | `/api/v1/admin/service-api-keys` | List metadata key aktif dan revoked |
| `POST` | `/api/v1/admin/service-api-keys` | Generate random 256-bit key; plaintext tampil satu kali |
| `POST` | `/api/v1/admin/service-api-keys/{id}/revoke` | Revoke key tanpa restart API |

Endpoint pengelolaan memakai `X-Admin-API-Key`. Secret berprefix `epk_`,
database hanya menyimpan SHA-256 hash, sedangkan response list hanya memuat
fingerprint tersamarkan.

```http
POST /api/v1/admin/service-api-keys
X-Admin-API-Key: <admin-key>
Content-Type: application/json

{"name":"Emisell Backend Production"}
```

```json
{
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
}
```

Pasang `secret` pada secret manager Emisell Backend dan kirim sebagai
`Authorization: Bearer`. Rotasi aman dilakukan dengan generate key baru,
memindahkan consumer, lalu revoke key lama. `SERVICE_API_KEY` environment tetap
diterima sebagai bootstrap fallback deployment lama.

## Admin: Engine observability

Endpoint observability memakai `X-Admin-API-Key` dan tidak boleh diekspos ke
checkout atau browser publik:

```http
GET /api/v1/admin/observability
X-Admin-API-Key: <admin-key>
```

```json
{
  "data": {
    "requests_total": 10000,
    "latency": {"average_ms":18.42,"p95_ms":50},
    "connector_outcomes": {"unknown_outcome":0,"not_supported":0,"rejected":2},
    "provider_webhooks": {"accepted":120,"duplicate":3,"invalid":0},
    "slo": {
      "status":"MEETING",
      "availability_target_percent":99.9,
      "availability_percent":99.99,
      "latency_p95_target_ms":500,
      "latency_p95_ms":50
    }
  }
}
```

`GET /api/v1/admin/metrics` mengembalikan format Prometheus. Counter merupakan
snapshot process sejak startup dan harus di-scrape ke storage terpusat untuk
agregasi lintas replica dan histori jangka panjang.

`GET /api/v1/admin/provider-availability` menjadi sumber menu **Payment
status** pada dashboard admin. Endpoint menggabungkan sumber resmi
`status.xendit.co`, `midtrans.com/id/status`, `duitku.statuspage.io`,
`status.doku.com`, dan `status.ipaymu.com` dengan
status per koneksi merchant/provider/environment. Response resmi dinormalisasi
menjadi `AVAILABLE`, `DEGRADED`, `UNAVAILABLE`, atau `UNKNOWN`, serta memuat
component, incident aktif, scheduled maintenance, dan riwayat incident yang
relevan dengan payment Indonesia. URL sumber bersifat hardcoded/allowlisted;
konten remote tidak pernah dijalankan sebagai HTML.

DOKU dibaca dari API publik PagerDuty Status Dashboard dan difilter ke service
payment/checkout. iPaymu dibaca dari health-check publik dan hanya memasukkan
record bertipe `Payment Channel`; service `Withdraw` tidak memengaruhi status
checkout. Maintenance terjadwal ditampilkan sejak diumumkan, tetapi baru
memengaruhi checkout ketika jendela maintenance dimulai. Outage provider-wide
menahan provider; outage atau maintenance aktif yang dapat dipetakan menahan
hanya payment method terkait. Restriction resmi digabungkan ke cache
availability installation tanpa mengubah assignment merchant. Jika sumber
resmi tidak dapat diakses, status menjadi `UNKNOWN` dan bersifat fail-open;
availability probe bercredential tetap melindungi setiap koneksi merchant.

API menjalankan refresh sumber status resmi di background segera setelah
startup lalu setiap 60 detik secara default. Interval dan batas waktu dapat
diatur dengan `PROVIDER_STATUS_POLL_INTERVAL_SECONDS` dan
`PROVIDER_STATUS_FETCH_TIMEOUT_SECONDS`. Worker hanya membaca lima endpoint
publik tersebut—tidak melakukan probe kredensial massal per merchant. Setiap
replica memakai cache lokalnya sendiri, jitter kecil untuk menghindari burst
bersamaan, timeout, dan backoff ketika seluruh sumber gagal. Cache gagal dibaca
tetap fail-open sehingga gangguan monitoring tidak mematikan checkout.

Probe internal tetap memuat reason code tenant-scoped seperti
`PROVIDER_MAINTENANCE`, `PROVIDER_TIMEOUT`, `CHANNEL_INACTIVE`, dan
`CREDENTIAL_INVALID`. Bukti yang melewati TTL ditampilkan sebagai `UNKNOWN`,
bukan outage aktif. Status resmi dan probe internal tidak mengubah assignment
merchant; keduanya merupakan control-plane evidence untuk checkout protection.

`GET /api/v1/admin/engine/readiness` menggabungkan pemeriksaan database,
connector registry, runtime configuration, serta nilai request timeout, body
limit, rate limit, dan max in-flight efektif. Endpoint ini aman untuk automation
admin, tetapi tetap tidak boleh diekspos sebagai endpoint publik.

## Partner API Contract

Partner API adalah kontrak private antara Payment Kernel dan Connector Runner.
Xendit dan Midtrans tidak di-import atau dibuat di process API; kernel menemukan manifest
runner saat startup dan hanya berbicara melalui interface canonical.

| Operation kernel | HTTP contract aktif |
|---|---|
| Health/capability discovery | `GET /partner/v1/health`, `GET /partner/v1/capabilities` |
| Validate payment mapping/input | `POST /partner/v1/payment-methods/validate`, `POST /partner/v1/hosted-payment-methods/validate`, `POST /partner/v1/payments/validate` |
| Verify/disable installation | `POST /partner/v1/installations/verify`, `POST /partner/v1/installations/disable` |
| Create/get/capture/cancel payment | `POST /partner/v1/payments/{create|get|capture|cancel}` |
| Sandbox simulation | `POST /partner/v1/payments/simulate` |
| Create/get refund | `POST /partner/v1/refunds/{create|get}` |
| HandleWebhook | `POST /partner/v1/webhooks/normalize` |

Semua route memakai `Authorization: Bearer <CONNECTOR_RUNNER_TOKEN>`, maksimum
body 1 MiB, JSON strict, dan response `Cache-Control: no-store`. Endpoint hanya
boleh tersedia di jaringan private; production memakai HTTPS dan token acak dari
secret manager. Credential installation tidak dicatat dan tidak dikembalikan
dalam response runner.

Mutation tetap membawa `idempotency_key` di body canonical. Kegagalan transport
setelah mutation mungkin diterima runner dipetakan menjadi `OUTCOME_UNKNOWN`;
kernel tidak boleh membuat transaksi kedua atau failover ke provider lain.

Contoh request dan response setiap endpoint tersedia di Dashboard
`/docs?contract=partner` dan folder `02 · Partner API Contract` pada Postman.

## Provider registry

### `GET /providers?q=<keyword>`

`q` bersifat opsional, case-insensitive, maksimum 128 karakter, dan mencari
provider `code` atau `name`. Tanpa `q`, endpoint mengembalikan seluruh
katalog. Dashboard Emisell sebaiknya memakai debounce 300–500 ms dan hanya
mengirim keyword yang sudah di-trim serta di-URL-encode.

```json
{
  "data": [
    {
      "code": "xendit",
      "name": "Xendit",
      "available": true,
      "connector_code": "xendit",
      "credential_schema": [
        {"code":"api_key","label":"Secret API key","secret":true,"required":true},
        {"code":"webhook_verification_token","label":"Payments API v3 webhook token","secret":true,"required":true}
      ],
      "environments": ["sandbox", "live"]
    },
    {
      "code": "midtrans",
      "name": "Midtrans",
      "available": true,
      "connector_code": "midtrans",
      "credential_schema": [
        {"code":"server_key","label":"Server key","secret":true,"required":true},
        {"code":"pop_id","label":"PoP ID (Core API)","secret":true,"required":false}
      ],
      "environments": ["sandbox", "live"]
    }
  ]
}
```

Contoh installation Midtrans memakai endpoint yang sama:

```json
{
  "provider_code": "midtrans",
  "provider_version": "emisell-midtrans-v2.0.3",
  "environment": "sandbox"
}
```

Credential dikonfigurasi tanpa Client Key karena connector server-to-server
memakai Server Key sebagai Basic Auth username. `pop_id` bersifat opsional dan
hanya diisi jika diterbitkan Midtrans untuk Core API merchant tersebut:

```http
PUT /api/v1/provider-installations/{id}/credentials
```

```json
{
  "credentials": {
    "server_key":"<Midtrans sandbox Server Key>",
    "pop_id":"<optional Midtrans PoP ID>"
  },
  "payment_methods": []
}
```

## Provider installations

### `POST /provider-installations`

```json
{
  "provider_code": "xendit",
  "environment": "sandbox"
}
```

`provider_version` opsional. Jika tidak dikirim, Payment Proxy memilih exact
version yang sedang dimuat runtime. Emisell Backend sebaiknya memakai default
ini agar tidak meng-hardcode versi connector.

```json
{
  "data": {
    "id": "ins_01k3...",
    "merchant_id": "merchant_123",
    "provider_code": "xendit",
    "provider_name": "Xendit",
    "environment": "sandbox",
    "public_webhook_url": "https://payments.example.com/webhooks/v1/providers/xendit/ins_01k3...",
    "execution_engine": "emisell_native",
    "provider_version": "emisell-xendit-v2.0.2",
    "status": "CONFIG_REQUIRED",
    "credential_metadata": {},
    "payment_methods": [],
    "version": 1
  }
}
```

`merchant_id` selalu berasal dari header terautentikasi `X-Emisell-Merchant-ID`, bukan dari body. Payment Proxy tidak menyimpan nama merchant atau store. Hanya satu installation non-uninstalled per Merchant ID, provider, dan environment. `public_webhook_url` dibuat server dari domain publik Payment Proxy dan dikonsumsi Emisell Backend untuk ditampilkan pada alur koneksi di Dashboard Emisell; client tidak boleh menyusun URL sendiri.

Service API key untuk komunikasi Emisell Backend tetap dikelola terpisah melalui `/api/v1/admin/service-api-keys`. Installation hanya menyimpan credential provider di vault terenkripsi dan tidak pernah menyalin atau mengembalikan service API key tersebut.

### `GET /provider-installations`

List installation tenant. `X-Emisell-Execution-Mode` opsional sebagai filter.

### `GET /provider-installations/{id}`

Mengembalikan installation tenant-scoped. Response hanya memuat status/configured fields; API key dan secret provider tidak pernah dikembalikan.

Dashboard **Emisell** adalah pemilik alur konfigurasi seller: memilih provider,
memasukkan credential, dan menampilkan callback URL. Dashboard internal Payment
Proxy hanya menampilkan status operasional ingress tanpa panduan setup seller.
Xendit menggunakan setup manual satu kali yang dipandu Dashboard Emisell,
sedangkan Midtrans menerima URL yang sama secara otomatis melalui
`X-Override-Notification` pada setiap create payment. Notification URL Midtrans
di dashboard provider hanya berfungsi sebagai fallback opsional.

### `PUT /provider-installations/{id}/credentials`

```json
{
  "credentials": {
    "api_key": "xnd_development_REDACTED",
    "webhook_verification_token": "token-from-provider-dashboard"
  },
  "payment_methods": []
}
```

Engine memverifikasi API key langsung ke Xendit, mengenkripsi credential dengan AES-256-GCM, dan menyimpan ciphertext per installation. Xendit Dashboard harus mengarahkan topik **Payments API v3 – Payment Status**, **Payment Request Status**, **Payment Session Completed**, dan **Payment Session Expired** ke:

```text
POST /webhooks/v1/providers/xendit/{installation_id}
```

Contoh response:

```json
{
  "data": {
    "id": "ins_01k3...",
    "connector_id": "xendit:ins_01k3...",
    "execution_engine": "emisell_native",
    "status": "READY",
    "credential_metadata": {
      "configured_fields": [
        {"code":"api_key","configured":true},
        {"code":"webhook_verification_token","configured":true}
      ],
      "webhook_ready": true,
      "verification_required": false,
      "verification": {
        "id": "iver_01k3...",
        "result": "PASSED",
        "provider_version": "emisell-xendit-v2.0.2",
        "manifest_digest": "sha256-digest-without-prefix",
        "verified_at": "2026-09-01T03:00:00Z"
      }
    },
    "version": 4
  }
}
```

Connector wajib mengembalikan environment yang terdeteksi dari credential.
Payment Proxy menolak `CREDENTIAL_MODE_MISMATCH` jika, misalnya, Xendit test
key atau Midtrans Sandbox Server Key dikirim ke installation Live. Dashboard
merchant tidak menebak mode berdasarkan label; hasil verifikasi connector
menjadi sumber kebenaran.

Credential yang tidak memiliki penanda mode yang dikenali menghasilkan
`422 INVALID_PROVIDER_CREDENTIAL`; key valid tetapi dipasang pada slot yang
salah menghasilkan `422 CREDENTIAL_MODE_MISMATCH`.

### `PATCH /provider-installations/{id}/credentials`

Merotasi sebagian credential tanpa pernah membaca secret lama ke browser.
Installation `ACTIVE` harus dideaktivasi terlebih dahulu. Field yang tidak
dikirim dipertahankan dari vault, kemudian seluruh gabungan credential
diverifikasi ulang oleh connector sebelum ciphertext diganti.

```json
{
  "credentials": {
    "api_key": "xnd_development_ROTATED_REDACTED"
  }
}
```

Field opsional dapat dihapus secara eksplisit:

```json
{
  "credentials": {},
  "clear_fields": ["pop_id"]
}
```

Secret lama, secret baru, maupun fragmennya tidak pernah dikembalikan. Response
hanya memuat `configured_fields`, waktu verifikasi, dan
`verified_environment`.

### Merchant Sandbox/Live switch

Switch adalah pemilih dua credential slot terpisah, bukan operasi yang
mengubah satu installation dari Sandbox menjadi Live:

```text
Provider
├── Sandbox installation → Sandbox credential
└── Live installation    → Production credential
```

Dashboard merchant mengambil keduanya melalui `GET /provider-installations`,
menampilkan slot yang dipilih, dan membuat installation baru dengan
`POST /provider-installations` bila slot tersebut belum tersedia. Credential
tetap diverifikasi terhadap mode slot sebelum dapat diaktifkan.

### `POST /provider-installations/{id}/activate`

```json
{"version":4}
```

Mengubah `READY` atau `INACTIVE` menjadi `ACTIVE`.

### `POST /provider-installations/{id}/deactivate`

```json
{"version":5}
```

Mengubah `ACTIVE` menjadi `INACTIVE` tanpa menghapus credential.

### `POST /provider-installations/{id}/upgrade`

```json
{
  "version": 6,
  "provider_version": "emisell-midtrans-v2.0.3"
}
```

Upgrade bersifat eksplisit dan hanya menerima installation `INACTIVE`. Target
harus sudah `RELEASED` oleh Provider App Registry dan tersedia pada shared
runtime. Response berubah menjadi `CONFIG_REQUIRED`, connector binding lama
dilepas, dan credential tetap berada di vault. Caller harus menjalankan kembali
configure/verify pada versi baru hingga `READY`, baru kemudian boleh activate.

### `DELETE /provider-installations/{id}`

Installation aktif harus dideaktivasi dahulu. Uninstall menghapus credential ciphertext dan mempertahankan transaksi serta audit.

## Internal CI/operator: Sandbox diagnostics

### `GET /admin/connector-certifications?provider=xendit&limit=25`

Mengembalikan bukti diagnostic run pada tenant pengujian. Endpoint ini bukan
kontrak Emisell Backend dan tidak menjadi tahap installation merchant.

### `POST /admin/connector-certifications/run`

Wajib admin authentication, `X-Emisell-Merchant-ID`, dan
`X-Emisell-Execution-Mode: sandbox`.

```json
{
  "installation_id": "ins_01k3...",
  "payment_method_code": "qris"
}
```

```json
{
  "data": {
    "id": "cert_01k3...",
    "provider_code": "xendit",
    "payment_method_code": "qris",
    "status": "PASSED",
    "checks": [
      {"code":"credential_vault","status":"PASSED"},
      {"code":"payment_create","status":"PASSED"},
      {"code":"next_action","status":"PASSED"},
      {"code":"payment_retrieve","status":"PASSED"},
      {"code":"sandbox_simulation","status":"PASSED"},
      {"code":"webhook_delivery","status":"PASSED"},
      {"code":"emisell_delivery","status":"PASSED"}
    ],
    "payment_id": "pay_01k3..."
  }
}
```

Fasilitas legacy ini dipertahankan untuk CI/diagnostik sandbox dan audit evidence.
Kelulusan membutuhkan webhook provider terminal `SUCCEEDED` serta delivery
outbox Emisell dengan status canonical yang sama. QRIS dan Virtual Account dapat
memakai simulator provider; flow redirect/mobile dilanjutkan memakai
`payment_id` yang sama:

```json
{
  "installation_id": "ins_01k3...",
  "payment_method_code": "card",
  "payment_id": "pay_01k3..."
}
```

Resume tidak membuat payment baru. Status provider harus `SUCCEEDED`, webhook payment yang sama harus sudah diproses, dan signed outbox event harus berstatus `DELIVERED` sebelum capability dipromosikan.

## Master payment methods dan assignment

### `GET /payment-methods?q=<keyword>`

Mengembalikan katalog canonical serta matriks `DOCUMENTED`, `CERTIFIED`, atau
`DISABLED` untuk setiap provider. `q` bersifat opsional, case-insensitive,
maksimum 128 karakter, dan mencari `code`, `name`, `category`, serta
`description`. Contoh `?q=bca` mengembalikan metode BCA yang cocok; tanpa `q`,
seluruh katalog aktif dikembalikan. Gunakan debounce 300–500 ms di Dashboard
Emisell agar request tidak dikirim pada setiap keypress.

### `GET /payment-method-assignments`

List semua assignment milik `X-Emisell-Merchant-ID`, termasuk status `ACTIVE`
dan `INACTIVE` serta environment `sandbox` dan `live`. Endpoint ini tidak
menerima filter `environment`; gunakan field `environment` pada setiap item
untuk membedakan konfigurasinya. `/payment-options` dan `/provider-options`
juga mengembalikan kedua environment dalam satu respons.

Assignment adalah konfigurasi pilihan merchant, bukan status operasional
provider. Ketika provider atau channel sedang offline/maintenance, record
assignment tetap `ACTIVE`; Payment Proxy menyembunyikannya hanya dari endpoint
checkout sampai pemeriksaan provider berikutnya menyatakan tersedia kembali.

### `PUT /payment-method-assignments`

Installation harus `ACTIVE` pada environment yang sama. Capability berstatus
`DOCUMENTED` maupun `CERTIFIED` dapat di-assign; hanya `DISABLED` yang ditolak.
`DOCUMENTED` berarti mapping didukung connector tetapi belum mempunyai bukti
sandbox lengkap. Environment diambil dari `installation_id`; header
`X-Emisell-Execution-Mode` tidak digunakan. Request menerima 1–50 item dan
diproses secara atomic: jika satu item gagal, seluruh batch dibatalkan.

```json
{
  "assignments": [
    {
      "installation_id": "ins_01k3...",
      "payment_method_code": "qris",
      "version": 0
    },
    {
      "installation_id": "ins_01k3...",
      "payment_method_code": "va_bca",
      "version": 0
    }
  ]
}
```

```json
{
  "data": [{
      "id": "pmo_01k3...",
      "environment": "sandbox",
      "payment_method_code": "qris",
      "installation_id": "ins_01k3...",
      "provider_code": "xendit",
      "label": "QRIS",
      "status": "ACTIVE",
      "version": 1
  }]
}
```

`label` tidak dikirim. Payment Proxy selalu memakai `name` dari master catalog
sebagai label checkout. `version=0` membuat assignment; update wajib memakai
version terakhir. Payment method yang tidak disertakan tidak otomatis dihapus
atau dinonaktifkan. Payload single-object lama masih diterima selama masa
kompatibilitas, tetapi integrasi baru wajib memakai `assignments` array.

### `POST /payment-method-assignments/{id}/deactivate`

```json
{"version":1}
```

### `GET /payment-options`

Endpoint ini tidak menerima filter environment dan mengembalikan seluruh option
`ACTIVE` milik merchant dari sandbox dan live. Setiap item membawa field
`environment`; saat option dipilih, Payment Proxy menurunkan environment dari
`payment_option_id`. Endpoint ini hanya dipakai oleh flow `direct` lanjutan yang
memilih channel sebelum memanggil provider. Flow `provider_hosted` mengambil
`payment_method_id` dari `/provider-options`; Payment Proxy lalu menyelesaikan
installation dan menerapkan tepat satu metode tersebut pada checkout provider.

Sebelum mengembalikan daftar, Payment Proxy memperbarui cache availability
internal berumur pendek. Method yang dinyatakan `offline`, `maintenance`, atau
inactive oleh provider tidak dikembalikan. Status dan alasan availability tidak
ditambahkan ke response merchant karena checkout cukup menerima pilihan yang
aman digunakan.

```json
{
  "data": [{
    "id": "pmo_01k3...",
    "environment": "sandbox",
    "payment_method_code": "qris",
    "category": "QR_CODE",
    "label": "QRIS"
  }]
}
```

### `GET /provider-options`

Endpoint ini tidak menerima filter environment dan mengembalikan sandbox serta
live milik merchant dalam satu respons. Data dikelompokkan berdasarkan
installation provider aktif dan setiap item membawa field `environment`.
Gunakan endpoint ini ketika Dashboard Emisell ingin menampilkan pilihan gateway
lebih dahulu, lalu metode yang tersedia pada gateway tersebut. Pilih
`supported_payment_methods[].id` sebagai `payment_method_id` untuk membuat
`provider_hosted` payment session.

```json
{
  "data": [{
    "provider_code": "xendit",
    "provider_name": "Xendit",
    "installation_id": "ins_01k3...",
    "provider_version": "emisell-xendit-v2.0.2",
    "environment": "sandbox",
    "logo_url": "/brands/providers/xendit.svg",
    "supported_payment_methods": [{
      "id": "pmo_01k3...",
      "payment_method_code": "qris",
      "category": "QR_CODE",
      "label": "QRIS",
      "logo_url": "/brands/payment-methods/qris.png"
    }]
  }]
}
```

`logo_url` adalah path relatif terhadap origin Dashboard Payment Proxy. Field
tersebut tidak dikirim jika provider atau payment method belum mempunyai aset
logo. Provider tanpa assignment aktif tidak muncul dan data selalu dibatasi
oleh `X-Emisell-Merchant-ID`. Provider yang gagal health probe atau tidak lagi
mempunyai method tersedia juga tidak muncul selama cache outage masih berlaku.
Pemeriksaan ini tidak mengubah installation maupun assignment merchant.

## Payments

Semua amount IDR adalah integer Rupiah utuh. Nilai `10000` berarti tepat Rp10.000. Payment Proxy menyimpan dan meneruskan nilai yang sama ke provider; tidak ada konversi `/100`.

### `POST /payment-sessions`

Headers tambahan:

```text
Idempotency-Key: order-2026-0001-attempt-1
```

Payment Proxy melakukan preflight availability yang sama sebelum membuat
payment. Provider yang sedang tidak tersedia menghasilkan
`503 PAYMENT_PROVIDER_TEMPORARILY_UNAVAILABLE`; direct method yang sedang
maintenance menghasilkan `503 PAYMENT_METHOD_TEMPORARILY_UNAVAILABLE`. Tidak
ada automatic failover ke provider lain karena hal tersebut dapat menciptakan
transaksi ganda.

Environment dan installation diambil dari `payment_method_id` untuk hosted
checkout atau dari `payment_option_id` untuk direct-channel. Header
`X-Emisell-Execution-Mode` tidak digunakan.

```json
{
  "payment_method_id": "pmo_01k3...",
  "checkout_mode": "provider_hosted",
  "merchant_reference": "order_2026_0001",
  "amount": 10000,
  "currency": "IDR",
  "description": "Order #2026-0001",
  "customer": {"name":"Budi","email":"budi@example.com"},
  "return_url": "https://shop.example/payments/return",
  "metadata": {"order_id":"2026-0001"}
}
```

`payment_method_id` adalah ID assignment opaque (`pmo_...`) dari
`provider-options[].supported_payment_methods[]`, bukan ID katalog global dan
bukan `installation_id`. Jika `checkout_mode` dihilangkan, request dengan field
ini otomatis menjadi `provider_hosted`. Payment Proxy memvalidasi bahwa
assignment, installation, provider, dan channel masih ACTIVE/tersedia, lalu
mengirim tepat satu metode pilihan itu ke runtime. Request lama yang mengirim
`installation_id` tidak lagi diterima.

`metadata` harus berupa JSON object maksimum 32 KiB. Nilai tersebut disimpan
pada payment ledger, dikembalikan tanpa bergantung pada provider, dan diteruskan
pada event `payment.updated` agar Emisell Backend dapat menghubungkan transaksi
dengan order internalnya.

Xendit, Midtrans, DOKU, dan Duitku dapat membatasi halaman hosted ke metode
pilihan secara exact. iPaymu Redirect Payment belum menyediakan allowlist exact
per transaksi, sehingga release saat ini hanya mengiklankan direct checkout
iPaymu:

```json
{
  "data": {
    "payment": {
      "id": "pay_01k3...",
      "installation_id": "ins_01k3...",
      "payment_method_id": "pmo_01k3...",
      "payment_method_code": "qris",
      "provider_code": "xendit",
      "checkout_mode": "provider_hosted",
      "status": "PENDING",
      "provider_payment_id": "ps-6a915387...",
      "execution_engine": "emisell_native",
      "checkout_url": "https://dev.xen.to/...",
      "metadata": {"order_id":"2026-0001"},
      "next_action": {
        "type": "redirect",
        "redirect_url": "https://dev.xen.to/..."
      }
    }
  }
}
```

Untuk Xendit, URL berasal dari `payment_link_url` Payment Session; Midtrans dari
`redirect_url` Snap; DOKU dari `response.payment.url`; dan Duitku dari
`paymentUrl`. Xendit menerima `allowed_payment_channels`, Midtrans menerima
`enabled_payments`, DOKU menerima `payment.payment_method_types`, dan Duitku
menerima satu `paymentMethod`. Nilainya dibentuk dari assignment `ACTIVE` yang
dipilih, sehingga metode lain tidak muncul pada Payment Link. Otorisasi tetap
diproses pada halaman milik provider; Emisell tidak membuat halaman provider
checkout dan tidak menerima PAN, expiry,
CVV/CVN, OTP, atau detail autentikasi pembayaran.

Flow `direct` lama tetap tersedia dengan `checkout_mode: direct` dan
`payment_option_id` untuk kebutuhan channel tertentu. Jangan mencampur
`payment_option_id` atau `payment_method_code` dengan `provider_hosted`.

Request dengan idempotency key dan body sama mengembalikan payment lama. Key sama dengan body berbeda menghasilkan `409 IDEMPOTENCY_CONFLICT`. Transport timeout mutation menghasilkan `202 OUTCOME_UNKNOWN`; jangan membuat payment pengganti sampai reconciliation selesai.

Setiap payment response mempunyai `flags` array. `late_payment` ditambahkan
ketika provider mengonfirmasi `SUCCEEDED` setelah status canonical sempat
`EXPIRED`; `provider_delayed_confirmation` ditambahkan ketika konfirmasi sukses
datang setelah `UNKNOWN`. Flag ikut dikirim pada event `payment.updated` agar
Emisell Backend dapat menjalankan kebijakan order secara eksplisit tanpa
mengubah arti status canonical.

### `GET /payment-sessions`

Query: `environment` (`sandbox` atau `live`), `status`, `provider`, `q`,
`limit` (1–100), dan `offset` (0–10.000). Filter `environment` bersifat
opsional; tanpa filter, kedua environment dikembalikan.

Endpoint Emisell Backend ini selalu tenant-scoped oleh
`X-Emisell-Merchant-ID`. Dashboard operator menggunakan endpoint Control Plane
`GET /api/v1/admin/payment-sessions` agar dapat melihat seluruh merchant, dengan
filter tambahan `merchant_id`. Pemisahan ini mencegah dashboard admin keliru
menampilkan hanya tenant demo tanpa membuka akses lintas tenant pada API merchant.

### `GET /payment-sessions/{id}`

Membaca projection lokal dan menyinkronkan resource yang sama langsung ke provider bila `provider_payment_id` tersedia.

### `GET /payment-sessions/{id}/timeline`

Mengembalikan status history, source, details, dan timestamp.

### `POST /payment-sessions/{id}/cancel`

```json
{"reason":"requested_by_customer"}
```

Wajib idempotency key. Connector Xendit native v1 saat ini mengembalikan `CANCEL_NOT_SUPPORTED` sampai conformance cancel per channel lulus.

## Conditional capability: Refunds

### `POST /refunds`

```json
{"payment_id":"pay_01k3...","amount":5000,"reason":"REQUESTED_BY_CUSTOMER"}
```

Kontrak sudah universal dan non-custodial. Payment Proxy selalu menggunakan
provider, environment, installation, credential, dan provider payment ID dari
payment asal. Tujuan dana tidak dapat diberikan client. Reason dinormalisasi ke
`REQUESTED_BY_CUSTOMER`, `CANCELLATION`, `DUPLICATE`, `FRAUDULENT`, atau
`OTHERS`.

Eksekusi memerlukan dua gate sekaligus:

1. metadata payment channel mempunyai `refund.supported=true` dan
   `return_to_original_source=true`;
2. exact provider runtime version mengiklankan operation `create_refund`.

Implementasi Unified Refund Xendit untuk QRIS full refund sudah tersedia dan
diuji pada contract test lokal, tetapi exact release belum mengiklankan
`create_refund`. Gate tetap tertutup sampai ada bukti transaksi sandbox nyata,
verified final webhook, duplicate callback, dan failure-mode test. Karena itu
request saat ini tetap menghasilkan `REFUND_NOT_SUPPORTED` sebelum credential
dibuka. Partial refund QRIS dan channel Xendit lain juga fail-closed.

### `GET /refunds/{id}`

Mengambil canonical refund. Xendit menyelesaikan Unified Refund melalui
`refund.succeeded` atau `refund.failed`; API tidak mengarang endpoint lookup
provider yang tidak terdokumentasi. Event final dikirim ke Emisell Backend
sebagai `refund.updated`.

Uninstall installation ditolak selama terdapat refund non-terminal atau payment
yang masih berada pada documented refund window. Merchant harus memakai
deactivate agar checkout baru berhenti tetapi credential tetap tersedia untuk
memenuhi kewajiban refund.

## Provider → Payment Proxy webhook ingress

### `POST /webhooks/v1/providers/xendit/{installation_id}`

Endpoint ini tidak memakai bearer service key. Xendit wajib mengirim `x-callback-token` dan `webhook-id`.

```http
POST /webhooks/v1/providers/xendit/ins_01k3...
x-callback-token: <provider-callback-token>
webhook-id: webhook-unique-id
Content-Type: application/json
```

```json
{
  "event": "payment.capture",
  "data": {"payment_request_id":"pr-8877c08a-...","status":"SUCCEEDED"}
}
```

```json
{"accepted":true}
```

Token diverifikasi dengan credential installation, `webhook-id` dideduplicate, raw payload dienkripsi, status canonical diperbarui, lalu outbox durable dibuat untuk Emisell Backend.

Untuk card, event authoritative adalah `payment_session.completed` atau `payment_session.expired`. `data.payment_session_id`/`data.id` dikorelasikan ke `provider_payment_id` berawalan `ps-`.

## Emisell Backend webhook receiver

### Konfigurasi outbound dari dashboard

Callback URL selalu diisi manual oleh operator, sedangkan webhook secret dibuat
oleh Payment Proxy. Plaintext secret hanya tampil satu kali saat generate atau
rotate dan setelah itu hanya masked hint yang dapat dibaca kembali.

Endpoint admin menggunakan `X-Admin-API-Key`:

| Method | Endpoint | Fungsi |
|---|---|---|
| `GET` | `/api/v1/admin/emisell-webhook` | Membaca URL, status, masked secret, dan hasil test terakhir |
| `PUT` | `/api/v1/admin/emisell-webhook` | Menyimpan Callback URL manual dan status aktif |
| `POST` | `/api/v1/admin/emisell-webhook/secret` | Generate/rotate secret `whsec_`; plaintext tampil satu kali |
| `POST` | `/api/v1/admin/emisell-webhook/test` | Mengirim event canonical `webhook.test` tanpa data pembayaran |

Contoh generate:

```http
POST /api/v1/admin/emisell-webhook/secret
X-Admin-API-Key: <admin-key>
```

```json
{
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
}
```

Rotasi bersifat fail-closed: delivery otomatis nonaktif sampai secret baru
dipasang pada receiver Emisell Backend, connection test berhasil, dan operator
mengaktifkan delivery kembali. Worker membaca konfigurasi database terbaru pada
poll berikutnya tanpa rebuild atau restart container.

Emisell Backend menyediakan endpoint berikut. Endpoint ini bukan bagian dari Payment Proxy API:

```http
POST https://api.emisell.com/webhooks/v1/payment-proxy
Content-Type: application/json
X-Emisell-Webhook-ID: evt_01k3...
X-Emisell-Webhook-Timestamp: 1787907600
X-Emisell-Webhook-Signature: v1=<hex-hmac-sha256>
X-Emisell-Webhook-Version: 1
X-Emisell-Event-Type: payment.updated
X-Emisell-Merchant-ID: merchant_123
Idempotency-Key: evt_01k3...
```

```json
{
  "id": "evt_01k3...",
  "object": "event",
  "api_version": "2026-08-28",
  "type": "payment.updated",
  "created_at": "2026-08-28T09:30:00Z",
  "merchant_id": "merchant_123",
  "resource": {"type":"payment","id":"pay_01k3..."},
  "metadata": {"order_id":"order_2026_0001"},
  "data": {
    "payment": {
      "id": "pay_01k3...",
      "payment_method_code": "qris",
      "merchant_reference": "order_2026_0001",
      "amount": 1000000,
      "currency": "IDR",
      "environment": "sandbox",
      "status": "SUCCEEDED",
      "updated_at": "2026-08-28T09:30:00Z"
    },
    "previous_status": "PENDING"
  }
}
```

Signature dihitung dari byte raw body tanpa parse/re-serialize:

```text
v1=hex(HMAC-SHA256(EMISELL_BACKEND_WEBHOOK_SECRET, unix_timestamp + "." + raw_body))
```

Receiver wajib menolak timestamp di luar toleransi, signature salah, versi tidak dikenal, serta perbedaan ID/type/merchant antara header dan body. Simpan event dan event ID secara atomik sebelum merespons `2xx`. Event duplikat dengan body identik tetap dibalas `2xx`; ID yang sama dengan body berbeda harus ditolak.

## Admin/operator: Webhook operations API

### `GET /webhook-inbox`

Query: `status`, `q`, `limit`, `offset`. Raw provider payload tidak dikembalikan.

### `GET /webhook-deliveries`

List outbox menuju Emisell Backend beserta attempt dan status.

### `POST /webhook-deliveries/{id}/replay`

Hanya delivery `DEAD`; wajib `Idempotency-Key`.

## Admin/operator: Reconciliation

### `GET /reconciliation/cases`

Menggabungkan payment/refund `UNKNOWN`, delivery `DEAD`, webhook `FAILED`, dan installation `ERROR`.

### `POST /reconciliation/payments/{id}/resolve`

Wajib `Idempotency-Key`. Endpoint mengambil provider payment ID yang sama, memperbarui status canonical, dan tidak melakukan failover.

## Infrastructure: Health

- `GET /health/live`: proses API hidup.
- `GET /health/ready`: PostgreSQL dan registry connector Emisell siap.

```json
{"status":"ready","checks":{"database":"ok","emisell_engine":"ok"}}
```

## Postman

- Dashboard: `http://localhost:13000/docs`
- Download: `http://localhost:13000/postman/Emisell-Payment-Proxy.postman_collection.json`

Collection tidak menyimpan API key. Isi `service_api_key`, `merchant_id`, dan
`xendit_api_key` sebagai current values lokal sebelum menjalankan request.
