# Production readiness — Emisell Payment Engine

Status: **HARDENING IN PROGRESS**

Dokumen ini adalah runbook operasional untuk memindahkan Payment Proxy dari
sandbox fungsional menuju production. Lulus unit test atau 10.000 request lokal
tidak dengan sendirinya membuktikan kapasitas production; target akhir harus
diuji pada topology, database, network, dan jumlah replica yang menyerupai
production.

## Baseline container production

Repository menyediakan topology single-server yang dapat dibootstrap tanpa
menulis `.env` secara manual:

```bash
./scripts/deploy-production.sh deploy payment-proxy.example.com \
  https://api.example.com/webhooks/v1/payment-proxy
```

Baseline ini meliputi:

- secret acak persisten yang dibuat pada first deploy;
- TLS publik otomatis melalui Caddy;
- private CA dan TLS internal antara Kernel dan setiap connector;
- image target terpisah sehingga connector tidak membawa binary Kernel atau
  binary provider lain;
- container aplikasi non-root dan read-only; ingress read-only dengan capability
  minimum; `no-new-privileges`, resource/pid limit, dan log rotation;
- network terpisah untuk ingress, database, connector control, provider egress,
  dan webhook egress;
- hanya port `80` dan `443` yang dipublikasikan;
- production startup tetap fail-closed ketika secret atau URL wajib hilang.

File `.deploy/production.env` harus diperlakukan sebagai recovery secret. File
ini tidak masuk Git dan wajib dibackup terenkripsi. Kehilangan
`CREDENTIAL_ENCRYPTION_KEY` menyebabkan credential provider di database tidak
dapat didekripsi.

Topology ini adalah baseline single-server, bukan klaim high availability.
Untuk multi-replica, pindahkan PostgreSQL, secret, ingress, metrics, dan image
registry ke layanan terkelola/orchestrator tanpa mengubah kontrak API.

## SLO awal

| Signal | Target awal | Alert |
|---|---:|---:|
| Unified API availability | `99.9%` non-5xx | burn-rate 14.4x/1h atau 6x/6h |
| Unified API latency | p95 `< 500 ms` | p95 `> 500 ms` selama 10 menit |
| Mutation `UNKNOWN` | `< 0.1%` | satu kejadian langsung masuk operational queue |
| Provider webhook processing | `99.9%` accepted/duplicate | invalid atau failed naik terus 5 menit |
| Emisell outbox delivery | `99.9%` dalam 5 menit | DEAD > 0 atau oldest pending > 5 menit |

Availability process dihitung dari response non-5xx dibagi seluruh request.
Kesalahan caller `4xx` tidak mengurangi availability, tetapi tetap harus
dipantau untuk menemukan integrasi checkout yang rusak.

## Endpoint observability

Keduanya dilindungi `X-Admin-API-Key`:

```text
GET /api/v1/admin/observability
GET /api/v1/admin/metrics
GET /api/v1/admin/engine/readiness
```

Endpoint pertama dipakai dashboard untuk snapshot process. Endpoint kedua
berformat Prometheus dan memuat:

- total response berdasarkan status class;
- in-flight request;
- histogram latency HTTP;
- connector outcome `unknown`, `not_supported`, dan `rejected`;
- provider webhook `accepted`, `duplicate`, dan `invalid`.

Counter bersifat process-local dan reset ketika restart. Production wajib
men-scrape setiap replica ke metrics storage terpusat; jangan menjumlahkan
snapshot dashboard secara manual.

Endpoint readiness report juga mengekspos request guard efektif tanpa secret.
Gunakan sebagai deployment verification setelah perubahan environment.

## API traffic guards

Production mengaktifkan tiga lapis guard per replica:

```text
API_REQUEST_TIMEOUT_SECONDS=25
API_RATE_LIMIT_RPS=300
API_RATE_LIMIT_BURST=600
API_MAX_IN_FLIGHT=500
```

Rate limit bersifat token bucket per merchant per replica dan melindungi
kapasitas lokal. Ingress atau WAF tetap wajib menerapkan limit gabungan seluruh
replica serta perlindungan untuk credential invalid. `429` dan `503 API_BUSY`
harus di-retry dengan jitter; mutation wajib mempertahankan `Idempotency-Key`.
Production startup ditolak jika traffic guard dimatikan atau request timeout
tidak lebih panjang dari connector timeout.

## UNKNOWN guard

Mutation Xendit diperlakukan `UNKNOWN` ketika:

- network timeout atau transport terputus;
- response body tidak dapat dibaca;
- provider membalas HTTP `408` atau `5xx`;
- provider membalas `2xx` tetapi JSON rusak atau identifier payment hilang.

HTTP `4xx` selain `408` tetap dianggap provider rejection deterministik.
Ketika `UNKNOWN`, API menyimpan payment/refund `UNKNOWN`, mengembalikan `202`,
dan dilarang membuat transaksi pengganti atau failover otomatis. Operator harus
melakukan GET/reconciliation terhadap logical resource yang sama.

## Webhook ordering dan duplicate

Database menggunakan unique key `(source, external_event_id)` dan outbox
deduplication key `{source}:{external_event_id}`. Duplicate event menghasilkan
`accepted=false` tanpa status history dan outbox kedua.

Canonical state guard mempertahankan state terminal terhadap event terlambat:

- `SUCCEEDED` tidak dapat turun menjadi pending/failed/expired;
- `FAILED`, `CANCELLED`, dan `EXPIRED` tidak dapat kembali pending;
- provider success boleh mengoreksi terminal non-success berdasarkan resource
  provider yang sama.

Raw provider payload disimpan terenkripsi; signature connector harus lolos
sebelum inbox atau canonical state berubah.

## Read-only load test

Load runner mempunyai allowlist dan tidak dapat memanggil mutation endpoint.
Credential dibaca dari environment agar tidak tampil di process arguments.

```bash
export PAYMENT_PROXY_BASE_URL=http://localhost:18080
export SERVICE_API_KEY='<secret>'
export DASHBOARD_MERCHANT_ID=merchant_load_test

./scripts/load-readonly.sh \
  -path /api/v1/payment-options \
  -requests 10000 \
  -concurrency 100 \
  -p95-target 500ms \
  -max-error-rate 0.001
```

`payment-options` tidak memakai query environment dan menguji option aktif
sandbox serta live milik merchant dalam satu respons.

`provider-options` masih memakai query environment. Set
`PAYMENT_PROXY_EXECUTION_MODE=sandbox|live` ketika endpoint tersebut dipilih
untuk menguji grouping per installation provider aktif.

Endpoint yang diizinkan hanya health, provider catalog, payment-method catalog,
payment options, dan provider options. Runner gagal apabila path mutation dipilih, error rate
melewati threshold, atau p95 melewati target.

Untuk production-like test, jalankan dari host berbeda melalui load balancer,
gunakan data merchant sintetis, simpan hasil, dan korelasikan dengan database
CPU/connection/lock, API CPU/memory/GC, provider rate limit, serta outbox lag.

## Database pool

Pool tidak lagi hard-coded dan dikontrol melalui:

```text
DATABASE_MAX_CONNECTIONS=40
DATABASE_MIN_CONNECTIONS=4
DATABASE_CONNECTION_MAX_LIFETIME_SECONDS=3600
DATABASE_CONNECTION_MAX_IDLE_SECONDS=300
```

Total `max connections` seluruh replica harus berada di bawah kapasitas
PostgreSQL setelah menyisakan ruang untuk migration, worker, dashboard query,
maintenance, dan emergency access.

## Security gates production

API menolak startup production apabila:

- service/admin API key kurang dari 32 karakter atau tampak sebagai default;
- encryption key bukan base64 32-byte atau seluruh byte sama;
- Xendit base URL tidak memakai HTTPS;
- public webhook URL atau certification return URL bukan HTTPS;
- webhook destination mengizinkan HTTP atau private network.

Tambahan aturan deployment:

1. simpan API key dan encryption key dalam secret manager;
2. pisahkan admin key dari service key;
3. jangan kirim key melalui query string, log, Postman export, atau browser;
4. lindungi `/api/v1/admin/*` dengan admin key kuat, per-IP rate limit, dan WAF/Cloudflare rate limiting;
5. rotasi service key dengan overlap pendek dan audit revoke;
6. backup database terenkripsi dan uji restore;
7. alert ketika decrypt/authentication failure muncul;
8. card tetap hosted provider; PAN/CVV tidak boleh masuk Emisell.

## Evidence wajib sebelum production

- race test, vet, unit/contract test hijau;
- 10.000+ read-only request pada topology production-like memenuhi SLO;
- soak test minimal 30 menit tanpa connection leak/goroutine growth;
- chaos timeout/408/5xx menghasilkan `UNKNOWN`, bukan automatic retry;
- duplicate dan out-of-order webhook tidak menggandakan outbox atau menurunkan
  terminal status;
- backup/restore drill berhasil;
- key rotation dan revoke drill berhasil;
- dashboard/Prometheus alert dapat mendeteksi 5xx, p95, UNKNOWN, DEAD outbox;
- satu canary merchant production berhasil sebelum rollout bertahap.

## Yang belum diklaim

Pondasi kode dan local hardening dapat selesai, tetapi status **production
ready** baru boleh diberikan setelah topology production-like load/soak test,
backup-restore drill, external monitoring, dan canary production evidence selesai.
