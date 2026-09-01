# Xendit isolated connector conformance

## Local production-hardening evidence — 28 Agustus 2026

Hardening berikut dijalankan tanpa membuat transaksi provider baru:

- chaos test membuktikan mutation timeout, HTTP `408`, HTTP `5xx`, response
  `2xx` rusak, dan response tanpa payment ID menjadi `UNKNOWN`;
- HTTP `4xx` validation tetap menjadi rejection deterministik;
- PostgreSQL integration test membuktikan webhook ID duplikat hanya menghasilkan
  satu inbox/outbox, sedangkan event `FAILED` terlambat tidak menurunkan payment
  `SUCCEEDED`;
- race test seluruh backend lulus;
- read-only load test `GET /api/v1/payment-options`: 10.000 request,
  concurrency 100, 0 failure, sekitar 12.719 request/detik, p50 6 ms,
  p95 15 ms, p99 38 ms pada mesin development lokal;
- telemetry server setelah test mencatat availability 100%, p95 10 ms, dan SLO
  process `MEETING`;
- tidak ada error/panic runtime dan data sintetis integration test dibersihkan.

Angka lokal adalah baseline regresi, bukan klaim kapasitas production. Bukti
production-like load balancer/database, soak test, backup-restore, external
Prometheus, serta canary merchant tetap wajib mengikuti
`docs/production-readiness.md`.

## Scope

Release `emisell-xendit-v1` menargetkan Payments API v3 dengan capability berikut:

| Method | Channel | Required proof |
|---|---|---|
| QRIS | `QRIS` | create, next action, retrieve, simulator, provider webhook, Emisell delivery |
| BCA, Mandiri, BNI, BRI, Permata, CIMB, BSI, Muamalat VA | channel VA terkait | create, VA action, retrieve, simulator, provider webhook, Emisell delivery |
| OVO, DANA, ShopeePay, LinkAja, AstraPay | channel wallet terkait | create, redirect/mobile authorization, retrieve, provider webhook, Emisell delivery |
| Card | Payment Session `PAYMENT_LINK` / `CARDS` | create session, hosted checkout, retrieve session, completed webhook, Emisell delivery |

Danamon VA, paylater, Jenius Pay, retail, dan refund tidak boleh dipromosikan hanya berdasarkan dokumentasi provider.

## Hasil certification saat ini

| Status | Capability |
|---|---|
| `CERTIFIED` | `qris`, `va_bca`, `va_mandiri`, `va_bni`, `va_bri`, `va_permata`, `va_cimb`, `va_bsi`, `va_muamalat`, `ewallet_ovo`, `ewallet_dana`, `ewallet_shopeepay`, `ewallet_linkaja`, `ewallet_astrapay`, `card` |
| `DOCUMENTED` | `va_danamon` — Payments API v3 sandbox account menolak channel/endpoint ini |
| `DOCUMENTED` | `paylater_kredivo`, `paylater_akulaku`, `paylater_indodana` — channel belum tersedia untuk account sandbox yang diuji |
| `DOCUMENTED` | `digital_banking_jenius` — Payments API v3 sandbox account menolak channel/endpoint ini |

`DOCUMENTED` berarti mapping katalog tersedia, bukan bukti bahwa provider menerima transaksi. Hanya `CERTIFIED` yang boleh di-assign ke checkout.

Pada 28 Agustus 2026, `card` dan delapan Virtual Account terlebih dahulu diverifikasi ulang menggunakan payment sandbox yang sama tanpa membuat transaksi pengganti. Kesembilan run menghasilkan `PASSED` dengan bukti `webhook_delivery` dan `emisell_delivery`.

Pada hari yang sama dilakukan fresh end-to-end run setelah callback Emisell development diaktifkan dan signed connection test diterima HTTP `202`. Run tersebut membuat payment sandbox baru untuk `qris`, delapan Virtual Account, `card`, serta lima e-wallet. Seluruh lima belas payment menghasilkan `PASSED`: status provider sukses, direct provider webhook diproses, outbox berstatus `DELIVERED`, dan event tersimpan di receiver Emisell. QRIS serta Virtual Account menghasilkan 10/10 checks; card dan e-wallet menghasilkan 8/8 checks setelah customer authorization diselesaikan. Pengiriman ulang satu event DANA identik dibalas HTTP `200` sebagai duplicate dan hanya menaikkan `duplicate_count`, tanpa membuat transaksi atau outbox baru.

Fresh run `ewallet_ovo` sempat `BLOCKED` karena Xendit mengembalikan flow `mobile_authorization` tanpa URL simulator. Payment yang sama kemudian berubah menjadi `SUCCEEDED` secara asynchronous; resume certification menghasilkan `PASSED` setelah provider webhook dan signed Emisell delivery terverifikasi. Dashboard tetap menampilkan instruksi approval aplikasi saat OVO masih menunggu, dan tidak membuat payment pengganti.

Receiver pada run ini adalah receiver Emisell lokal untuk development. Sebelum live release, bukti yang sama wajib diulang menggunakan Callback URL HTTPS milik Emisell Backend production.

## Prasyarat

- Docker project aktif.
- Xendit development Secret API Key dengan permission Money-in Write dan Balance Read.
- Installation Xendit sandbox dalam status ACTIVE.
- `PAYMENT_PROXY_PUBLIC_BASE_URL` berupa HTTPS publik.
- `CERTIFICATION_RETURN_URL` berupa halaman HTTPS publik yang dapat dibuka pelanggan setelah provider action; gunakan `/certifications/return` pada domain dashboard.
- Payments API v3 webhook token dari Xendit Dashboard tersimpan di credential vault.
- Topik `Payment Status`, `Payment Request Status`, `Payment Session Completed`, dan `Payment Session Expired` di Xendit Dashboard mengarah ke callback installation.
- Callback Xendit mengarah ke:

```text
https://<payment-domain>/webhooks/v1/providers/xendit/<installation-id>
```

## Menjalankan diagnostik internal

Tidak ada tombol certification pada dashboard provider. Evidence baru dijalankan
oleh CI/operator internal pada tenant sandbox bila diperlukan:

```http
POST /api/v1/admin/connector-certifications/run
X-Admin-API-Key: <admin-key>
X-Emisell-Merchant-ID: <test-tenant>
X-Emisell-Execution-Mode: sandbox
Content-Type: application/json

{
  "installation_id": "ins_...",
  "payment_method_code": "va_bca"
}
```

Run memeriksa:

1. installation aktif dan sandbox;
2. canonical capability mapping;
3. ciphertext credential dapat dibuka untuk installation yang benar;
4. payment request berhasil dibuat;
5. customer next action tersedia;
6. payment yang sama dapat diambil kembali;
7. sandbox simulator menerima payment, atau customer menyelesaikan redirect/mobile authorization pada channel yang simulatornya tidak tersedia;
8. direct provider webhook payment yang sama benar-benar diterima dan diproses.
9. signed canonical event untuk payment yang sama benar-benar berstatus `DELIVERED` ke Emisell Backend.

Jika channel membutuhkan customer authorization, run pertama menghasilkan
`BLOCKED` dengan `payment_id`. Setelah halaman sandbox provider selesai,
CI/operator mengirim kembali request dengan tambahan `payment_id`. Resume
mengambil resource yang sama dan tidak membuat payment kedua.

Card selalu memakai hosted Xendit Payment Session. Nomor kartu, expiry, dan CVV dimasukkan hanya pada domain checkout Xendit; data tersebut dilarang dalam request Emisell, metadata, log, database, dan Postman collection. Certification card memakai test card Xendit pada halaman hosted, kemudian mewajibkan status session `COMPLETED` serta webhook `payment_session.completed` untuk payment yang sama.

Evidence sandbox card mencakup dua jalur: frictionless authorization dan 3DS challenge dengan OTP simulator Xendit. Keduanya menghasilkan session `COMPLETED`, canonical payment `SUCCEEDED`, webhook inbox `PROCESSED`, dan outbox Emisell `DELIVERED`.

Jika callback tidak diterima dalam verification window, create/retrieve tetap diuji tetapi hasil akhir `BLOCKED`, bukan false `PASSED`.

## Webhook proof

Simulator Xendit merespons `PENDING`; hasil akhir dikirim asynchronous melalui webhook. Bukti lengkap memerlukan:

```text
payment_sessions.status = SUCCEEDED
webhook_inbox.source = xendit
webhook_inbox.status = PROCESSED
outbox_events.status = DELIVERED
```

Webhook duplicate dengan `webhook-id` yang sama tidak boleh membuat transition atau outbox kedua.

## Safety

- Jangan menjalankan simulator dengan live key.
- Jangan menyalin API key ke dokumentasi, log, atau Postman export.
- Mutation transport timeout menghasilkan `UNKNOWN`.
- Jangan membuat payment baru sebagai pengganti sampai GET/reconciliation memastikan hasil resource lama.
