# Dashboard operations roadmap

Dashboard mengikuti domain Emisell Payment Engine. Halaman tidak boleh memanggil service API secara langsung atau membawa provider credential ke browser.

## Menu

| Group | Menu | Status | Scope |
|---|---|---|---|
| Workspace | Overview | Available | KPI 24 jam, grafik volume 7 hari, status distribution, provider health, recent payments, operational backlog |
| Workspace | Payments | Available | List/filter, detail payment, QRIS next action, durable timeline, sync dan cancel terkontrol |
| Connections | Providers | Available | Registry, verified capability, release, health dan install entry point |
| Connections | Installations | Available | Sandbox/live lifecycle, configure, verify, activate, deactivate, uninstall |
| Operations | Reconciliation | Available | Queue exception tenant-scoped, lookup langsung ke provider, optimistic counter, idempotency dan audited resolution |
| Operations | Webhooks | Available | Provider inbox dan Emisell Backend outbox dipisahkan; destination aktif, contract version, legacy payload, delivery attempts, deduplication dan replay DEAD ditampilkan |
| Developers | API documentation | Available | Terintegrasi dalam shell dashboard; endpoint accordion, request Postman, response, collection dan conformance |

## Analytics definitions

- Successful volume: jumlah `amount` payment `SUCCEEDED`; IDR disimpan dalam minor unit dan dibagi 100 saat ditampilkan.
- Payment attempts: seluruh payment yang dibuat dalam rentang waktu.
- Success rate: `SUCCEEDED / terminal`, dengan terminal = `SUCCEEDED + FAILED + CANCELLED + EXPIRED`.
- Webhook success rate: event `PROCESSED` atau `IGNORED` dibanding event selesai diproses.
- Operational backlog: payment `UNKNOWN`, outbox pending/dead, dan webhook failed.

Overview membaca data nyata melalui `GET /api/v1/admin/dashboard/overview`. Endpoint memerlukan `X-Admin-API-Key`; hanya Next.js server yang mengirim header tersebut. Browser hanya menerima hasil render dan tidak menerima admin key.

## Delivery phases

1. Foundation: Overview analytics, health, backend provider verification, API docs, operator session.
2. Operations: Providers, Installations, Payments, Webhooks, dan Reconciliation.
3. Extensions: Midtrans, DOKU, Duitku, lalu penambahan capability per connector berdasarkan conformance.

Mutation provider dan payment dilindungi signed HttpOnly operator session, same-site policy, Next.js Server Action origin validation, fixed tenant server configuration, audit actor, idempotency, guard status, optimistic version, dan konfirmasi aksi berisiko. Credential tidak masuk browser storage dan service key tidak dikirim ke client. Sebelum production multi-user, perluasan berikutnya adalah identity provider, granular RBAC, serta approval dua langkah untuk aksi live.
