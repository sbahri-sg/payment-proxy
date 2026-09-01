# Emisell Payment Platform

Emisell Payment Platform adalah payment engine dan control plane milik Emisell. Kernel Go mengelola lifecycle, idempotency, status canonical, reconciliation, webhook inbox, serta outbox ke Emisell Backend. Detail provider hanya berada di connector.

```text
Emisell Backend
      │
      ▼
Unified Payment API
      │
      ▼
Emisell Payment Kernel (Go)
      │
      ├── installation lifecycle
      ├── payment lifecycle
      ├── idempotency and UNKNOWN guard
      ├── provider webhook inbox
      └── signed Emisell outbox
              │
              ▼
      Universal Connector Contract
              │
      private /partner/v1/*
              │
      Isolated Connector Runtimes
              │
      ┌───────┼────────┬────────┐
      ▼       ▼        ▼        ▼
   Xendit  Midtrans   DOKU    Duitku
```

Payment baru menggunakan Emisell Payment Kernel dengan runtime connector
`isolated_container`. Payment lama dari runtime eksternal dipertahankan sebagai
`legacy_external` dan bersifat read-only agar migrasi tidak menghilangkan riwayat.

## Scope aktif

Connector Xendit di isolated runner menggunakan Payments API v3 dan Payment Session:

- QRIS: create, retrieve, customer QR action, simulator sandbox, dan webhook normalization;
- Virtual Account BCA, Mandiri, BNI, BRI, Permata, CIMB Niaga, BSI, dan Muamalat: create, retrieve, nomor VA, simulator sandbox, dan webhook normalization;
- OVO, DANA, ShopeePay, LinkAja, dan AstraPay: create, redirect/mobile authorization, retrieve, dan webhook normalization;
- Card: hosted Xendit Payment Session, redirect aman, pembayaran sandbox frictionless dan 3DS challenge, retrieve, serta webhook normalization tanpa PAN/CVV melewati Emisell;
- API key verification melalui Xendit Balance API;
- callback token verification dan deduplication menggunakan `webhook-id`;
- credential terenkripsi AES-256-GCM per installation.

Implementasi Xendit QRIS full refund memakai Unified Refund dan final melalui
webhook sudah disiapkan, tetapi operation release tetap fail-closed sampai bukti
sandbox serta webhook tersertifikasi. Refund channel lain juga tetap fail-closed
sampai policy return-to-source, identifier provider, dan conformance tersedia.
Danamon VA, Kredivo, Akulaku, Indodana, Jenius Pay, dan channel lain tetap
`DOCUMENTED`. Core tidak perlu berubah ketika capability baru ditambahkan;
connector dan capability mapping yang diperluas.

Connector Midtrans `emisell-midtrans-v1.1.0` berjalan sebagai Provider App
container tersendiri, terpisah dari runner Xendit. Ini membuktikan bahwa kontrak
engine tidak bergantung pada implementasi atau process provider tertentu:

- Provider Apps memakai alur provider-first: identitas provider dibuat satu
  kali, lalu setiap ZIP submission menjadi riwayat versi di bawah provider
  tersebut; runtime OCI dibangun dan dideploy terpisah;

- credential memakai `server_key` wajib dan `pop_id` opsional yang disimpan
  terenkripsi per installation;
- QRIS, BCA/BNI/BRI/CIMB/Permata VA, Mandiri Bill, GoPay, dan ShopeePay sudah
  mempunyai mapping Core API v2, create/get, customer next action, serta webhook
  signature normalization;
- cancel dan refund Midtrans sudah mempunyai adapter dan contract test, tetapi
  belum diumumkan di manifest runtime sampai bukti sandbox nyata tersedia;
- channel yang belum diprovisikan tetap fail-closed pada sandbox maupun live;
  connector tidak membuat fallback checkout yang belum terbukti dapat dibayar;
- capability Midtrans tetap `DOCUMENTED` sampai masing-masing channel lulus
  installation sandbox, payment, status, webhook terminal, dan delivery ke
  Emisell Backend; BCA Virtual Account sudah mempunyai evidence end-to-end dan
  berstatus `CERTIFIED` untuk BCA, BNI, dan Permata Virtual Account, sedangkan
  channel lain masih menunggu evidence terminal.

## Menjalankan lokal

```bash
cp .env.example .env
docker compose up --build -d
docker compose ps
```

Alamat default:

- Dashboard: `http://localhost:13000`
- Payment API: `http://localhost:18080`
- Connector Runner: `http://127.0.0.1:18082` (private contract; local development only)
- Midtrans Provider App: `http://127.0.0.1:18083` (private contract; local development only)
- Liveness: `http://localhost:18080/health/live`
- Readiness: `http://localhost:18080/health/ready`
- Runtime capabilities: `GET http://localhost:18080/api/v1/engine/capabilities`
- Admin readiness report: `GET http://localhost:18080/api/v1/admin/engine/readiness`

## Deploy production satu perintah

Deployment production tidak memakai `.env` yang diedit manual. Script bootstrap
membuat seluruh password, encryption key, API key, session secret, webhook
secret, serta sertifikat TLS internal connector pada deployment pertama. Nilai
tersebut disimpan dengan permission `0600` di `.deploy/production.env` dan
digunakan kembali pada redeploy berikutnya.

Prasyarat yang tidak dapat dibuat Docker secara otomatis:

1. siapkan server Linux dengan Docker Engine dan Docker Compose;
2. arahkan DNS domain Payment Proxy ke IP server;
3. buka port TCP `80` dan TCP/UDP `443`;
4. siapkan URL HTTPS receiver Emisell Backend jika ingin mengaktifkan fallback
   delivery saat deployment pertama.

Jalankan dari root repository:

```bash
./scripts/deploy-production.sh deploy payment-proxy.example.com \
  https://api.example.com/webhooks/v1/payment-proxy
```

Callback URL boleh dikosongkan dan diatur kemudian dari menu Webhooks:

```bash
./scripts/deploy-production.sh deploy payment-proxy.example.com
```

Perintah di atas otomatis:

- membuat secret acak yang fail-closed dan tidak masuk Git;
- membuat private CA serta TLS certificate terpisah untuk Xendit dan Midtrans
  connector;
- membangun image minimum yang berbeda untuk Kernel, Xendit connector,
  Midtrans connector, dan dashboard;
- menjalankan PostgreSQL migration;
- menjalankan Kernel, worker, connector, dan dashboard sebagai non-root dengan
  read-only filesystem; gateway memakai capability minimum, sementara seluruh
  topology mendapat resource limit, log rotation, dan network segmentation;
- menjalankan Caddy sebagai ingress dengan HTTPS certificate otomatis;
- menunggu API, connector, dan dashboard berstatus sehat.

Operasional berikutnya:

```bash
./scripts/deploy-production.sh status
./scripts/deploy-production.sh logs api
./scripts/deploy-production.sh credentials
./scripts/deploy-production.sh deploy
```

Perintah `deploy` tanpa domain menggunakan konfigurasi yang sudah tersimpan.
Jangan menghapus `.deploy/production.env`: encryption key di dalamnya diperlukan
untuk membuka credential provider yang tersimpan. Backup file tersebut secara
terenkripsi bersama backup PostgreSQL. Production topology tersedia di
`docker-compose.production.yml`; Compose lokal tetap berada di
`docker-compose.yml`.

Konfigurasi minimum API:

```text
DATABASE_URL
CREDENTIAL_ENCRYPTION_KEY       base64 tepat 32 byte
SERVICE_API_KEY
ADMIN_API_KEY                  wajib di production
CONNECTOR_RUNNER_BASE_URLS    comma-separated private HTTPS runtime URLs
CONNECTOR_RUNNER_TOKENS       token per runtime dengan urutan yang sama
CONNECTOR_RUNNER_TOKEN        token runtime Xendit
MIDTRANS_PROVIDER_APP_TOKEN   token runtime Midtrans yang terpisah
XENDIT_BASE_URL                default https://api.xendit.co
MIDTRANS_SANDBOX_BASE_URL      default https://api.sandbox.midtrans.com
MIDTRANS_LIVE_BASE_URL         default https://api.midtrans.com
CONNECTOR_TIMEOUT_SECONDS      default 15
API_REQUEST_TIMEOUT_SECONDS    default 25
API_RATE_LIMIT_RPS             production default 300; 0 disables in development
API_RATE_LIMIT_BURST           production default 600; pair with RPS
API_MAX_IN_FLIGHT              production default 500; 0 disables in development
PAYMENT_PROXY_PUBLIC_BASE_URL  HTTPS publik; wajib di production
CERTIFICATION_RETURN_URL       HTTPS dashboard return page; wajib di production
```

`PAYMENT_PROXY_PUBLIC_BASE_URL` digunakan untuk callback langsung:

```text
POST /webhooks/v1/providers/xendit/{installation_id}
POST /webhooks/v1/providers/midtrans/{installation_id}
```

Dashboard Next.js juga meneruskan kontrak publik ke Kernel melalui jaringan
internal Docker. Dengan demikian satu origin dashboard dapat dipakai untuk
checkout dan provider webhook meskipun reverse proxy publik hanya menunjuk ke
port dashboard:

```text
/api/v1/*                         -> api:8080/api/v1/*
/webhooks/v1/providers/*          -> api:8080/webhooks/v1/providers/*
/api/webhooks/v1/providers/*      -> api:8080/webhooks/v1/providers/* (alias)
/health/*                         -> api:8080/health/*
```

Route `/api/auth/*` tetap dimiliki dashboard dan tidak diteruskan ke Kernel.

Untuk development dengan ngrok:

```bash
./scripts/xendit-dev-tunnel.sh start
```

Deployment Emisell yang memakai Nginx bersama menggunakan
`deploy/nginx-payment-proxy.conf`. Konfigurasi tersebut meneruskan `/api/v1/*`,
`/webhooks/v1/providers/*`, dan health checks ke Payment Proxy API, sedangkan
route dashboard diteruskan ke port dashboard. Set URL stabil berikut sebelum
recreate API dan dashboard:

```text
PAYMENT_PROXY_PUBLIC_BASE_URL=https://payment-proxy.emisell.com
```

Setelah tunnel aktif, pasang URL yang ditampilkan pada topik Xendit Dashboard **Payments API v3 – Payment Status**, **Payment Request Status**, **Payment Session Completed**, dan **Payment Session Expired**. Gunakan path `/webhooks/v1/providers/xendit/{installation_id}`, lalu konfigurasi ulang credential dengan API key dan webhook token.

Webhook Xendit di atas adalah **provider ingress**. Event keluar menuju Emisell Backend dikelola dari menu **Webhooks** pada dashboard:

1. isi Callback URL backend Emisell secara manual;
2. generate secret `whsec_` dan salin plaintext yang hanya tampil satu kali ke receiver Emisell Backend;
3. kirim connection test bertanda tangan;
4. aktifkan delivery setelah receiver siap.

Secret disimpan AES-GCM di database dan worker membaca perubahan tanpa restart. Environment berikut hanya bootstrap/fallback deployment lama:

```text
EMISELL_BACKEND_WEBHOOK_URL=https://api.emisell.com/webhooks/v1/payment-proxy
EMISELL_BACKEND_WEBHOOK_SECRET=<shared-secret-minimum-32-character>
```

Docker lokal memakai `emisell-receiver` sebagai contract-test receiver. Status `DELIVERED` ke receiver lokal bukan bukti bahwa Emisell Backend production telah menerima event. Dashboard Webhooks menampilkan destination aktif agar kedua kondisi ini tidak tertukar.

## Alur merchant

1. Install provider untuk `sandbox` atau `live`.
2. Masukkan API key dan webhook verification token.
3. Engine memverifikasi key langsung ke provider dan menyimpan ciphertext.
4. Aktifkan installation.
5. Assign master payment method yang sudah diverifikasi backend ke installation aktif.
6. Checkout meminta `payment-options`, lalu membuat payment dengan `payment_option_id`.

Semua amount API menggunakan minor unit integer. Untuk IDR, `1000000` berarti Rp10.000.

## Guardrail utama

- Mutation wajib memakai `Idempotency-Key`.
- Versi kontrak ditentukan oleh base path `/api/v1`; setiap response membawa `X-Request-ID` untuk tracing.
- Rate limit dan max in-flight bersifat per replica; ingress/WAF tetap mengendalikan limit cluster-wide.
- Timeout/transport error pada mutation menjadi `UNKNOWN`; jangan automatic failover.
- Credential provider tidak pernah dikembalikan API atau dashboard. Webhook secret hanya dikembalikan satu kali saat generate/rotate, kemudian hanya masked hint yang tersedia.
- Main Service API key dapat dibuat dan dicabut dari menu **API Keys** tanpa restart. Secret random 256-bit hanya tampil satu kali; PostgreSQL hanya menyimpan SHA-256 hash dan metadata tersamarkan.
- Credential plaintext hanya hidup selama request, lalu dienkripsi dengan AAD installation ID.
- Webhook diverifikasi sebelum status atau outbox berubah.
- ID event body, header webhook, dan idempotency key harus identik; Emisell Backend deduplicate berdasarkan ID tersebut.
- Terminal payment status tidak diturunkan oleh event/polling terlambat.
- Provider connector tidak boleh memasukkan detail provider ke Payment Kernel.

## Dokumentasi

- Scope endpoint Emisell Backend: [`docs/backend-api-scope.md`](docs/backend-api-scope.md)
- Dashboard API documentation: `http://localhost:13000/docs`
- AI-readable Backend contract: `http://localhost:13000/docs/llms.txt`
- Dashboard Main Service credentials: `http://localhost:13000/api-keys`
- [Engine source of truth](docs/emisell-payment-engine.md)
- [Production readiness](docs/production-readiness.md)
- [Architecture](docs/architecture.md)
- [Runtime refactor audit and phased plan](docs/runtime-refactor-audit.md)
- [Xendit conformance](docs/conformance-xendit.md)
- [Midtrans conformance](docs/conformance-midtrans.md)
- [API reference](docs/api.md)
- [Dashboard roadmap](docs/dashboard-roadmap.md)
- [Brand assets](docs/brand-assets.md)

## Validasi

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...

cd ../dashboard
npm run build

cd ..
docker compose build
docker compose up -d

./scripts/deploy-production.sh init payment-proxy.example.com
./scripts/deploy-production.sh validate
```

Read-only capacity smoke test (mutation endpoint tidak diizinkan):

```bash
export SERVICE_API_KEY='<local service key>'
./scripts/load-readonly.sh -requests 10000 -concurrency 100
```
