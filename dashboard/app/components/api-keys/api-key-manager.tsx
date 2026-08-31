"use client";

import { useActionState, useEffect, useState } from "react";
import {
  generateServiceAPIKeyAction,
  revokeServiceAPIKeyAction,
  type APIKeyActionState,
} from "../../actions/api-keys";
import type { ServiceAPIKey } from "../../lib/payment-proxy";

const idle: APIKeyActionState = { status: "idle", message: "" };

function formatTime(value?: string) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("id-ID", {
    day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit",
  }).format(new Date(value));
}

export function APIKeyManager({ initialKeys }: { initialKeys: ServiceAPIKey[] }) {
  const [keys, setKeys] = useState(initialKeys);
  const [oneTimeSecret, setOneTimeSecret] = useState("");
  const [copied, setCopied] = useState(false);
  const [generateState, generateAction, generatePending] = useActionState(generateServiceAPIKeyAction, idle);
  const [revokeState, revokeAction, revokePending] = useActionState(revokeServiceAPIKeyAction, idle);

  useEffect(() => {
    if (!generateState.apiKey) return;
    setKeys((current) => [generateState.apiKey!, ...current.filter((item) => item.id !== generateState.apiKey!.id)]);
    if (generateState.secret) setOneTimeSecret(generateState.secret);
  }, [generateState.apiKey, generateState.secret]);

  useEffect(() => {
    if (!revokeState.apiKey) return;
    setKeys((current) => current.map((item) => item.id === revokeState.apiKey!.id ? revokeState.apiKey! : item));
  }, [revokeState.apiKey]);

  async function copySecret() {
    if (!oneTimeSecret) return;
    await navigator.clipboard.writeText(oneTimeSecret);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  }

  const activeCount = keys.filter((item) => item.status === "ACTIVE").length;
  const busy = generatePending || revokePending;

  return (
    <section className="api-key-workspace">
      <section className="api-key-summary">
        <article><span>ACTIVE KEYS</span><strong>{activeCount}</strong><small>accepted by Internal Gateway</small></article>
        <article><span>ACCESS</span><strong>Full</strong><small>all authenticated /api/v1 routes</small></article>
        <article><span>STORAGE</span><strong>SHA-256</strong><small>plaintext is never persisted</small></article>
        <article><span>ROTATION</span><strong>No restart</strong><small>generate new, switch, then revoke old</small></article>
      </section>

      <section className="api-key-grid">
        <form className="dashboard-panel api-key-create-card" action={generateAction}>
          <div className="api-key-card-heading"><div><p>MAIN SERVICE ACCESS</p><h2>Generate API key</h2></div><span>FULL ACCESS</span></div>
          <p className="api-key-description">Gunakan key ini hanya pada server Emisell Backend untuk memanggil Payment Proxy. Jangan pernah mengirimkannya ke checkout browser.</p>
          <label>
            Nama API key
            <input name="name" minLength={3} maxLength={80} placeholder="Emisell Backend Production" autoComplete="off" required disabled={busy}/>
            <small>Gunakan nama yang menunjukkan service dan environment agar rotasi mudah diaudit.</small>
          </label>
          <div className="api-key-scope"><span>Scope</span><code>gateway:full</code><small>Provider, installation, payment methods, payment sessions, refunds, webhook operations, dan reconciliation.</small></div>
          <button className="dashboard-primary-button" type="submit" disabled={busy}>{generatePending ? "Membuat API key…" : "Generate API key"}</button>
          {generateState.message && <div className={`form-message ${generateState.status}`} role="status">{generateState.message}</div>}
        </form>

        <section className="dashboard-panel api-key-secret-card">
          <div className="api-key-card-heading"><div><p>ONE-TIME SECRET</p><h2>Credential Emisell Backend</h2></div><span className={oneTimeSecret ? "ready" : "waiting"}>{oneTimeSecret ? "SIAP DISALIN" : "MENUNGGU"}</span></div>
          {oneTimeSecret ? <div className="api-key-secret-value">
            <strong>Salin sekarang — hanya ditampilkan sekali</strong>
            <code>{oneTimeSecret}</code>
            <p>Simpan pada secret manager Emisell Backend sebagai Bearer token. Dashboard tidak dapat mengambil nilai ini kembali.</p>
            <div><button className="secondary-button" type="button" onClick={() => void copySecret()}>{copied ? "Sudah disalin" : "Salin API key"}</button><button className="secondary-button" type="button" onClick={() => setOneTimeSecret("")}>Saya sudah menyimpan</button></div>
          </div> : <div className="api-key-secret-empty"><span>••••••••••••••••</span><strong>Belum ada secret baru</strong><p>Generate key di panel kiri. Hanya metadata dan fingerprint tersamarkan yang tetap tersimpan.</p></div>}
          <div className="api-key-usage"><span>Contoh header</span><pre><code>{`Authorization: Bearer epk_...\nX-Emisell-Merchant-ID: <merchant-id>`}</code></pre></div>
        </section>
      </section>

      <section className="dashboard-panel api-key-list-panel">
        <div className="panel-heading"><div><p className="panel-kicker">ISSUED CREDENTIALS</p><h2>Service API keys</h2><p>Secret asli tidak pernah ditampilkan pada daftar ini.</p></div><span>{keys.length} total</span></div>
        <div className="api-key-list-head"><span>Name</span><span>Credential</span><span>Scope</span><span>Created</span><span>Status</span><span/></div>
        <div className="api-key-list">
          {keys.map((item) => <article className="api-key-row" key={item.id}>
            <span><strong>{item.name}</strong><small>{item.id}</small></span>
            <span><code>{item.key_hint}</code><small>secret masked</small></span>
            <span><b>{item.scopes.join(", ")}</b><small>full Internal Gateway</small></span>
            <span><strong>{formatTime(item.created_at)}</strong><small>by {item.created_by}</small></span>
            <span><b className={`status-badge ${item.status === "ACTIVE" ? "success" : "neutral"}`}><i/>{item.status}</b>{item.revoked_at && <small>{formatTime(item.revoked_at)}</small>}</span>
            <span>{item.status === "ACTIVE" && <form action={revokeAction} onSubmit={(event) => { if (!window.confirm(`Cabut akses ${item.name}? Key ini langsung berhenti bekerja.`)) event.preventDefault(); }}><input type="hidden" name="id" value={item.id}/><button className="api-key-revoke" type="submit" disabled={busy}>Revoke</button></form>}</span>
          </article>)}
        </div>
        {keys.length === 0 && <div className="management-empty api-key-empty"><span>⌁</span><h3>Belum ada API key tersimpan</h3><p>Generate key pertama untuk komunikasi Emisell Backend ke Payment Proxy.</p></div>}
        {revokeState.message && <div className={`form-message api-key-list-message ${revokeState.status}`} role="status">{revokeState.message}</div>}
      </section>

      <section className="api-key-safety-note"><strong>Rotasi tanpa downtime</strong><p>Generate key baru, pasang dan verifikasi pada Emisell Backend, lalu revoke key lama. Jangan revoke satu-satunya key aktif sebelum consumer berpindah.</p></section>
    </section>
  );
}
