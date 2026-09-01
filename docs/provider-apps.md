# Provider Apps

Provider Apps adalah control plane untuk mengirim, memvalidasi, memverifikasi,
dan menerbitkan versi connector baru. Konsep lifecycle mengikuti API Kurir:
ZIP adalah paket submission untuk review, bukan runtime yang dieksekusi.

Alurnya **provider-first**. Operator membuat identitas provider satu kali,
kemudian seluruh ZIP dan riwayat submission berada di bawah provider tersebut.
Versi `v1` dan `v1.1` tidak menjadi dua provider di dashboard.

## Boundary

```text
Dashboard
        │ create provider identity
        ▼
Provider Registry
        │ provider-bound submission ZIP (dashboard / partner CI)
        ▼
Version Registry ── immutable artifact + SHA-256 + audit
        │
        ├── static validation
        ├── automated backend runtime verification
        └── publish gate ── exact runtime manifest version + digest required
                                │
                                ▼
                  Runtime Dispatcher (provider + version)
                                │
                                ▼
                  Shared isolated provider runtime
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
POST {{base_url}}/api/v1/admin/provider-app-providers
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
    "provider_code": "xendit",
    "provider_name": "Xendit",
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

## Submission contract v1

ZIP maksimum 25 MB dan maksimum 128 file. Expanded size maksimum 64 MB.
Seperti API Kurir, submission wajib memiliki dua file kontrak pada root:

```text
provider-submission.zip
├── emisell-extension.yaml
├── openapi.yaml
├── README.md
├── SECURITY.md
└── contract-tests/
```

Sumber submission Xendit dan Midtrans berada di `provider-apps/<provider>` dan
dibangun dengan `./scripts/build-<provider>-provider-app.sh`. Hasilnya kecil dan
portable karena tidak berisi binary Linux. Runtime provider dibangun terpisah
sebagai OCI image dari `backend/Dockerfile`, lalu dideploy satu kali untuk setiap
versi provider.

Path absolut, path traversal, symlink, duplicate entry, zip bomb, native binary,
file `.env`, private key, pola secret, host berupa IP, dan runtime `native_go`
akan ditolak. `openapi.yaml` harus memakai OpenAPI 3.x dan mendeklarasikan semua
operasi canonical yang dipilih manifest.

Bundle lama (`manifest.json + connector + checksums.txt`) masih diterima sebagai
`legacy_runtime_bundle` selama masa transisi. Submission baru wajib memakai
`provider_submission_v1`; builder baru tidak lagi menghasilkan bundle binary.

## Lifecycle

```text
UPLOADED → VALIDATED → CERTIFIED → PUBLISHED → DEPRECATED
    └───────────────→ DISABLED ←──────────────┘
```

- `UPLOADED`: ZIP aman untuk disimpan dan manifest dapat dibaca.
- `VALIDATED`: artifact dibaca ulang dari database dan digest/contract cocok.
- `CERTIFIED`: status storage kompatibel untuk release yang otomatis lolos
  verifikasi backend. Dashboard menampilkannya sebagai `VERIFIED`; operator
  tidak mengisi hasil uji per metode secara manual.
- `PUBLISHED`: hanya dapat dicapai jika shared runtime memuat provider dan versi
  yang sama persis serta melaporkan digest executable immutable. Digest runtime
  disimpan di release registry, terpisah dari digest ZIP submission.
- `DEPRECATED`: release lama tidak ditawarkan untuk instalasi baru.
- `DISABLED`: artifact ditahan dari lifecycle selanjutnya.

Versi lama tidak otomatis mengganti installation aktif. Upgrade installation
akan menjadi operasi eksplisit setelah isolated runner tersedia.

## Postman: upload

```http
POST {{base_url}}/api/v1/admin/provider-app-providers/{{provider_app_code}}/versions
X-Admin-API-Key: {{admin_api_key}}
Content-Type: multipart/form-data
```

Pada **Body → form-data**:

| Key | Type | Value |
|---|---|---|
| `bundle` | File | `xendit-provider-app-emisell-v1.1.0.zip` |

Contoh response `201 Created`:

```json
{
  "data": {
    "id": "papp_9d3a...",
    "provider_code": "midtrans",
    "provider_name": "Midtrans",
    "version": "emisell-xendit-v1.1.0",
    "status": "UPLOADED",
    "runtime": "isolated_container",
    "sdk_version": "v1",
    "file_name": "xendit-provider-app-emisell-v1.1.0.zip",
    "artifact_size": 11220,
    "artifact_sha256": "a791f4...9d2c",
    "manifest": {
      "package_format": "provider_submission_v1",
      "contract_version": "v1",
      "code": "xendit",
      "version": "emisell-xendit-v1.1.0",
      "environments": ["sandbox", "live"],
      "payment_methods": ["qris", "card", "va_bca", "va_mandiri", "ewallet_ovo", "ewallet_dana"],
      "outbound_hosts": ["api.xendit.co"]
    },
    "scan_report": {
      "passed": true,
      "package_format": "provider_submission_v1",
      "file_count": 5,
      "checks": [
        { "code": "archive_safety", "status": "PASSED" },
        { "code": "source_only", "status": "PASSED" },
        { "code": "manifest_contract", "status": "PASSED" },
        { "code": "openapi_contract", "status": "PASSED" },
        { "code": "credential_separation", "status": "PASSED" }
      ]
    }
  }
}
```

## Postman: transition

```http
POST {{base_url}}/api/v1/admin/provider-apps/{{provider_app_id}}/transition
X-Admin-API-Key: {{admin_api_key}}
Content-Type: application/json
```

Validate:

```json
{
  "expected_status": "UPLOADED",
  "status": "VALIDATED"
}
```

Backend verify:

```json
{
  "expected_status": "VALIDATED",
  "status": "CERTIFIED"
}
```

Pada transisi ini backend membaca ulang ZIP dan mencocokkannya dengan shared
runtime exact version: provider identity, immutable runtime digest, operations,
credential schema, automated test profiles, dan canonical payment-method
mapping. Hasilnya disimpan pada `verification_report` sebagai evidence
read-only. Credential merchant tidak dipakai.

Publish:

```json
{
  "expected_status": "CERTIFIED",
  "status": "PUBLISHED"
}
```

SHA-256 tidak diketik operator. Backend membacanya dari shared runtime,
mencocokkannya dengan verification report, lalu membuat audit note otomatis.

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
- SHA-256 immutable untuk archive submission dan digest terpisah untuk runtime.
- Artifact immutable; list API tidak mengembalikan binary.
- Unique provider/version dan storage quota 250 MB per provider.
- Audit actor, request ID, digest, transition, automated verification report,
  dan automated publish note.
- Credential schema only; tidak ada credential value pada bundle.
- Publish memerlukan exact runtime version dan digest executable runtime.
- Legacy bundle tetap memerlukan SHA-256 runtime sama dengan entrypoint ZIP;
  submission v1 tidak memiliki entrypoint karena runtime dikelola terpisah.

Setelah release baru dipublish, installation lama tidak berubah otomatis:

```text
ACTIVE → deactivate → INACTIVE → upgrade → CONFIG_REQUIRED → configure/verify → READY → activate → ACTIVE
```

`POST /api/v1/provider-installations/{id}/upgrade` menerima optimistic
`version` dan `provider_version` target. Target harus `RELEASED` dan sama dengan
manifest runtime yang sedang berjalan.

Cryptographic partner signing, malware scanner, isolated runner deployment,
resource limit, dan outbound network enforcement tetap merupakan production
gate sebelum connector pihak ketiga dapat menjalankan transaksi.
