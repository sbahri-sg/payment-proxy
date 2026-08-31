const requestHeaders = `POST /webhooks/v1/payment-proxy HTTP/1.1
Content-Type: application/json
X-Emisell-Webhook-ID: evt_01k3...
X-Emisell-Webhook-Timestamp: 1787909400
X-Emisell-Webhook-Signature: v1=<hex-hmac-sha256>
X-Emisell-Webhook-Version: 1
X-Emisell-Event-Type: payment.updated
X-Emisell-Merchant-ID: merchant_123
Idempotency-Key: evt_01k3...`;

const paymentPayload = `{
  "id": "evt_01k3...",
  "object": "event",
  "api_version": "2026-08-28",
  "type": "payment.updated",
  "created_at": "2026-08-28T10:10:00Z",
  "merchant_id": "merchant_123",
  "resource": {
    "type": "payment",
    "id": "pay_01k3..."
  },
  "data": {
    "payment": {
      "id": "pay_01k3...",
      "merchant_reference": "order_2026_0001",
      "amount": 1000000,
      "currency": "IDR",
      "environment": "sandbox",
      "status": "SUCCEEDED",
      "updated_at": "2026-08-28T10:10:00Z"
    },
    "previous_status": "PENDING"
  }
}`;

const refundPayload = `{
  "id": "evt_01k4...",
  "object": "event",
  "api_version": "2026-08-28",
  "type": "refund.updated",
  "created_at": "2026-08-28T10:15:00Z",
  "merchant_id": "merchant_123",
  "resource": {
    "type": "refund",
    "id": "ref_01k3..."
  },
  "data": {
    "refund": {
      "id": "ref_01k3...",
      "payment_id": "pay_01k3...",
      "amount": 50000,
      "currency": "IDR",
      "status": "SUCCEEDED",
      "updated_at": "2026-08-28T10:15:00Z"
    }
  }
}`;

const signatureVerification = `import { createHmac, timingSafeEqual } from "node:crypto";

export function verifyPaymentProxyWebhook(rawBody, headers, secret) {
  const eventId = headers["x-emisell-webhook-id"];
  const eventType = headers["x-emisell-event-type"];
  const merchantId = headers["x-emisell-merchant-id"];
  const timestamp = headers["x-emisell-webhook-timestamp"];
  const received = headers["x-emisell-webhook-signature"];
  const timestampNumber = Number(timestamp);

  if (!eventId || !eventType || !merchantId ||
      !timestamp || !received ||
      !Number.isFinite(timestampNumber) ||
      Math.abs(Date.now() / 1000 - timestampNumber) > 300) {
    return false;
  }

  const expected = "v1=" + createHmac("sha256", secret)
    .update(timestamp + ".")
    .update(rawBody)
    .digest("hex");
  const left = Buffer.from(received);
  const right = Buffer.from(expected);

  return left.length === right.length && timingSafeEqual(left, right);
}

// rawBody wajib Buffer asli sebelum JSON.parse().`;

const receiverFlow = `// Setelah signature valid dan body selesai divalidasi:
const event = JSON.parse(rawBody.toString("utf8"));

if (event.id !== headers["x-emisell-webhook-id"] ||
    event.type !== headers["x-emisell-event-type"] ||
    event.merchant_id !== headers["x-emisell-merchant-id"]) {
  return response.status(400).json({ error: "event identity mismatch" });
}

// Transaksi database: insert event secara idempotent + enqueue internal job.
const result = await storeWebhookOnce({
  id: event.id,
  merchantId: event.merchant_id,
  type: event.type,
  payloadSha256: sha256(rawBody),
  payload: event,
});

if (result.conflict) {
  return response.status(409).json({ error: "event id payload conflict" });
}

return response.status(result.duplicate ? 200 : 202).json({
  accepted: true,
  duplicate: result.duplicate,
  event_id: event.id,
});`;

function CodeExample({ title, content }: { title: string; content: string }) {
  return (
    <section className="emisell-contract-code">
      <h4>{title}</h4>
      <pre><code>{content}</code></pre>
    </section>
  );
}

export function EmisellWebhookContractGuide({
  destination = "https://api.emisell.com/webhooks/v1/payment-proxy",
  defaultOpen = false,
}: {
  destination?: string;
  defaultOpen?: boolean;
}) {
  return (
    <section className="emisell-contract-guide" id="emisell-contract">
      <header className="emisell-contract-heading">
        <div>
          <p className="label">EMISELL BACKEND INTEGRATION</p>
          <h3>Kontrak penerima webhook Payment Proxy</h3>
          <p>
            Endpoint ini dibuat di backend Emisell. Payment Proxy mengirim event
            canonical dari durable outbox; payload mentah Xendit, Midtrans, DOKU,
            atau provider lain tidak pernah diteruskan langsung.
          </p>
        </div>
        <span>API version 2026-08-28</span>
      </header>

      <div className="emisell-contract-destination">
        <small>ACTIVE RECEIVER TARGET</small>
        <code>{destination}</code>
      </div>

      <div className="emisell-contract-flow" aria-label="Outbound webhook flow">
        <article><span>1</span><div><strong>Provider event</strong><small>Xendit mengirim status ke endpoint installation Payment Proxy.</small></div></article>
        <article><span>2</span><div><strong>Normalize + commit</strong><small>Kernel memetakan status dan menyimpan projection bersama outbox.</small></div></article>
        <article><span>3</span><div><strong>Sign + deliver</strong><small>Worker menandatangani raw body dengan HMAC SHA-256.</small></div></article>
        <article><span>4</span><div><strong>Emisell consumes</strong><small>Backend menyimpan event sekali, lalu memproses order secara asynchronous.</small></div></article>
      </div>

      <div className="emisell-contract-events">
        <article>
          <code>payment.updated</code>
          <p>Dikirim setelah webhook provider mengubah projection payment canonical.</p>
          <small>Gunakan <code>data.payment.status</code> untuk sinkronisasi pembayaran/order.</small>
        </article>
        <article>
          <code>refund.updated</code>
          <p>Dikirim setelah status refund berhasil dipetakan ke refund milik Emisell.</p>
          <small>Hubungkan kembali melalui <code>data.refund.payment_id</code>.</small>
        </article>
      </div>

      <div className="emisell-contract-accordions">
        <details className="emisell-contract-accordion" open={defaultOpen}>
          <summary><span><b>1</b><strong>Header, payload payment, dan arti field</strong></span><i>⌄</i></summary>
          <div className="emisell-contract-body">
            <div className="emisell-contract-code-grid">
              <CodeExample title="HTTP request headers" content={requestHeaders} />
              <CodeExample title="Payload payment.updated" content={paymentPayload} />
            </div>
            <div className="emisell-field-grid">
              <article><code>id</code><p>ID event immutable. Harus sama dengan <code>X-Emisell-Webhook-ID</code> dan <code>Idempotency-Key</code>.</p></article>
              <article><code>api_version</code><p>Versi schema payload, bukan versi provider. Tolak versi yang belum didukung.</p></article>
              <article><code>type</code><p>Jenis event canonical. Harus sama dengan <code>X-Emisell-Event-Type</code>.</p></article>
              <article><code>created_at</code><p>Waktu event dibuat setelah perubahan provider berhasil disimpan oleh Payment Proxy.</p></article>
              <article><code>merchant_id</code><p>Tenant tujuan. Harus sama dengan header dan wajib dicocokkan ke merchant Emisell.</p></article>
              <article><code>resource</code><p>Pointer ke resource Payment Proxy. Tipe dan ID harus sama dengan object di dalam <code>data</code>.</p></article>
              <article><code>merchant_reference</code><p>Referensi order yang sebelumnya dikirim Emisell ketika membuat payment.</p></article>
              <article><code>previous_status</code><p>Status projection sebelum event ini; berguna untuk audit, bukan pengganti validasi transisi lokal.</p></article>
              <article><code>updated_at</code><p>Penentu urutan projection. Abaikan event yang lebih lama dari versi yang sudah diterapkan.</p></article>
              <article><code>environment</code><p><code>sandbox</code> dan <code>live</code> wajib dipisahkan pada data serta proses bisnis.</p></article>
            </div>
          </div>
        </details>

        <details className="emisell-contract-accordion">
          <summary><span><b>2</b><strong>Payload refund dan status canonical</strong></span><i>⌄</i></summary>
          <div className="emisell-contract-body">
            <div className="emisell-contract-code-grid single">
              <CodeExample title="Payload refund.updated" content={refundPayload} />
            </div>
            <div className="emisell-status-groups">
              <article>
                <strong>Payment status</strong>
                <p><code>CREATED</code> <code>PROCESSING</code> <code>PENDING</code> <code>SUCCEEDED</code> <code>FAILED</code> <code>CANCELLED</code> <code>EXPIRED</code> <code>UNKNOWN</code></p>
              </article>
              <article>
                <strong>Refund status</strong>
                <p><code>PENDING</code> <code>SUCCEEDED</code> <code>FAILED</code> <code>UNKNOWN</code></p>
              </article>
            </div>
            <div className="emisell-contract-warning">
              <strong>Aturan order Emisell</strong>
              <p><code>SUCCEEDED</code> boleh menandai pembayaran berhasil. <code>FAILED</code>, <code>CANCELLED</code>, dan <code>EXPIRED</code> hanya diterapkan jika event tidak lebih lama. <code>UNKNOWN</code> wajib masuk rekonsiliasi dan tidak boleh dianggap gagal atau memicu gateway lain otomatis.</p>
            </div>
          </div>
        </details>

        <details className="emisell-contract-accordion">
          <summary><span><b>3</b><strong>Verifikasi HMAC dan receiver idempotent</strong></span><i>⌄</i></summary>
          <div className="emisell-contract-body">
            <div className="emisell-contract-code-grid">
              <CodeExample title="Verifikasi signature · Node.js" content={signatureVerification} />
              <CodeExample title="Simpan durable sebelum membalas" content={receiverFlow} />
            </div>
            <ol className="emisell-contract-rules">
              <li>Pertahankan byte <code>raw request body</code>; jangan re-serialize JSON sebelum menghitung signature.</li>
              <li>Hitung <code>HMAC-SHA256(secret, timestamp + "." + rawBody)</code> dan bandingkan secara constant-time.</li>
              <li>Tolak timestamp dengan selisih lebih dari lima menit dan tolak semua identity mismatch antara header/body.</li>
              <li>Simpan hash payload bersama event ID. Duplikat identik dibalas 200; ID sama dengan payload berbeda dibalas 409.</li>
              <li>Simpan event dan antrean internal dalam satu transaksi, baru balas 202. Pemrosesan order yang berat dilakukan asynchronous.</li>
            </ol>
          </div>
        </details>

        <details className="emisell-contract-accordion">
          <summary><span><b>4</b><strong>Response, retry, dan operasional</strong></span><i>⌄</i></summary>
          <div className="emisell-contract-body">
            <div className="emisell-response-grid">
              <article><strong>202 Accepted</strong><p>Event baru sudah tersimpan durable dan masuk antrean internal.</p></article>
              <article><strong>200 OK</strong><p>Event identik pernah diterima. Tidak diproses dua kali.</p></article>
              <article><strong>400 / 401 / 409</strong><p>Schema, signature, atau identity conflict. Delivery menjadi dead-letter tanpa retry agresif.</p></article>
              <article><strong>408 / 425 / 429 / 5xx</strong><p>Payment Proxy mengulang dengan exponential backoff, jitter, maksimal delapan attempt; <code>Retry-After</code> dihormati sampai satu jam.</p></article>
            </div>
            <div className="emisell-contract-warning">
              <strong>Production checklist</strong>
              <p>Gunakan HTTPS publik, secret minimal 32 karakter dari secret manager, raw-body middleware khusus route ini, unique index pada event ID, queue internal, monitoring dead-letter, dan alert ketika delivery tertunda. Jangan meletakkan secret di browser, repository, log, atau database merchant.</p>
            </div>
          </div>
        </details>
      </div>
    </section>
  );
}
