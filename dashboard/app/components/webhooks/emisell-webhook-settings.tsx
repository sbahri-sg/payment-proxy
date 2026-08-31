"use client";

import { useActionState, useEffect, useState } from "react";
import {
  generateEmisellWebhookSecretAction,
  saveEmisellWebhookSettingsAction,
  testEmisellWebhookAction,
  type WebhookSettingsActionState,
} from "../../actions/webhook-settings";
import type { EmisellWebhookSettings } from "../../lib/payment-proxy";

const idle: WebhookSettingsActionState = { status: "idle", message: "" };

function formatTime(value?: string) {
  if (!value) return "Belum pernah diuji";
  return new Intl.DateTimeFormat("id-ID", {
    day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit",
  }).format(new Date(value));
}

export function EmisellWebhookSettingsPanel({ initialSettings }: { initialSettings: EmisellWebhookSettings }) {
  const [settings, setSettings] = useState(initialSettings);
  const [callbackURL, setCallbackURL] = useState(initialSettings.callback_url);
  const [enabled, setEnabled] = useState(initialSettings.enabled);
  const [oneTimeSecret, setOneTimeSecret] = useState("");
  const [copied, setCopied] = useState(false);
  const [saveState, saveAction, savePending] = useActionState(saveEmisellWebhookSettingsAction, idle);
  const [generateState, generateAction, generatePending] = useActionState(generateEmisellWebhookSecretAction, idle);
  const [testState, testAction, testPending] = useActionState(testEmisellWebhookAction, idle);

  useEffect(() => {
    if (!saveState.settings) return;
    setSettings(saveState.settings);
    setCallbackURL(saveState.settings.callback_url);
    setEnabled(saveState.settings.enabled);
  }, [saveState.settings]);

  useEffect(() => {
    if (!generateState.settings) return;
    setSettings(generateState.settings);
    setEnabled(false);
    if (generateState.secret) setOneTimeSecret(generateState.secret);
  }, [generateState.settings, generateState.secret]);

  async function copySecret() {
    if (!oneTimeSecret) return;
    await navigator.clipboard.writeText(oneTimeSecret);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  }

  const test = testState.test;
  const lastTestSuccess = test?.success ?? settings.last_test_success;
  const lastTestAt = test?.tested_at ?? settings.last_test_at;
  const canTest = Boolean(callbackURL.trim() && settings.secret_configured);
  const busy = savePending || generatePending || testPending;

  return (
    <section className="emisell-settings-panel" aria-labelledby="emisell-settings-title">
      <header className="dashboard-panel emisell-settings-summary">
        <div>
          <p>OUTBOUND CONNECTION</p>
          <h2 id="emisell-settings-title">Emisell Backend connection</h2>
          <span>Atur endpoint penerima, signing secret, dan status pengiriman webhook.</span>
        </div>
        <aside>
          <b className={settings.enabled ? "active" : "inactive"}>{settings.enabled ? "AKTIF" : "NONAKTIF"}</b>
          <small>Sumber {settings.source === "database" ? "dashboard" : "environment fallback"}</small>
        </aside>
      </header>

      <div className="emisell-settings-grid">
        <form className="dashboard-panel emisell-callback-card" action={saveAction}>
          <div className="emisell-settings-card-heading">
            <div><p>TUJUAN WEBHOOK</p><h3>Endpoint Emisell</h3></div>
            <span>MANUAL URL</span>
          </div>
          <label>
            Callback URL
            <input
              type="url"
              name="callback_url"
              value={callbackURL}
              onChange={(event) => setCallbackURL(event.target.value)}
              placeholder="https://api.emisell.com/webhooks/v1/payment-proxy"
              required={enabled}
              autoComplete="off"
              spellCheck={false}
            />
            <small>Production wajib HTTPS publik. Gunakan endpoint backend Emisell, bukan URL dashboard browser.</small>
          </label>
          <label className={`emisell-settings-toggle ${!settings.secret_configured ? "disabled" : ""}`}>
            <input
              type="checkbox"
              name="enabled"
              checked={enabled}
              disabled={!settings.secret_configured}
              onChange={(event) => setEnabled(event.target.checked)}
            />
            <span>
              <strong>Aktifkan pengiriman event</strong>
              <small>{settings.secret_configured ? "Worker akan memproses event outbox yang menunggu." : "Generate webhook secret terlebih dahulu."}</small>
            </span>
          </label>
          <button className="dashboard-primary-button emisell-settings-submit" type="submit" disabled={busy}>
            {savePending ? "Menyimpan…" : "Simpan pengaturan"}
          </button>
          {saveState.message && <div className={`form-message ${saveState.status}`} role="status">{saveState.message}</div>}
        </form>

        <section className="dashboard-panel emisell-secret-card">
          <div className="emisell-settings-card-heading">
            <div><p>HMAC SIGNING</p><h3>Webhook secret</h3></div>
            <span className={settings.secret_configured ? "stored" : "missing"}>{settings.secret_configured ? "TERSIMPAN" : "BELUM DIBUAT"}</span>
          </div>
          {oneTimeSecret ? (
            <div className="emisell-generated-secret">
              <strong>Salin sekarang — hanya ditampilkan sekali</strong>
              <code>{oneTimeSecret}</code>
              <p>Pasang nilai ini sebagai webhook secret pada receiver Emisell Backend sebelum delivery diaktifkan kembali.</p>
              <div>
                <button type="button" className="secondary-button" onClick={() => void copySecret()}>{copied ? "Sudah disalin" : "Salin secret"}</button>
                <button type="button" className="secondary-button" onClick={() => setOneTimeSecret("")}>Saya sudah menyimpan</button>
              </div>
            </div>
          ) : settings.secret_configured ? (
            <div className="emisell-secret-mask">
              <code>{settings.secret_hint || "Secret terenkripsi"}</code>
              <small>Secret asli tidak dapat ditampilkan kembali.</small>
            </div>
          ) : (
            <div className="emisell-secret-empty">
              <strong>Belum ada signing secret</strong>
              <p>Generate secret lalu salin satu kali ke konfigurasi receiver Emisell Backend.</p>
            </div>
          )}
          <form
            action={generateAction}
            onSubmit={(event) => {
              if (settings.secret_configured && !window.confirm("Rotate secret akan menonaktifkan delivery dan membuat secret lama tidak berlaku. Lanjutkan?")) event.preventDefault();
            }}
          >
            <button className="secondary-button emisell-generate-button" type="submit" disabled={busy}>
              {generatePending ? "Membuat…" : settings.secret_configured ? "Rotate secret" : "Generate secret"}
            </button>
          </form>
          {generateState.message && <div className={`form-message ${generateState.status}`} role="status">{generateState.message}</div>}
        </section>
      </div>

      <section className="dashboard-panel emisell-test-card">
        <div>
          <p>VERIFIKASI KONEKSI</p>
          <h3>Test webhook tanpa data pembayaran</h3>
          <span>Mengirim event <code>webhook.test</code> dengan header dan signature production. Event tidak berisi customer, order, payment, atau credential provider.</span>
        </div>
        <form action={testAction}>
          <button className="dashboard-primary-button" type="submit" disabled={busy || !canTest}>{testPending ? "Menguji…" : "Kirim test webhook"}</button>
        </form>
        {(test || settings.last_test_at) && (
          <div className={`emisell-test-result ${lastTestSuccess ? "success" : "failed"}`}>
            <strong>{lastTestSuccess ? "Koneksi berhasil" : "Koneksi belum berhasil"}</strong>
            <small>{test?.message || settings.last_test_error || `HTTP ${settings.last_test_http_status ?? "—"}`} · {formatTime(lastTestAt)}</small>
          </div>
        )}
        {testState.message && !test && <div className={`form-message ${testState.status}`} role="status">{testState.message}</div>}
      </section>
    </section>
  );
}
