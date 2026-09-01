# Midtrans connector conformance

Status: **IMPLEMENTED · BCA/BNI/PERMATA VA SANDBOX CERTIFIED**

Connector: `emisell-midtrans-v2.0.1`
Runtime: isolated Midtrans Provider App container

Dokumen ini membedakan implementasi kode dari bukti bahwa account Midtrans
merchant benar-benar dapat mengeksekusi suatu channel. Provider berstatus
available berarti connector dapat di-install. Capability `DOCUMENTED` maupun
`CERTIFIED` boleh di-assign; status `CERTIFIED` hanya menandai bahwa sandbox run
Emisell sudah mempunyai evidence lengkap.

## Scope implementasi awal

| Canonical method | Midtrans Core API | Runtime |
|---|---|---|
| `qris` | `payment_type=qris`, acquirer GoPay | Implemented, provider activation pending |
| `va_bca` | `payment_type=bank_transfer`, bank BCA | Implemented, sandbox certified |
| `va_bni` | `payment_type=bank_transfer`, bank BNI | Implemented, sandbox certified |
| `va_bri`, `va_cimb` | `payment_type=bank_transfer` | Implemented, provider activation pending |
| `va_mandiri` | `payment_type=echannel` | Implemented, certification pending |
| `va_permata` | `payment_type=permata` | Implemented, sandbox certified |
| `ewallet_gopay` | `payment_type=gopay` | Implemented, provider activation pending |
| `ewallet_shopeepay` | `payment_type=shopeepay` | Implemented, provider activation pending |

Card, Danamon VA, BSI VA, OVO, DANA, retail, dan paylater tetap tercatat sebagai
informasi katalog tetapi ditandai `CONNECTOR_METHOD_NOT_IMPLEMENTED`. Danamon
Core API standar tidak disamakan dengan varian BI-SNAP. Connector tidak menebak
payload untuk channel yang berbeda kontrak.

## Credential dan endpoint

- Sandbox base: `https://api.sandbox.midtrans.com`
- Live base: `https://api.midtrans.com`
- Credential installation: `server_key` wajib dan `pop_id` opsional. Client Key
  tidak dibutuhkan untuk server-to-server Core API.
- `pop_id` dikirim sebagai header `X-POP-ID` bila Midtrans memang menerbitkannya
  untuk merchant. Nilai ini tidak boleh ditebak dari Merchant ID.
- Authentication provider: HTTP Basic, Server Key sebagai username dan password
  kosong.
- Provider notification URL:

```text
https://<payment-domain>/webhooks/v1/providers/midtrans/<installation-id>
```

Referensi resmi: [Core API bank transfer](https://docs.midtrans.com/docs/coreapi-core-api-bank-transfer-integration),
[QRIS Charge API](https://docs.midtrans.com/reference/qris), dan
[notification signature](https://docs.midtrans.com/reference/receiving-notifications).

## Historical sandbox evidence

1. Buat installation Midtrans environment `sandbox` memakai Merchant ID Emisell.
2. Isi Server Key sandbox. Plaintext hanya melewati request, lalu disimpan
   sebagai ciphertext AES-256-GCM.
3. Pastikan `PAYMENT_PROXY_PUBLIC_BASE_URL` memakai HTTPS publik. Connector
   mengirim `X-Override-Notification` per transaksi ke endpoint installation di
   atas, sehingga testing tidak bergantung pada satu URL global dashboard.
4. Activate installation.
5. Jalankan diagnostic endpoint internal dari CI/operator pada tenant sandbox.
6. Buka payment detail dan selesaikan QR, VA, atau redirect pada simulator
   Midtrans. Run awal tetap `BLOCKED`; jangan membuat payment pengganti.
7. Resume diagnostic dengan `payment_id` yang sama.
8. Capability baru menjadi `CERTIFIED` hanya jika status provider sukses,
   signature webhook valid, inbox terproses, dan signed outbox ke Emisell Backend
   berstatus delivered.

Midtrans membentuk `signature_key` sebagai SHA-512 dari `order_id + status_code +
gross_amount + server_key`. Connector membandingkannya secara constant-time.
Payload mentah provider tidak diteruskan ke Emisell Backend.

## Evidence release Provider App 1 September 2026

Artifact `emisell-midtrans-v2.0.1` dibangun sebagai ZIP immutable dan dijalankan
sebagai container tersendiri. ZIP submission berisi source review, sedangkan
digest executable runtime dicatat dan diverifikasi secara terpisah saat publish.
BCA VA menghasilkan webhook `pending` dan `settlement` langsung melalui
override notification URL; certification run `cert_3d38639c09440121510415f65cc93e62`
berstatus `PASSED`, termasuk direct provider webhook dan signed delivery ke
Emisell Backend. Provider App kemudian dipublish dan installation sandbox
di-upgrade secara eksplisit ke v2.0.1.

## Evidence sandbox 28 Agustus 2026

Installation sandbox yang diuji berhasil menyelesaikan BCA, BNI, dan Permata
Virtual Account melalui simulator resmi Midtrans. Evidence yang tersimpan mencakup create dan retrieve,
customer authorization, webhook `pending`, webhook terminal `settlement` yang
dinormalisasi menjadi `SUCCEEDED`, serta signed outbox berstatus `DELIVERED`
dengan response Emisell HTTP 202. Capability `va_bca` kemudian berubah menjadi
`CERTIFIED` untuk ketiga metode tersebut.

Core API account ini menolak QRIS/GoPay karena PoP provisioning dan menolak
BRI/CIMB/ShopeePay karena channel belum aktif. Snap Preference API juga menolak
aktivasi BRI VA, CIMB VA, ShopeePay, dan Other QRIS sebagai channel yang invalid
untuk merchant ini. Sesi Snap GoPay/QRIS yang sempat dibuat nyata-nyata
menampilkan tidak ada channel pembayaran, sehingga fallback otomatis tidak
dipakai. Sandbox dan live sama-sama fail-closed sampai aktivasi channel/PoP
resmi diselesaikan oleh Midtrans.

Connector memperlakukan
`status_code` 4xx/5xx di body sebagai error walaupun transport HTTP bernilai 200;
response seperti ini tidak boleh menghasilkan payment lokal palsu berstatus
`PENDING`.

Gate certification hanya menerima webhook terminal `SUCCEEDED` dan delivery
Emisell dengan status canonical yang sama. Webhook `PENDING` tidak dapat dipakai
sebagai bukti kelulusan setelah status provider direkonsiliasi lewat GET Status.

## Operation yang masih ditutup

Adapter cancel dan refund mempunyai unit/contract test, termasuk `refund_key`
stabil dan aturan outcome ambigu. Operation tersebut belum dicantumkan pada
manifest runtime sampai real sandbox evidence untuk status, duplicate retry,
webhook, dan failure mode tersimpan. Capture dan card juga belum tersedia.
