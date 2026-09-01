# Emisell Payment Engine architecture

> Dokumen kontrak yang mengikat implementasi berada di
> [Emisell Payment Engine — source of truth](emisell-payment-engine.md).
> Dokumen ini memberi penjelasan operasional yang lebih ringkas.

## Prinsip

Payment Kernel hanya mengenal domain universal. Ia tidak mengetahui Basic Auth Xendit, `BCA_VIRTUAL_ACCOUNT`, URL provider, atau bentuk webhook provider.

```go
type Connector interface {
    VerifyInstallation(ctx, input)
    CreatePayment(ctx, input)
    GetPayment(ctx, input)
    CapturePayment(ctx, input)
    CancelPayment(ctx, input)
    SimulatePayment(ctx, input)
    CreateRefund(ctx, input)
    GetRefund(ctx, input)
    HandleWebhook(ctx, input)
}
```

Method boleh mengembalikan `OPERATION_NOT_SUPPORTED`. Capability registry memastikan method yang belum disertifikasi tidak dapat di-assign oleh merchant.

## Ownership

| Domain | Owner |
|---|---|
| Installation lifecycle | Payment Kernel |
| Credential ciphertext | Emisell credential vault table |
| Provider authentication/payload | Connector |
| Canonical payment/refund | Payment Kernel |
| Provider transaction truth | Provider |
| Webhook verification | Connector |
| Webhook deduplication/inbox | Payment Kernel |
| Event delivery ke Emisell | Durable outbox worker |
| Master payment method | Emisell catalog |
| Provider channel mapping | Capability registry |

## Credential lifecycle

```text
Dashboard server
  → Payment API over authenticated request
  → connector verifies credential directly to provider
  → credential JSON encrypted AES-256-GCM
  → ciphertext stored per installation
  → plaintext buffers cleared
```

AAD adalah `provider-credential:{installation_id}` sehingga ciphertext tidak dapat dipindahkan ke installation lain. API hanya mengembalikan daftar field `configured`, waktu konfigurasi, dan flag webhook; tidak ada nilai atau fragmen secret.

## Installation state machine

```text
CONFIG_REQUIRED → VERIFYING → READY → ACTIVE
                       │        │        │
                       ▼        ▼        ▼
                     ERROR   INACTIVE ←──┘
                                │
                                ▼
                           UNINSTALLED
```

Uninstall hanya diizinkan setelah deactivate. Credential ciphertext dihapus saat uninstall; audit dan canonical transaction record tetap dipertahankan.

## Payment lifecycle

```text
CREATED → PROCESSING/PENDING → SUCCEEDED
                  ├──────────→ FAILED
                  ├──────────→ CANCELLED
                  ├──────────→ EXPIRED
                  └──────────→ UNKNOWN
```

`UNKNOWN` digunakan ketika mutation mungkin sudah diterima provider tetapi response tidak dapat dipastikan. Resource yang sama harus direkonsiliasi melalui provider payment ID. Automatic failover dilarang karena dapat membuat double charge.

## Routing sederhana seperti Shopify

Merchant memilih satu installation untuk setiap master payment method dan environment:

```text
qris / sandbox → Xendit sandbox installation
va_bca / live  → Xendit live installation
```

Checkout hanya menerima `payment_option_id`. Kernel me-resolve assignment, installation, capability, dan channel code. Tidak ada weighted routing atau provider retry pada tahap ini.

## Xendit connector v1

| Canonical method | Xendit channel | Status |
|---|---|---|
| `qris` | `QRIS` | `CERTIFIED` |
| `va_bca` | `BCA_VIRTUAL_ACCOUNT` | `CERTIFIED` |
| `va_mandiri`, `va_bni`, `va_bri`, `va_permata`, `va_cimb`, `va_bsi`, `va_muamalat` | provider VA channel terkait | `CERTIFIED` |
| `ewallet_ovo`, `ewallet_dana`, `ewallet_shopeepay`, `ewallet_linkaja`, `ewallet_astrapay` | provider wallet channel terkait | `CERTIFIED` |
| `card` | Payment Session `PAYMENT_LINK` + channel `CARDS` | `CERTIFIED` |
| Danamon VA, paylater, Jenius Pay | sesuai capability catalog | `DOCUMENTED` |

Connector membuat `POST /v3/payment_requests`, mengambil `GET /v3/payment_requests/{id}`, dan menggunakan `POST /v3/payment_requests/{id}/simulate` hanya untuk channel sandbox yang didukung simulator. E-wallet memakai customer redirect/mobile authorization; certification melanjutkan payment yang sama setelah aksi pelanggan. Card menggunakan `POST /sessions` dan `GET /sessions/{id}` dengan hosted payment link Xendit. Emisell hanya menyimpan URL redirect dan identifier sesi; PAN, expiry, serta CVV tidak pernah masuk ke API, log, database, atau dashboard Emisell. Amount IDR menggunakan Rupiah utuh dan diteruskan ke provider tanpa konversi skala.

## Webhook

```text
Xendit
  → /webhooks/v1/providers/xendit/{installation_id}
  → verify x-callback-token
  → deduplicate (source, webhook-id)
  → encrypt raw payload
  → canonical status transition
  → durable outbox
  → signed event to Emisell Backend
```

Ada dua kontrak yang tidak boleh dicampur:

1. **Provider ingress**: Xendit mengirim payload provider ke Payment Proxy. Payload mentah diverifikasi, dienkripsi, lalu dinormalisasi.
2. **Emisell Backend delivery**: Payment Proxy mengirim event canonical ke endpoint milik Emisell Backend, default production `POST /webhooks/v1/payment-proxy`. Payload provider mentah dan detail connector tidak diteruskan.

Setiap event keluar mempunyai satu ID `evt_*` yang sama pada body, `X-Emisell-Webhook-ID`, dan `Idempotency-Key`. Envelope memuat `api_version=2026-08-28`, merchant, resource canonical, previous status, dan snapshot payment/refund. Event delivery memakai HMAC-SHA256 `v1=hex(HMAC(secret, timestamp + "." + raw_body))`. Emisell Backend wajib memverifikasi signature dan timestamp terhadap raw body, memastikan identitas header/body sama, menyimpan event secara durable, lalu deduplicate berdasarkan event ID.

Callback URL outbound diisi manual dari dashboard. Payment Proxy menghasilkan
secret `whsec_` 256-bit, menyimpannya terenkripsi AES-GCM, dan hanya menampilkan
plaintext satu kali. Rotasi menonaktifkan delivery sampai receiver Emisell
Backend diperbarui. Worker me-resolve konfigurasi database setiap poll sehingga
perubahan URL, secret, atau enabled state tidak memerlukan restart.

Response `2xx` berarti event telah tersimpan durable. HTTP `408`, `425`, `429`, `5xx`, dan network error di-retry dengan exponential backoff; HTTP `3xx` serta `4xx` lain langsung menjadi `DEAD` karena payload atau konfigurasi perlu diperbaiki sebelum replay operator.

## Data model

| Table | Fungsi |
|---|---|
| `providers` | Registry provider dan dynamic credential schema |
| `provider_versions` | Version/release connector |
| `provider_installations` | Tenant installation lifecycle |
| `provider_credentials` | Ciphertext per installation |
| `payment_methods` | Master payment method |
| `provider_payment_method_capabilities` | Universal-to-provider channel mapping |
| `payment_method_assignments` | Pilihan gateway merchant |
| `payment_sessions` | Canonical payment/idempotency ledger |
| `payment_status_history` | Durable status timeline |
| `webhook_inbox` | Deduplicated encrypted provider events |
| `outbox_events` | At-least-once events ke Emisell |
| `connector_certification_runs` | Evidence capability sandbox |

Nama kolom database lama seperti `engine_payment_id` dipertahankan sementara untuk migrasi tanpa kehilangan data. Kontrak API menampilkannya sebagai `provider_payment_id`.

## Extension provider

Provider baru harus:

1. mengimplementasikan `connector.Connector` dan manifest capability;
2. lulus validasi `registry.Registry`, lalu didaftarkan hanya di composition root Connector Runner;
3. menambahkan credential schema tanpa secret default;
4. mengisi capability mapping dan source URL;
5. lulus test unit, contract, sandbox simulation, webhook, idempotency, dan timeout ambiguity;
6. baru dipromosikan dari `DOCUMENTED` menjadi `CERTIFIED`.

API dan Payment Kernel tidak boleh menambahkan branching `if provider == ...`.
Aturan mapping, amount, operation support, dan profil sandbox adalah milik
connector manifest.

Semua connector berjalan out-of-process di Connector Runner melalui namespace
private `/partner/v1/*`. Payment Kernel menemukan manifest saat startup dan tidak
perlu berubah ketika implementasi provider baru ditambahkan ke runner.
