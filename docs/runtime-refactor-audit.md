# Audit refactor Provider Runtime

Tanggal audit: 2026-08-31

## Kesimpulan

Project tidak perlu di-rewrite. Fondasi domain payment, persistence, keamanan
credential, idempotency, reconciliation, dan webhook sudah layak dipertahankan.
Refactor sebaiknya memisahkan orchestration Control Plane dari eksekusi Data
Plane secara bertahap, dengan kontrak connector yang sekarang sebagai seam.

Container yang ada juga sudah bersifat shared per provider: satu Xendit runner
dan satu Midtrans Provider App melayani banyak installation. Masalah utama
sebelum refactor bukan `1 merchant = 1 container`, melainkan routing Kernel yang
hanya memakai `provider_code`. Kolom `provider_version` sudah ada pada
installation, tetapi belum menjadi identitas dispatch dan belum dipin pada
payment.

## Arsitektur saat ini

```text
Emisell Backend / Dashboard
            |
            v
Go API + Payment Kernel
  - installation lifecycle
  - assignments dan normalized Payment API
  - credential encryption
  - payment/refund state
  - webhook inbox + outbox
            |
            v
in-memory connector registry (sebelumnya: provider_code)
            |
            v
remote connector client
            |
     /partner/v1/*
            |
     +------+------+
     |             |
Xendit runner   Midtrans Provider App
  (shared)          (shared)
```

Startup API menemukan runtime dari `CONNECTOR_RUNNER_BASE_URLS`. Topologi
container masih statis di Docker Compose. Publish Provider App memvalidasi
artefak dan mencatat release ke database, tetapi belum membuat, menghentikan,
atau melakukan rollout container secara otomatis.

## Bagian yang reusable

| Area | Keputusan | Alasan |
|---|---|---|
| Normalized connector contract | Pertahankan | Provider payload dan autentikasi sudah tidak bocor ke Kernel. |
| Canonical payment/refund model | Pertahankan | Status, UNKNOWN guard, dan larangan failover setelah mutation sudah benar. |
| Installation dan assignment | Pertahankan lalu rapikan service layer | Model tenant, environment, optimistic version, dan payment option sudah dapat dipakai. |
| Credential vault | Pertahankan | AES-256-GCM dengan AAD installation ID, metadata termasking, dan plaintext clearing sudah tepat. |
| Provider webhook inbox | Pertahankan | Verifikasi dilakukan di connector; dedup dan status transition dilakukan atomik di Kernel. |
| Emisell outbox worker | Pertahankan | Delivery durable, signature, retry policy, replay, dan dead-letter sudah terpisah dari ingress. |
| Provider App validation/review | Pertahankan | Artefak immutable, digest binding, static validation, dan lifecycle review merupakan Control Plane yang berguna. |
| PostgreSQL store dan migration history | Pertahankan | Data historis dan audit lebih aman dimigrasikan daripada dibuat ulang. |

## Bagian yang perlu direfactor

1. **Runtime identity dan dispatch.** Runtime harus dipilih dengan pasangan
   `(provider_code, provider_version)`, bukan provider saja. Beberapa versi yang
   sedang rollout harus dapat hidup berdampingan.
2. **Pin versi pada transaksi.** Payment lama harus tetap dibaca, dibatalkan,
   atau direkonsiliasi melalui versi runtime yang membuatnya walaupun
   installation kemudian di-upgrade.
3. **Lifecycle service eksplisit.** Handler HTTP saat ini mengorkestrasi state,
   credential, engine call, encryption, dan persistence secara langsung.
   Workflow `Install -> Configure -> Verify -> Activate` perlu dipindahkan ke
   application service dengan state machine dan test sendiri.
4. **Upgrade safety.** Upgrade installation saat ini berpindah dari `INACTIVE`
   ke `READY` tanpa verifikasi ulang credential terhadap runtime baru. Targetnya
   upgrade kembali ke `CONFIG_REQUIRED` atau `VERIFYING`, lalu baru `READY`
   setelah verification evidence baru tersimpan.
5. **Webhook version ownership.** URL webhook masih menunjuk installation dan
   memilih versi installation saat ini. Sebelum rollout multi-version penuh,
   perlu installation revision/runtime binding agar event untuk payment lama
   tidak selalu dinormalisasi oleh versi terbaru.
6. **Runtime catalog dan health.** Konfigurasi URL/token berbasis environment
   cukup untuk fase awal, tetapi perlu diganti oleh catalog runtime yang mencatat
   provider version, endpoint internal, health, digest, rollout state, dan
   waktu heartbeat.
7. **Provider App bukan Runtime Manager.** Publish saat ini hanya mengubah
   catalog/release. Eksekusi artifact, rollout, rollback, dan garbage collection
   harus menjadi komponen terpisah dengan allowlist image/digest dan tidak
   dijalankan di process API.
8. **Batas package Go.** `internal/api/server.go` dan `internal/store/postgres.go`
   sudah terlalu besar. Pisahkan per domain setelah application service ada;
   jangan memindahkan file secara mekanis sebelum boundary perilaku stabil.

Catatan audit tambahan: manifest runtime Midtrans saat ini memakai
`emisell-midtrans-v1.1.0`, sedangkan seed migration lama masih membuat
`emisell-midtrans-v1`. Fresh deployment harus memperoleh release v1.1.0 melalui
Provider App publish atau migration release yang eksplisit sebelum installation
baru dibuat.

## Target bertahap

```text
                     CONTROL PLANE
Provider catalog -> versions -> installations -> credentials -> activation
                           |
                           | RuntimeRef(provider, version)
                           v
                     Runtime Dispatcher
                           |
          +----------------+----------------+
          |                                 |
 Xendit v1 shared runtime          Xendit v2 shared runtime
          |                                 |
          +----------------+----------------+
                           |
                       provider API

                      DATA PLANE
normalized payment command -> pinned runtime -> provider -> canonical result
provider webhook -> verified normalization -> inbox -> canonical state -> outbox
```

Installation tetap menyimpan credential/configuration merchant. Ia tidak
memiliki container. Runtime dimiliki oleh provider version dan dapat melayani
banyak installation.

## Tahapan implementasi

### Tahap 0 — fondasi version-aware (dikerjakan dalam refactor ini)

- registry menerima beberapa runtime untuk provider yang sama selama versinya
  berbeda;
- lookup ambigu tanpa versi gagal tertutup;
- engine memilih connector dengan `(provider_code, provider_version)`;
- Core meneruskan version binding ke private connector contract;
- payment mem-pin `provider_version` melalui migration dan memakai versi itu
  untuk sync, cancel, refund, serta reconciliation;
- installation baru tanpa versi eksplisit memilih release `RELEASED` dari
  Control Plane database lalu memastikan runtime exact version tersedia;
- publish Provider App memeriksa runtime exact version, bukan sekadar provider.

### Tahap 1 — application services dan lifecycle

- `InstallationService` sekarang menangani install, configure + verify,
  activate, deactivate, upgrade, dan uninstall;
- endpoint configure lama tetap menjadi compatibility facade, tanpa endpoint
  verify publik tambahan: langkah Verify terjadi internal pada satu workflow;
- verification evidence disimpan atomik bersama hasil READY, mencakup runtime
  version, manifest digest, timestamp, result, actor, dan request ID;
- upgrade sekarang kembali ke `CONFIG_REQUIRED`, melepas connector binding lama,
  dan wajib melewati verifikasi ulang sebelum aktivasi.

### Tahap 2 — Runtime Dispatcher sebagai boundary

- ekstrak interface `RuntimeResolver`/`RuntimeDispatcher` dari engine;
- buat persistent runtime catalog dan health state;
- tambahkan timeout, bulkhead, circuit breaker, dan metric per provider version;
- jalankan dua versi bersamaan dan lakukan canary pada installation sandbox
  sebelum live upgrade.

Static Docker Compose masih boleh dipakai pada tahap ini. Yang penting routing
tidak lagi mengasumsikan satu versi per provider.

### Tahap 3 — pisahkan proses Control Plane dan Data Plane

- API Control Plane menangani provider, release, installation, credential, dan
  activation;
- Payment API/Data Plane hanya menerima normalized command dan immutable
  runtime binding;
- webhook ingress dan outbox worker tetap dapat diskalakan terpisah;
- gunakan service-to-service authentication dan network policy untuk semua
  private runtime endpoint.

### Tahap 4 — Runtime Manager terkontrol

- runtime deployment hanya menerima image/artifact digest yang sudah certified;
- rollout, rollback, drain, dan retirement bersifat provider-version scoped;
- jangan mematikan versi lama selama masih direferensikan payment aktif atau
  webhook retention window;
- installation upgrade tidak pernah membuat container baru.

## Guardrail migrasi

- jangan ubah normalized Payment API sekaligus;
- jangan pindahkan provider-specific logic ke Kernel;
- jangan menghapus payment, webhook, outbox, atau audit history;
- pertahankan runtime lama sampai jumlah referensi transaksi aktif nol dan
  retention webhook selesai;
- mutation timeout tetap `UNKNOWN` dan selalu direkonsiliasi pada runtime yang
  sama;
- setiap fase harus lulus unit test, race test, migration test, dan satu
  end-to-end sandbox flow sebelum fase berikutnya.
