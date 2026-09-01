# Emisell Payment Engine — source of truth

Status: **ACTIVE FOUNDATION**

Owner: **Emisell**

Runtime: **Go Payment Kernel + isolated Connector Runner**
Reference connectors: **Xendit `emisell-xendit-v2.0.2` + Midtrans `emisell-midtrans-v2.0.1` + Duitku `emisell-duitku-v2.0.1` + DOKU `emisell-doku-v2.0.1` + iPaymu `emisell-ipaymu-v2.0.1`**

Dokumen ini mengunci batas arsitektur Payment Proxy. Implementasi, endpoint,
dashboard, dan connector baru tidak boleh menyimpang dari kontrak di bawah tanpa
perubahan dokumen dan review eksplisit.

## 1. Tujuan

Emisell Payment Engine adalah orchestration layer pembayaran milik Emisell,
bukan pengganti ledger provider dan bukan fork Hyperswitch. Modelnya sengaja
sederhana seperti Shopify: merchant memilih satu gateway aktif untuk setiap
master payment method dan environment.

```text
Emisell Checkout / Backend
          │
          ▼
  Unified API /api/v1/*
          │
          ▼
  Payment Kernel (Go)
   lifecycle · idempotency · canonical state · webhook inbox · outbox
          │
          ▼
 Remote Connector Registry
          │ private /partner/v1/*
          ▼
 Isolated Connector Runner
          │
     ┌────┼──────────┬─────────┬────────┐
     ▼    ▼          ▼         ▼        ▼
  Xendit Midtrans   DOKU     Duitku   iPaymu
```

Kernel tidak mengetahui URL, authentication, channel code, request payload,
response payload, signature webhook, atau limit nominal provider.

## 2. Batas domain

| Domain | Pemilik | Aturan |
|---|---|---|
| Unified API dan tenant authorization | Kernel | Hanya kontrak canonical |
| Installation dan assignment | Kernel | Terpisah per merchant dan environment |
| Idempotency dan canonical payment state | Kernel | Tidak didelegasikan ke connector |
| Provider auth/payload/channel | Connector | Tidak boleh bocor ke Kernel |
| Operation support dan limit provider | Connector manifest | Divalidasi saat startup dan sebelum mutation |
| Provider webhook verification | Connector | Kernel menangani dedup, state transition, dan outbox |
| Direct method visibility | Catalog + documented/certified assignment | Capability disabled tidak dapat diaktifkan |
| Provider transaction truth | Provider | Disinkronkan melalui resource yang sama |
| Event ke Emisell Backend | Durable outbox | Payload canonical dan HMAC-signed |

## 3. Kontrak Connector SDK

Setiap connector di dalam runner wajib memenuhi interface universal berikut:

```go
type Connector interface {
    Code() string
    Manifest() Manifest
    ValidatePaymentMethod(PaymentMethodMapping) error
    ValidatePayment(PaymentValidation) error

    VerifyInstallation(context.Context, InstallationInput) (InstallationResult, error)
    DisableInstallation(context.Context, InstallationInput) error
    CreatePayment(context.Context, PaymentInput) (PaymentResult, error)
    GetPayment(context.Context, PaymentLookup) (PaymentResult, error)
    CapturePayment(context.Context, CaptureInput) (PaymentResult, error)
    CancelPayment(context.Context, PaymentLookup, string, string) (PaymentResult, error)
    SimulatePayment(context.Context, PaymentLookup, int64, string) error
    CreateRefund(context.Context, RefundInput) (RefundResult, error)
    GetRefund(context.Context, RefundLookup) (RefundResult, error)
    HandleWebhook(context.Context, WebhookInput) (WebhookEvent, error)
}
```

Semua method tersedia agar kontrak compile-time seragam. Hanya operation yang
tercantum dalam `Manifest.Operations` yang boleh ditawarkan Kernel. Method yang
belum didukung tetap mengembalikan `connector.ErrNotSupported` sebagai defense
in depth.

## 4. Manifest dan registry

Manifest minimal memuat:

- `code`, `name`, `version`, dan jenis runtime;
- daftar operation yang benar-benar didukung;
- schema credential tanpa nilai secret;
- profil conformance sandbox per canonical payment method;
- validator mapping dan nominal di implementasi connector.

Runner membangun registry implementasi provider sekali saat startup. Payment
Kernel menemukan salinan manifest melalui `/partner/v1/capabilities` dan
membangun remote registry. Startup gagal apabila:

- tidak ada connector;
- connector `nil`;
- manifest tidak valid;
- `Connector.Code()` berbeda dari `Manifest.Code`;
- ada provider code duplikat;
- operation atau credential field duplikat/tidak dikenal.

Manifest disalin saat masuk dan keluar registry agar caller tidak dapat mengubah
capability runtime secara tidak sengaja.

## 5. Mapping payment method

Terdapat tiga lapisan yang tidak boleh digabung:

```text
Master method       Provider mapping               Provider channel
qris           →    qr_code / qris            →    QRIS
va_bca         →    bank_transfer / bca       →    BCA_VIRTUAL_ACCOUNT
card           →    card / card               →    CARDS
```

`payment_methods` adalah catalog universal. Mapping disimpan di
`provider_payment_method_capabilities`. Connector memvalidasi bahwa kombinasi
canonical code dan mapping memang dapat dieksekusi. Assignment direct-channel
dapat dibuat untuk capability `DOCUMENTED` atau `CERTIFIED` dan installation
`ACTIVE`; capability `DISABLED` tetap ditolak.

Dashboard yang memilih gateway lebih dahulu mengambil
`GET /api/v1/provider-options`; flow direct-channel mengambil
`GET /api/v1/payment-options` dan mengirim kembali `payment_option_id` ke
`POST /api/v1/payment-sessions`.
Browser tidak menerima service key dan tidak memanggil provider connector.

## 6. Operation matrix reference connector

| Operation | Xendit v1 | Midtrans v1 | Duitku v1 | DOKU v1 | iPaymu v1 | Catatan |
|---|---:|---:|---:|---:|---:|---|
| Verify/disable installation | Ya | Ya | Ya | Ya | Ya | Credential diverifikasi langsung ke provider; disable lokal |
| Create/get payment | Ya | Ya | Ya | Ya | Ya | iPaymu lookup memakai referenceId |
| Provider-hosted checkout | Ya | Ya | Ya | Ya | Ya | UI checkout tetap milik provider |
| Sandbox simulation | Ya | Tidak | Tidak | Tidak | Tidak | Provider lain memakai customer action pada halaman provider |
| Handle webhook | Ya | Ya | Ya | Ya | Ya | Signature diverifikasi connector lalu dinormalisasi |
| Capture | Belum | Belum | Belum | Belum | Belum | Tidak diiklankan manifest |
| Cancel | Belum | Disiapkan | Belum | Belum | Belum | Tetap ditutup sampai sandbox evidence |
| Create refund | Disiapkan | Disiapkan | Belum | Belum | Belum | Tetap ditutup sampai sandbox + webhook evidence |
| Get refund provider | Tidak | Disiapkan | Belum | Belum | Belum | Projection canonical selalu tersedia |

Operation baru baru boleh dimasukkan ke manifest setelah implementation,
contract test, sandbox evidence, webhook evidence, dan failure-mode test lulus.

## 7. Lifecycle dan consistency

Payment state canonical:

```text
CREATED → PROCESSING/PENDING → SUCCEEDED
                  ├──────────→ FAILED
                  ├──────────→ CANCELLED
                  ├──────────→ EXPIRED
                  └──────────→ UNKNOWN
```

Aturan mutasi:

1. semua create/cancel/refund memakai `Idempotency-Key`;
2. key + body sama mengembalikan resource yang sama;
3. key sama + body berbeda ditolak;
4. timeout mutation yang ambigu menjadi `UNKNOWN`;
5. `UNKNOWN` tidak boleh automatic failover ke provider lain;
6. status terminal tidak boleh diturunkan oleh webhook terlambat;
7. retry hanya memakai logical operation dan provider resource yang sama.

`SUCCEEDED` tetap boleh mengoreksi `EXPIRED`, `FAILED`, atau `CANCELLED` karena
konfirmasi dana masuk tidak boleh dibuang. Koreksi `EXPIRED → SUCCEEDED`
menambahkan flag canonical `late_payment`; koreksi `UNKNOWN → SUCCEEDED`
menambahkan `provider_delayed_confirmation`. Flag ikut ke outbox agar aturan
order terlambat berada di Emisell Backend, bukan di connector provider.

Refund mempunyai guard tambahan:

1. dana selalu kembali melalui payment route asal;
2. payment method asal disimpan bersama payment dan refund;
3. policy refund berada pada metadata capability per payment method, bukan
   hanya pada manifest provider;
4. full/partial dan multiple-partial divalidasi transaksional;
5. alasan serta actor disimpan pada refund dan audit log;
6. acknowledgement provider asynchronous tetap `PENDING` sampai callback final;
7. installation tidak boleh di-uninstall selama refund liability masih terbuka.

## 8. Webhook boundary

```text
Provider
  → /webhooks/v1/providers/{provider}/{installation_id}
  → Connector.HandleWebhook: verify + normalize
  → encrypted inbox + deduplication
  → canonical transition
  → durable outbox
  → signed event ke Emisell Backend
```

Payload provider tidak diteruskan ke Emisell Backend. Event keluar hanya memuat
resource canonical, previous/current status, merchant identity, event ID, dan
timestamp. Delivery bersifat at-least-once; Emisell Backend wajib deduplicate
berdasarkan `evt_*`.

## 9. Isolated Connector Runner

Fase aktif memakai connector out-of-process melalui `/partner/v1/*`. Process
Payment Proxy tidak mengimpor Xendit atau Midtrans dan hanya mengenal interface `Connector`.
Runner memuat implementasi provider, memverifikasi bearer token private, membatasi
body 1 MiB, tidak mengikuti redirect, dan tidak mencatat credential/payload.

Readiness API bergantung pada readiness runner. Timeout mutation yang mungkin
sudah diterima runner dipertahankan sebagai `OUTCOME_UNKNOWN`. Production wajib
HTTPS internal, secret manager, network policy, resource limit, dan replica
runner yang dapat diskalakan independen.

## 10. Checklist menambah provider

1. buat package connector dan manifest;
2. implementasikan credential verification tanpa menyimpan plaintext;
3. implementasikan create/get payment dan webhook normalization;
4. isi master capability mapping dan source documentation;
5. daftarkan connector hanya di composition root Connector Runner;
6. lulus manifest/registry test dan provider unit test;
7. lulus sandbox conformance per method;
8. uji idempotency, timeout ambigu, duplicate/out-of-order webhook, dan redaction;
9. izinkan assignment ketika mapping connector valid dan capability tidak `DISABLED`;
10. ubah capability dari `DOCUMENTED` ke `CERTIFIED` setelah evidence tersimpan.

Perubahan provider baru tidak boleh menambah `if provider == ...` pada API,
Payment Kernel, checkout contract, atau canonical webhook delivery.

## 11. Non-goals fase awal

- weighted routing dan smart retry lintas provider;
- automatic failover setelah mutation ambigu;
- penyimpanan PAN/CVV atau UI card form milik Emisell;
- ledger settlement milik provider;
- mengaktifkan semua operation hanya karena provider mendokumentasikannya;
- kompatibilitas penuh Hyperswitch.

## 12. Urutan pengembangan

1. **Foundation (selesai):** manifest, registry, reference connector Xendit,
   kernel tanpa provider branching, conformance unit tests.
2. **Xendit production hardening (local baseline selesai):** manifest metrics,
   10.000-request read-only load test, chaos/timeout, webhook ordering, dan
   security regression sudah tersedia. Production-like soak, secret rotation,
   backup-restore, external monitoring, dan canary evidence masih wajib.
3. **Isolated runner (selesai):** Xendit dipindahkan dari process API, capability
   discovery, bearer authentication, readiness, contract test, dan unknown-outcome
   transport aktif melalui `/partner/v1/*`.
4. **Second connector (local baseline selesai):** Midtrans dimuat di runner untuk
   membuktikan kontrak tidak bias terhadap Xendit. Unit/contract test dan release
   gate sudah aktif; sandbox evidence per method masih wajib sebelum `CERTIFIED`.
5. **Third connector (local baseline selesai):** Duitku POP berjalan di shared
   Provider App sendiri dengan hosted/direct invoice, status, credential probe,
   HMAC-SHA256, callback normalization, dan source-only release bundle.
6. **Fourth connector (local baseline selesai):** DOKU Checkout berjalan pada
   isolated shared runtime dengan hosted/direct payment link resmi, Non-SNAP
   request signing, status order, notification verification, dan source-only bundle.
7. **Fifth connector (local baseline selesai):** iPaymu API v2 berjalan pada
   shared runtime dengan Redirect Payment resmi, direct method mapping, signed
   reference lookup, callback HMAC, dan source-only release bundle.
8. **Deployment hardening berikutnya:** deployment automation, per-provider
   resource isolation, canary, kill switch, dan production-like soak.
