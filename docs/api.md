# Emisell Payment Platform API Contracts v1

## Provider Apps (admin control plane)

Connector baru dimulai dengan membuat identitas provider melalui
`POST /internal/v1/provider-app-providers`. Setelah itu versi ZIP dikirim ke
`POST /internal/v1/provider-app-providers/{providerCode}/versions`, kemudian
melewati lifecycle `UPLOADED → VALIDATED → CERTIFIED → PUBLISHED`.
Request menggunakan `multipart/form-data` dengan field file `bundle` dan header
`X-Admin-API-Key`. Publish ditolak dengan `CONNECTOR_RUNTIME_NOT_READY` sampai
isolated runtime memuat provider code dan exact manifest version yang sama.

Kontrak bundle, contoh request Postman, contoh response, dan security gate
lengkap tersedia di [`docs/provider-apps.md`](provider-apps.md).

Dokumentasi dibagi berdasarkan pemanggil dan arah komunikasi. Kontrak tidak boleh dicampur walaupun seluruhnya berada di platform yang sama.

| Contract | Arah | Base path | Status |
|---|---|---|---|
| Emisell Internal Gateway | Emisell Backend → Payment Proxy | `/api/v1/*` | Active |
| Emisell status webhook | Payment Proxy → Emisell Backend | callback URL milik Emisell | Active |
| Provider webhook ingress | Provider → Payment Proxy | `/webhooks/v1/providers/{provider}/{installation_id}` | Active |
| Partner API Contract | Payment Proxy → isolated Connector Runner | `/partner/v1/*` | Active; private service contract |

Detail payload Xendit, Midtrans, DOKU, atau Duitku tidak boleh bocor ke Emisell Backend atau checkout. Dashboard `/docs` dan collection Postman memakai pembagian kontrak yang sama.

## Emisell Internal Gateway

Base path: `/api/v1`. API ini adalah kontrak canonical antara Emisell Backend dan Payment Proxy.

## Authentication dan headers

| Header | Required | Fungsi |
|---|---:|---|
| `Authorization: Bearer <SERVICE_API_KEY>` | semua `/api/v1/*` | service authentication |
| `X-Emisell-Merchant-ID` | semua `/api/v1/*` | tenant isolation |
| `X-Emisell-API-Version: 2026-08-28` | recommended | pin kontrak; versi tidak didukung menghasilkan `406` |
| `X-Emisell-Actor` | optional | actor audit; default `emisell-backend` |
| `X-Emisell-Execution-Mode` | create/assignment/certification | `sandbox` atau `live` |
| `Idempotency-Key` | payment/cancel/refund/reconciliation/replay | ID logical operation, 8–128 karakter |

Error selalu stabil:

```json
{"error":{"code":"INVALID_STATE","message":"resource state does not allow this operation"}}
```

Semua response membawa `X-Emisell-API-Version`, `X-Request-ID`, dan
`Cache-Control: no-store`. Jika traffic guard aktif, response juga membawa
`X-RateLimit-Limit` dan `X-RateLimit-Remaining`. Ikuti `Retry-After` pada
`429 RATE_LIMIT_EXCEEDED` atau `503 API_BUSY`; mutation hanya boleh diulang
dengan `Idempotency-Key` yang sama.

## Engine capability contract

`GET /api/v1/engine/capabilities` adalah sumber machine-readable untuk runtime
yang benar-benar berjalan. Emisell Backend dapat memeriksa versi connector,
operation, field credential, dan certification profile sebelum mengaktifkan
fitur baru.

```http
GET /api/v1/engine/capabilities
Authorization: Bearer <service-key>
X-Emisell-Merchant-ID: merchant_123
X-Emisell-API-Version: 2026-08-28
```

```json
{
  "data": {
    "engine": "emisell_payment_engine",
    "contract_version": "2026-08-28",
    "connector_contract": "v1",
    "selection_mode": "merchant_assignment",
    "unknown_policy": "reconcile_same_provider",
    "connectors": [
      {
        "code": "xendit",
        "version": "emisell-xendit-v1",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "create_payment", "get_payment", "simulate_payment", "handle_webhook"]
      },
      {
        "code": "midtrans",
        "version": "emisell-midtrans-v1.1.0",
        "runtime": "isolated_container",
        "operations": ["verify_installation", "create_payment", "get_payment", "handle_webhook"]
      }
    ]
  }
}
```

Katalog database menentukan payment method yang boleh tampil di checkout.
Manifest capability menentukan operation yang mampu dijalankan binary. Keduanya
harus siap; capability endpoint tidak menggantikan assignment merchant.

## Emisell Backend service API keys

Menu Dashboard **API Keys** menerbitkan Bearer credential untuk Main Service
Emisell. Key hasil generate mempunyai scope `gateway:full` dan dapat memanggil
seluruh endpoint `/api/v1/*` dengan merchant context yang valid.

| Method | Endpoint admin | Fungsi |
|---|---|---|
| `GET` | `/internal/v1/service-api-keys` | List metadata key aktif dan revoked |
| `POST` | `/internal/v1/service-api-keys` | Generate random 256-bit key; plaintext tampil satu kali |
| `POST` | `/internal/v1/service-api-keys/{id}/revoke` | Revoke key tanpa restart API |

Endpoint pengelolaan memakai `X-Admin-API-Key`. Secret berprefix `epk_`,
database hanya menyimpan SHA-256 hash, sedangkan response list hanya memuat
fingerprint tersamarkan.

```http
POST /internal/v1/service-api-keys
X-Admin-API-Key: <admin-key>
X-Emisell-Actor: dashboard:operator
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

## Engine observability

Endpoint observability memakai `X-Admin-API-Key` dan tidak boleh diekspos ke
checkout atau browser publik:

```http
GET /internal/v1/observability
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

`GET /internal/v1/metrics` mengembalikan format Prometheus. Counter merupakan
snapshot process sejak startup dan harus di-scrape ke storage terpusat untuk
agregasi lintas replica dan histori jangka panjang.

`GET /internal/v1/engine/readiness` menggabungkan pemeriksaan database,
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
| Validate payment mapping/input | `POST /partner/v1/payment-methods/validate`, `POST /partner/v1/payments/validate` |
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

### `GET /providers`

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
  "provider_version": "emisell-midtrans-v1.1.0",
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
  "provider_version": "emisell-xendit-v1",
  "environment": "sandbox"
}
```

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
    "provider_version": "emisell-xendit-v1",
    "status": "CONFIG_REQUIRED",
    "credential_metadata": {},
    "payment_methods": [],
    "version": 1
  }
}
```

`merchant_id` selalu berasal dari header terautentikasi `X-Emisell-Merchant-ID`, bukan dari body. Payment Proxy tidak menyimpan nama merchant atau store. Hanya satu installation non-uninstalled per Merchant ID, provider, dan environment. `public_webhook_url` dibuat server dari domain publik Payment Proxy dan dikonsumsi Emisell Backend untuk ditampilkan pada alur koneksi di Dashboard Emisell; client tidak boleh menyusun URL sendiri.

Service API key untuk komunikasi Emisell Backend tetap dikelola terpisah melalui `/internal/v1/service-api-keys`. Installation hanya menyimpan credential provider di vault terenkripsi dan tidak pernah menyalin atau mengembalikan service API key tersebut.

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
      "webhook_ready": true
    },
    "version": 4
  }
}
```

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
  "provider_version": "emisell-midtrans-v1.1.0"
}
```

Upgrade bersifat eksplisit dan hanya menerima installation `INACTIVE`. Target
harus sudah `RELEASED` oleh Provider App Registry dan harus sama dengan versi
runtime yang sedang dimuat. Response berubah menjadi `READY`; caller kemudian
melakukan activate memakai `version` terbaru. Credential tetap berada di vault.

### `DELETE /provider-installations/{id}`

Installation aktif harus dideaktivasi dahulu. Uninstall menghapus credential ciphertext dan mempertahankan transaksi serta audit.

## Connector certification

### `GET /connector-certifications?provider=xendit&limit=25`

Mengembalikan bukti certification run per merchant.

### `POST /connector-certifications/run`

Wajib `X-Emisell-Execution-Mode: sandbox`.

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

Run `PASSED` otomatis mempromosikan capability `DOCUMENTED` menjadi `CERTIFIED` dan menulis audit evidence. Kelulusan wajib memiliki webhook provider terminal `SUCCEEDED` serta delivery outbox Emisell dengan status canonical yang sama; webhook `PENDING` tidak cukup walaupun GET Status provider sudah menunjukkan sukses. QRIS serta delapan Virtual Account memakai simulator provider. Lima e-wallet dan card memakai flow dua tahap: run pertama membuat payment dan mengembalikan `BLOCKED` dengan `payment_id`; selesaikan redirect/mobile authorization provider, lalu kirim ulang endpoint yang sama dengan payment tersebut:

```json
{
  "installation_id": "ins_01k3...",
  "payment_method_code": "card",
  "payment_id": "pay_01k3..."
}
```

Resume tidak membuat payment baru. Status provider harus `SUCCEEDED`, webhook payment yang sama harus sudah diproses, dan signed outbox event harus berstatus `DELIVERED` sebelum capability dipromosikan.

## Master payment methods dan assignment

### `GET /payment-methods`

Mengembalikan katalog canonical serta matriks `DOCUMENTED`, `CERTIFIED`, atau `DISABLED` untuk setiap provider.

### `GET /payment-method-assignments`

List assignment termasuk yang inactive. Header execution mode opsional.

### `PUT /payment-method-assignments`

Installation harus `ACTIVE` pada environment yang sama.

```json
{
  "installation_id": "ins_01k3...",
  "payment_method_code": "qris",
  "label": "QRIS",
  "version": 0
}
```

```json
{
  "data": {
    "id": "pmo_01k3...",
    "environment": "sandbox",
    "payment_method_code": "qris",
    "installation_id": "ins_01k3...",
    "provider_code": "xendit",
    "label": "QRIS",
    "status": "ACTIVE",
    "version": 1
  }
}
```

`version=0` membuat assignment. Update wajib memakai version terakhir.

### `POST /payment-method-assignments/{id}/deactivate`

```json
{"version":1}
```

### `GET /payment-options`

Wajib execution mode. Checkout hanya menerima opaque option ID:

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

## Payments

Semua amount adalah integer minor unit. Untuk IDR, `1000000` berarti Rp10.000. Xendit QRIS menerima Rp1–Rp10.000.000; seluruh VA yang diaktifkan menerima Rp10.000–Rp50.000.000; card menerima Rp5.000–Rp200.000.000.

### `POST /payment-sessions`

Headers tambahan:

```text
X-Emisell-Execution-Mode: sandbox
Idempotency-Key: order-2026-0001-attempt-1
```

```json
{
  "payment_option_id": "pmo_01k3...",
  "merchant_reference": "order_2026_0001",
  "amount": 1000000,
  "currency": "IDR",
  "description": "Order #2026-0001",
  "customer": {"name":"Budi","email":"budi@example.com"},
  "metadata": {"order_id":"2026-0001"}
}
```

QRIS response:

```json
{
  "data": {
    "payment": {
      "id": "pay_01k3...",
      "provider_code": "xendit",
      "status": "PENDING",
      "provider_payment_id": "pr-8877c08a-...",
      "execution_engine": "emisell_native",
      "next_action": {
        "type": "qr_code_information",
        "raw_qr_data": "00020101..."
      }
    }
  }
}
```

Setiap VA memakai `payment_option_id` canonical masing-masing; `next_action.type` bernilai `virtual_account_information` dan `display_text` berisi nomor VA. E-wallet mengembalikan `redirect` atau `mobile_authorization`.

Card menggunakan payment option canonical `card`. Request tetap sama dan wajib mempunyai `return_url` HTTPS. Response mengembalikan `next_action.type=redirect` menuju hosted checkout Xendit. Client membuka URL tersebut dan memasukkan data kartu langsung pada domain Xendit. Jangan pernah menambahkan `card_number`, expiry, CVV/CVN, atau data autentikasi kartu ke request Payment Proxy.

Contoh response card:

```json
{
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
}
```

Request dengan idempotency key dan body sama mengembalikan payment lama. Key sama dengan body berbeda menghasilkan `409 IDEMPOTENCY_CONFLICT`. Transport timeout mutation menghasilkan `202 OUTCOME_UNKNOWN`; jangan membuat payment pengganti sampai reconciliation selesai.

### `GET /payment-sessions`

Query: `status`, `provider`, `q`, `limit` (1–100), dan `offset` (0–10.000). Execution mode opsional.

### `GET /payment-sessions/{id}`

Membaca projection lokal dan menyinkronkan resource yang sama langsung ke provider bila `provider_payment_id` tersedia.

### `GET /payment-sessions/{id}/timeline`

Mengembalikan status history, source, details, dan timestamp.

### `POST /payment-sessions/{id}/cancel`

```json
{"reason":"requested_by_customer"}
```

Wajib idempotency key. Connector Xendit native v1 saat ini mengembalikan `CANCEL_NOT_SUPPORTED` sampai conformance cancel per channel lulus.

## Refunds

### `POST /refunds`

```json
{"payment_id":"pay_01k3...","amount":50000,"reason":"requested_by_customer"}
```

Kontrak sudah universal. Refund hanya diteruskan apabila connector runtime
mengiklankan operation `create_refund` pada manifest. Xendit native v1 belum
mengiklankannya, sehingga API mengembalikan `REFUND_NOT_SUPPORTED` sebelum
credential dibuka atau request provider dibuat.

### `GET /refunds/{id}`

Mengambil canonical refund dan menyinkronkan ke provider bila connector mendukungnya.

## Provider webhook ingress

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
| `GET` | `/internal/v1/emisell-webhook` | Membaca URL, status, masked secret, dan hasil test terakhir |
| `PUT` | `/internal/v1/emisell-webhook` | Menyimpan Callback URL manual dan status aktif |
| `POST` | `/internal/v1/emisell-webhook/secret` | Generate/rotate secret `whsec_`; plaintext tampil satu kali |
| `POST` | `/internal/v1/emisell-webhook/test` | Mengirim event canonical `webhook.test` tanpa data pembayaran |

Contoh generate:

```http
POST /internal/v1/emisell-webhook/secret
X-Admin-API-Key: <admin-key>
X-Emisell-Actor: dashboard:operator
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
}
```

Signature dihitung dari byte raw body tanpa parse/re-serialize:

```text
v1=hex(HMAC-SHA256(EMISELL_BACKEND_WEBHOOK_SECRET, unix_timestamp + "." + raw_body))
```

Receiver wajib menolak timestamp di luar toleransi, signature salah, versi tidak dikenal, serta perbedaan ID/type/merchant antara header dan body. Simpan event dan event ID secara atomik sebelum merespons `2xx`. Event duplikat dengan body identik tetap dibalas `2xx`; ID yang sama dengan body berbeda harus ditolak.

## Webhook operations API

### `GET /webhook-inbox`

Query: `status`, `q`, `limit`, `offset`. Raw provider payload tidak dikembalikan.

### `GET /webhook-deliveries`

List outbox menuju Emisell Backend beserta attempt dan status.

### `POST /webhook-deliveries/{id}/replay`

Hanya delivery `DEAD`; wajib `Idempotency-Key`.

## Reconciliation

### `GET /reconciliation/cases`

Menggabungkan payment/refund `UNKNOWN`, delivery `DEAD`, webhook `FAILED`, dan installation `ERROR`.

### `POST /reconciliation/payments/{id}/resolve`

Wajib `Idempotency-Key`. Endpoint mengambil provider payment ID yang sama, memperbarui status canonical, dan tidak melakukan failover.

## Health

- `GET /health/live`: proses API hidup.
- `GET /health/ready`: PostgreSQL dan registry connector Emisell siap.

```json
{"status":"ready","checks":{"database":"ok","emisell_engine":"ok"}}
```

## Postman

- Dashboard: `http://localhost:13000/docs`
- Download: `http://localhost:13000/postman/Emisell-Payment-Proxy.postman_collection.json`

Collection tidak menyimpan API key. Isi `service_api_key`, `merchant_id`,
`api_contract_version`, dan `xendit_api_key` sebagai current values lokal sebelum
menjalankan request.
