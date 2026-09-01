# Emisell Backend API Scope

Dokumen ini menetapkan endpoint yang menjadi kontrak Emisell Backend. Tujuannya
adalah mencegah API operator, runtime connector, dan detail provider ikut
dianggap sebagai kewajiban integrasi Main Service.

## Kontrak inti Emisell Backend

| Domain | Endpoint | Alasan |
|---|---|---|
| Provider | `GET /api/v1/providers?q=<keyword>` | Katalog dan pencarian provider untuk Dashboard Merchant. |
| Provider option | `GET /api/v1/provider-options?environment=...` | Provider connection aktif beserta metode aktif yang dikelompokkan untuk pilihan checkout merchant. |
| Payment method | `GET /api/v1/payment-methods?q=<keyword>` | Discovery metode pembayaran canonical beserta provider pendukung. |
| Installation | `POST /api/v1/provider-installations` | Membuat slot Sandbox atau Live. |
| Installation | `GET /api/v1/provider-installations` | Menampilkan kedua slot connection merchant. |
| Installation | `GET /api/v1/provider-installations/{id}` | Membaca state terbaru setelah mutation atau callback. |
| Credential | `PUT /api/v1/provider-installations/{id}/credentials` | Konfigurasi awal dan verifikasi provider. |
| Credential | `PATCH /api/v1/provider-installations/{id}/credentials` | Rotasi sebagian credential tanpa membaca secret lama. |
| Lifecycle | `POST .../{id}/activate` | Mengizinkan connection dipakai checkout. |
| Lifecycle | `POST .../{id}/deactivate` | Menghentikan pemakaian tanpa menghapus credential. |
| Lifecycle | `DELETE /api/v1/provider-installations/{id}` | Uninstall dan hapus ciphertext credential. |
| Readiness | `GET /api/v1/integration-readiness` | Evidence kesiapan integrasi per merchant dan Sandbox/Live. |
| Payment | `POST /api/v1/payment-sessions` | Membuat hosted checkout provider dari installation aktif. |
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
| Release verification | `/api/v1/admin/provider-apps/*/transition` | Admin Control Plane/backend release gate |
| Sandbox diagnostics | `/api/v1/admin/connector-certifications*` | Internal CI/troubleshooting only |
| Operational payment views | list payment dan timeline | Dashboard operator |
| Webhook operations | settings, inbox, deliveries, replay | Dashboard operator/worker |
| Reconciliation | `/api/v1/reconciliation/*` | Operator workflow |
| Health | `/health/live`, `/health/ready` | Orchestrator/load balancer |
| Provider ingress | `/webhooks/v1/providers/*` | Payment provider → Payment Proxy |
| Connector contract | `/partner/v1/*` | Payment Proxy → isolated runtime |
| Direct-channel configuration | assignments dan `/payment-options` | Flow lanjutan; bukan kebutuhan hosted checkout utama |

Endpoint sandbox diagnostics sudah berada di namespace admin dan tetap
tenant-scoped. Jangan tambahkan pemanggilan dari Emisell Backend; lifecycle
merchant hanya Install → Configure/Verify credential → Activate.

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
- endpoint create payment per QRIS/VA/e-wallet/card: hosted checkout memakai
  `installation_id`, lalu halaman provider menampilkan channel yang aktif.
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
