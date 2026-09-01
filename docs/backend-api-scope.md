# Emisell Backend API Scope

Dokumen ini menetapkan endpoint yang menjadi kontrak Emisell Backend. Tujuannya
adalah mencegah API operator, runtime connector, dan detail provider ikut
dianggap sebagai kewajiban integrasi Main Service.

## Kontrak inti Emisell Backend

| Domain | Endpoint | Alasan |
|---|---|---|
| Provider | `GET /api/v1/providers` | Katalog aktif dan dynamic credential schema untuk Dashboard Merchant. |
| Installation | `POST /api/v1/provider-installations` | Membuat slot Sandbox atau Live. |
| Installation | `GET /api/v1/provider-installations` | Menampilkan kedua slot connection merchant. |
| Installation | `GET /api/v1/provider-installations/{id}` | Membaca state terbaru setelah mutation atau callback. |
| Credential | `PUT /api/v1/provider-installations/{id}/credentials` | Konfigurasi awal dan verifikasi provider. |
| Credential | `PATCH /api/v1/provider-installations/{id}/credentials` | Rotasi sebagian credential tanpa membaca secret lama. |
| Lifecycle | `POST .../{id}/activate` | Mengizinkan connection dipakai checkout. |
| Lifecycle | `POST .../{id}/deactivate` | Menghentikan pemakaian tanpa menghapus credential. |
| Lifecycle | `DELETE /api/v1/provider-installations/{id}` | Uninstall dan hapus ciphertext credential. |
| Method | `GET /api/v1/payment-methods` | Master metode canonical. |
| Assignment | `GET /api/v1/payment-method-assignments` | Konfigurasi gateway merchant saat ini. |
| Assignment | `PUT /api/v1/payment-method-assignments` | Mengikat metode ke installation aktif. |
| Assignment | `POST .../{id}/deactivate` | Menutup option untuk checkout baru. |
| Checkout | `GET /api/v1/payment-options` | Opaque option yang boleh dipakai checkout. |
| Readiness | `GET /api/v1/integration-readiness` | Evidence kesiapan integrasi per merchant dan Sandbox/Live. |
| Payment | `POST /api/v1/payment-sessions` | Create payment normalized untuk seluruh provider/metode. |
| Payment | `GET /api/v1/payment-sessions/{id}` | Status canonical dan provider sync. |
| Payment | `POST .../{id}/cancel` | Cancel bila capability connector tersedia. |
| Refund | `POST /api/v1/refunds` | Return-to-source refund bersyarat pada policy channel dan connector release. |
| Refund | `GET /api/v1/refunds/{id}` | Projection canonical untuk status asynchronous. |
| Event | Receiver canonical milik Emisell Backend | Menerima status durable dari outbox Payment Proxy. |

## Bukan kontrak Emisell Backend

| Kelompok | Contoh | Pemilik |
|---|---|---|
| Provider release | `/api/v1/admin/provider-app-*` | Admin Control Plane |
| Service keys | `/api/v1/admin/service-api-keys` | Admin Control Plane |
| Observability | `/api/v1/admin/observability`, metrics, readiness | Operator/infrastructure |
| Runtime discovery | `/api/v1/engine/capabilities` | Payment Kernel/deployment diagnostic |
| Certification | `/api/v1/connector-certifications*` | Operator/provider release workflow |
| Operational payment views | list payment dan timeline | Dashboard operator |
| Webhook operations | settings, inbox, deliveries, replay | Dashboard operator/worker |
| Reconciliation | `/api/v1/reconciliation/*` | Operator workflow |
| Health | `/health/live`, `/health/ready` | Orchestrator/load balancer |
| Provider ingress | `/webhooks/v1/providers/*` | Payment provider → Payment Proxy |
| Connector contract | `/partner/v1/*` | Payment Proxy → isolated runtime |

Endpoint operational yang saat ini masih memakai namespace service diperlakukan
sebagai API internal dashboard. Jangan tambahkan pemanggilan baru dari Emisell
Backend. Pemindahan path/auth ke `/api/v1/admin/*` dilakukan sebagai refactor
terpisah agar tidak memutus dashboard yang sedang berjalan.

## Endpoint yang tidak perlu dibuat

- `POST /provider-installations/{id}/verify`: konfigurasi credential sudah
  melakukan verifikasi provider.
- `POST /provider-installations/{id}/switch-environment`: Sandbox dan Live
  adalah dua installation terpisah, bukan state yang dimutasi.
- `/xendit/*`, `/midtrans/*`, atau endpoint provider lain: connector
  menerjemahkan semuanya ke model normalized.
- endpoint merchant untuk memilih runtime/container: Runtime Dispatcher memilih
  shared runtime dari `provider_code + provider_version`.
- endpoint membaca/decrypt credential: API hanya mengembalikan metadata field.
- endpoint create payment per QRIS/VA/e-wallet/card: `payment_option_id`
  menentukan metode pada `POST /payment-sessions`.
- capture pada kontrak inti sebelum ada released connector yang mengiklankan
  dan lulus capability tersebut. Refund sudah menjadi kontrak inti bersyarat:
  channel wajib mempunyai policy return-to-source dan runtime wajib
  mengiklankan `create_refund`.

Uninstall connection ditolak dengan `REFUND_LIABILITY_OPEN` selama credential
masih dibutuhkan untuk payment yang refundable atau refund non-terminal.
Deactivate menghentikan checkout baru tanpa menghancurkan kemampuan memenuhi
refund transaksi lama.

## Aturan penambahan endpoint

Endpoint baru hanya ditambahkan bila menghasilkan resource atau lifecycle
canonical baru. Perbedaan payload, authentication, URL, signature, environment,
atau channel provider harus diselesaikan oleh connector dan schema metadata,
bukan dengan endpoint baru pada Emisell Backend API.
