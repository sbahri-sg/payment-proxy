# Provider Apps

Provider Apps adalah control plane untuk mengirim, memvalidasi, mensertifikasi,
dan menerbitkan versi connector baru. Konsep lifecycle mengikuti API Kurir,
tetapi artifact pembayaran tidak pernah dimuat ke process Payment Kernel.

Alurnya **provider-first**. Operator membuat identitas provider satu kali,
kemudian seluruh ZIP dan riwayat submission berada di bawah provider tersebut.
Versi `v1` dan `v1.1` tidak menjadi dua provider di dashboard.

## Boundary

```text
Dashboard
        │ create provider identity
        ▼
Provider Registry
        │ provider-bound ZIP upload (dashboard / partner CI)
        ▼
Version Registry ── immutable artifact + SHA-256 + audit
        │
        ├── static validation
        ├── conformance/security approval
        └── publish gate ── exact runtime manifest version required
                                │
                                ▼
                         Isolated Connector Runner
                                │
                                ▼
                        Native provider endpoint
```

Credential merchant tidak termasuk Provider App. Nilai credential tetap masuk
melalui Provider Installation, terenkripsi per `merchant_id`, provider, dan
environment.

## Provider registry

Provider wajib dibuat sebelum connector ZIP dapat di-upload. `provider_code`
adalah identitas permanen dan tidak berasal bebas dari setiap ZIP.

```http
POST {{admin_base_url}}/internal/v1/provider-app-providers
X-Admin-API-Key: {{admin_api_key}}
Content-Type: application/json
```

Contoh request Postman:

```json
{
  "provider_code": "midtrans",
  "provider_name": "Midtrans",
  "description": "Midtrans payment connector for Emisell merchants.",
  "website_url": "https://midtrans.com",
  "documentation_url": "https://docs.midtrans.com",
  "support_email": "support@midtrans.com"
}
```

Contoh response `201 Created`:

```json
{
  "data": {
    "provider_code": "midtrans",
    "provider_name": "Midtrans",
    "description": "Midtrans payment connector for Emisell merchants.",
    "status": "DRAFT",
    "version_count": 0,
    "created_by": "payment-proxy-admin",
    "updated_by": "payment-proxy-admin"
  }
}
```

Saat upload, validator memastikan `manifest.code` dan `manifest.name` sama
dengan provider pada URL. Bundle tidak dapat menyamar sebagai provider lain.

## Bundle contract

ZIP maksimum 25 MB dan maksimum 128 file. Expanded size maksimum 64 MB. Bundle
wajib memiliki:

```text
provider-connector.zip
├── manifest.json
├── connector
└── checksums.txt
```

Contoh minimal untuk belajar validator tersedia di `docs/examples/provider-app`.
Sumber Provider App Midtrans yang benar berada di `provider-apps/midtrans` dan
bundle release dibangun memakai `./scripts/build-midtrans-provider-app.sh`.

`checksums.txt` menggunakan format:

```text
<64-character-sha256>  manifest.json
<64-character-sha256>  connector
```

Semua file selain `checksums.txt` harus tercantum tepat satu kali. Path absolut,
path traversal, symlink, duplicate entry, checksum mismatch, host berupa IP,
dan runtime `native_go` akan ditolak.

## Lifecycle

```text
UPLOADED → VALIDATED → CERTIFIED → PUBLISHED → DEPRECATED
    └───────────────→ DISABLED ←──────────────┘
```

- `UPLOADED`: ZIP aman untuk disimpan dan manifest dapat dibaca.
- `VALIDATED`: artifact dibaca ulang dari database dan digest/contract cocok.
- `CERTIFIED`: operator mencatat conformance dan security approval.
- `PUBLISHED`: hanya dapat dicapai jika runtime memuat provider dan versi yang
  sama persis. Metadata provider/version baru kemudian dirilis.
- `DEPRECATED`: release lama tidak ditawarkan untuk instalasi baru.
- `DISABLED`: artifact ditahan dari lifecycle selanjutnya.

Versi lama tidak otomatis mengganti installation aktif. Upgrade installation
akan menjadi operasi eksplisit setelah isolated runner tersedia.

## Postman: upload

```http
POST {{admin_base_url}}/internal/v1/provider-app-providers/{{provider_app_code}}/versions
X-Admin-API-Key: {{admin_api_key}}
Content-Type: multipart/form-data
```

Pada **Body → form-data**:

| Key | Type | Value |
|---|---|---|
| `bundle` | File | `midtrans-connector-emisell-v1.1.0.zip` |

Contoh response `201 Created`:

```json
{
  "data": {
    "id": "papp_9d3a...",
    "provider_code": "midtrans",
    "provider_name": "Midtrans",
    "version": "emisell-midtrans-v1.1.0",
    "status": "UPLOADED",
    "runtime": "isolated_container",
    "sdk_version": "v1",
    "file_name": "midtrans-connector-emisell-v1.1.0.zip",
    "artifact_size": 1843200,
    "artifact_sha256": "a791f4...9d2c",
    "manifest": {
      "contract_version": "v1",
      "code": "midtrans",
      "version": "emisell-midtrans-v1.1.0",
      "entrypoint": "connector",
      "environments": ["sandbox", "live"],
      "payment_methods": ["qris", "va_bca", "va_mandiri", "va_bni", "va_bri", "va_permata", "va_cimb", "ewallet_gopay", "ewallet_shopeepay"],
      "outbound_hosts": ["api.midtrans.com", "api.sandbox.midtrans.com"]
    },
    "scan_report": {
      "passed": true,
      "file_count": 3,
      "checks": [
        { "code": "archive_safety", "status": "PASSED" },
        { "code": "manifest_contract", "status": "PASSED" },
        { "code": "artifact_checksums", "status": "PASSED" },
        { "code": "credential_separation", "status": "PASSED" }
      ]
    }
  }
}
```

## Postman: transition

```http
POST {{admin_base_url}}/internal/v1/provider-apps/{{provider_app_id}}/transition
X-Admin-API-Key: {{admin_api_key}}
Content-Type: application/json
```

Validate:

```json
{
  "expected_status": "UPLOADED",
  "status": "VALIDATED",
  "review_note": ""
}
```

Certify:

```json
{
  "expected_status": "VALIDATED",
  "status": "CERTIFIED",
  "review_note": "Sandbox conformance and security review passed."
}
```

Publish:

```json
{
  "expected_status": "CERTIFIED",
  "status": "PUBLISHED",
  "review_note": "Isolated runtime emisell-midtrans-v1.1.0 deployed and health checked."
}
```

Publish sebelum runtime siap menghasilkan:

```json
{
  "error": {
    "code": "CONNECTOR_RUNTIME_NOT_READY",
    "message": "deploy the isolated connector runtime with this exact manifest version before publish"
  }
}
```

## Security gates

- Admin API authentication dan dashboard operator session.
- Strict multipart and request-size limits.
- ZIP path traversal, symlink, duplicate path, file count, and zip-bomb guard.
- SHA-256 untuk archive dan setiap file.
- Artifact immutable; list API tidak mengembalikan binary.
- Unique provider/version dan storage quota 250 MB per provider.
- Audit actor, request ID, digest, transition, dan review note.
- Credential schema only; tidak ada credential value pada bundle.
- Publish memerlukan exact runtime version dan SHA-256 executable runtime harus
  sama dengan entrypoint di ZIP yang sudah divalidasi.

Setelah release baru dipublish, installation lama tidak berubah otomatis:

```text
ACTIVE → deactivate → INACTIVE → upgrade → READY → activate → ACTIVE
```

`POST /api/v1/provider-installations/{id}/upgrade` menerima optimistic
`version` dan `provider_version` target. Target harus `RELEASED` dan sama dengan
manifest runtime yang sedang berjalan.

Cryptographic partner signing, malware scanner, isolated runner deployment,
resource limit, dan outbound network enforcement tetap merupakan production
gate sebelum connector pihak ketiga dapat menjalankan transaksi.
