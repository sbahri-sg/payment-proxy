"use client";

import Link from "next/link";
import { useActionState } from "react";
import {
  createProviderAppProviderAction,
  transitionProviderAppAction,
  uploadProviderAppAction,
  type ProviderAppActionState,
} from "../../actions/provider-apps";
import type { ProviderAppProvider, ProviderAppVersion } from "../../lib/payment-proxy";

const idle: ProviderAppActionState = { status: "idle", message: "" };

function formatBytes(value: number) {
  if (value < 1024 * 1024) return `${Math.max(1, Math.round(value / 1024))} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat("id-ID", {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function nextAction(item: ProviderAppVersion) {
  if (item.status === "UPLOADED") return { status: "VALIDATED" as const, label: "Validate bundle", note: false };
  if (item.status === "VALIDATED") return { status: "CERTIFIED" as const, label: "Approve certification", note: true };
  if (item.status === "CERTIFIED") return { status: "PUBLISHED" as const, label: "Publish release", note: true };
  return null;
}

export function ProviderAppRegistry({ initialProviders }: { initialProviders: ProviderAppProvider[] }) {
  const [state, action, pending] = useActionState(createProviderAppProviderAction, idle);
  const providers = state.provider && !initialProviders.some((item) => item.provider_code === state.provider?.provider_code)
    ? [state.provider, ...initialProviders]
    : initialProviders;
  const totalVersions = providers.reduce((total, item) => total + item.version_count, 0);

  return <section className="provider-app-workspace">
    <section className="provider-app-summary provider-app-registry-summary">
      <article><span>PROVIDERS</span><strong>{providers.length}</strong><small>registered identities</small></article>
      <article><span>ACTIVE</span><strong>{providers.filter((item) => item.status === "ACTIVE").length}</strong><small>published connector</small></article>
      <article><span>DRAFT</span><strong>{providers.filter((item) => item.status === "DRAFT").length}</strong><small>awaiting release</small></article>
      <article><span>VERSIONS</span><strong>{totalVersions}</strong><small>immutable artifacts</small></article>
    </section>

    <details className="dashboard-panel provider-app-create" open={providers.length === 0}>
      <summary>
        <span><b>＋</b><span><strong>Add provider</strong><small>Buat identitas provider sebelum menerima connector ZIP.</small></span></span>
        <i>⌄</i>
      </summary>
      <form action={action}>
        <label><strong>Provider name</strong><input name="provider_name" minLength={2} maxLength={120} placeholder="Contoh: Midtrans" required disabled={pending}/></label>
        <label><strong>Provider code</strong><input name="provider_code" pattern="[a-z0-9_-]{2,48}" placeholder="midtrans" required disabled={pending}/><small>Identitas permanen; huruf kecil, angka, _ atau -.</small></label>
        <label className="provider-app-field-wide"><strong>Description</strong><textarea name="description" maxLength={500} rows={3} placeholder="Kegunaan connector dan cakupan layanan provider." disabled={pending}/></label>
        <label><strong>Website URL</strong><input type="url" name="website_url" placeholder="https://provider.example" disabled={pending}/></label>
        <label><strong>Documentation URL</strong><input type="url" name="documentation_url" placeholder="https://docs.provider.example" disabled={pending}/></label>
        <label><strong>Support email</strong><input type="email" name="support_email" placeholder="support@provider.example" disabled={pending}/></label>
        <div className="provider-app-create-actions">
          <button className="dashboard-primary-button" type="submit" disabled={pending}>{pending ? "Creating…" : "Create provider"}</button>
          <small>Setelah dibuat, nama dan kode pada manifest ZIP wajib sama dengan identitas ini.</small>
        </div>
        {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
      </form>
    </details>

    <section className="dashboard-panel provider-app-list-panel">
      <div className="panel-heading"><div><p className="panel-kicker">PROVIDER REGISTRY</p><h2>Connector providers</h2><p>Satu provider dapat memiliki banyak versi, tetapi hanya satu versi yang aktif.</p></div><span>{providers.length} providers</span></div>
      <div className="provider-app-list provider-app-provider-list">
        {providers.map((item) => <Link className="provider-app-card provider-app-provider-card" href={`/provider-apps/${encodeURIComponent(item.provider_code)}`} key={item.provider_code}>
          <header>
            <div className="provider-app-identity"><span>{item.provider_name.slice(0, 1).toUpperCase()}</span><div><h3>{item.provider_name}</h3><code>{item.provider_code}</code></div></div>
            <b className={`provider-app-status ${item.status.toLowerCase()}`}><i/>{item.status}</b>
          </header>
          <p className="provider-app-provider-description">{item.description || "Belum ada deskripsi provider."}</p>
          <div className="provider-app-metadata">
            <span><small>VERSIONS</small><strong>{item.version_count}</strong></span>
            <span><small>ACTIVE</small><strong>{item.active_version || "—"}</strong></span>
            <span><small>LATEST</small><strong>{item.latest_version || "—"}</strong></span>
            <span><small>UPDATED</small><strong>{formatTime(item.updated_at)}</strong></span>
          </div>
          <footer className="provider-app-provider-footer"><span>{item.latest_status ? `Latest status: ${item.latest_status}` : "Ready for first upload"}</span><b>Manage provider →</b></footer>
        </Link>)}
      </div>
      {providers.length === 0 && <div className="management-empty provider-app-empty"><span>◇</span><h3>Belum ada provider</h3><p>Klik Add provider untuk mendaftarkan identitas connector pertama.</p></div>}
    </section>
  </section>;
}

export function ProviderAppVersionManager({ provider, initialApps }: { provider: ProviderAppProvider; initialApps: ProviderAppVersion[] }) {
  const [uploadState, uploadAction, uploadPending] = useActionState(uploadProviderAppAction, idle);
  const [transitionState, transitionAction, transitionPending] = useActionState(transitionProviderAppAction, idle);
  let apps = uploadState.providerApp && !initialApps.some((item) => item.id === uploadState.providerApp?.id)
    ? [uploadState.providerApp, ...initialApps]
    : initialApps;
  if (transitionState.providerApp) apps = apps.map((item) => item.id === transitionState.providerApp?.id ? transitionState.providerApp : item);

  return <section className="provider-app-workspace">
    <section className="provider-app-summary">
      <article><span>VERSIONS</span><strong>{apps.length}</strong><small>immutable artifacts</small></article>
      <article><span>VALIDATED</span><strong>{apps.filter((item) => ["VALIDATED", "CERTIFIED", "PUBLISHED", "DEPRECATED"].includes(item.status)).length}</strong><small>contract checks passed</small></article>
      <article><span>CERTIFIED</span><strong>{apps.filter((item) => ["CERTIFIED", "PUBLISHED", "DEPRECATED"].includes(item.status)).length}</strong><small>review approved</small></article>
      <article><span>ACTIVE VERSION</span><strong>{provider.active_version || "—"}</strong><small>production release</small></article>
    </section>

    <section className="provider-app-onboarding-grid">
      <form className="dashboard-panel provider-app-upload provider-app-upload-locked" action={uploadAction}>
        <input type="hidden" name="provider_code" value={provider.provider_code}/>
        <div><p>CONNECTOR DELIVERY</p><h2>Upload new version</h2><span>Provider identity dikunci oleh registry dan diverifikasi terhadap manifest.</span></div>
        <div className="provider-app-provider-lock"><small>LOCKED PROVIDER</small><strong>{provider.provider_name}</strong><code>{provider.provider_code}</code></div>
        <label><strong>Connector bundle</strong><input type="file" name="bundle" accept=".zip,application/zip" required disabled={uploadPending}/><small>ZIP maksimum 25 MB · root manifest.json + checksums.txt · SDK contract v1</small></label>
        <button className="dashboard-primary-button" type="submit" disabled={uploadPending}>{uploadPending ? "Scanning bundle…" : "Upload and scan"}</button>
        {uploadState.message && <div className={`form-message ${uploadState.status}`} role="status">{uploadState.message}</div>}
      </form>
      <aside className="dashboard-panel provider-app-guidance">
        <p>SUBMISSION FLOW</p><h2>Release checklist</h2>
        <ol><li><b>1</b><span><strong>Upload</strong><small>ZIP dan manifest diterima immutable.</small></span></li><li><b>2</b><span><strong>Validate</strong><small>Checksum, schema, host, dan operasi diperiksa.</small></span></li><li><b>3</b><span><strong>Certify</strong><small>Evidence sandbox dan keamanan disetujui.</small></span></li><li><b>4</b><span><strong>Publish</strong><small>Runtime harus cocok dengan versi dan SHA-256.</small></span></li></ol>
      </aside>
    </section>

    {transitionState.message && <div className={`form-message provider-app-global-message ${transitionState.status}`} role="status">{transitionState.message}</div>}

    <section className="dashboard-panel provider-app-list-panel">
      <div className="panel-heading"><div><p className="panel-kicker">SUBMISSION HISTORY</p><h2>Connector versions</h2><p>Versi lama tetap tersimpan sebagai audit trail dan tidak tampil sebagai provider duplikat.</p></div><span>{apps.length} versions</span></div>
      <div className="provider-app-version-list">
        {apps.map((item, index) => {
          const transition = nextAction(item);
          return <details className="provider-app-version" key={item.id} open={item.status === "PUBLISHED" || index === 0}>
            <summary>
              <div className="provider-app-version-title"><b>v{item.version}</b><span><strong>{item.file_name}</strong><small>{item.runtime.replaceAll("_", " ")} · {formatBytes(item.artifact_size)} · {formatTime(item.created_at)}</small></span></div>
              <span className={`provider-app-status ${item.status.toLowerCase()}`}><i/>{item.status}</span><em>⌄</em>
            </summary>
            <article className="provider-app-version-body">
              <div className="provider-app-contract"><div><small>OPERATIONS · {item.manifest.operations.length}</small><p>{item.manifest.operations.map((operation) => <b key={operation}>{operation.replaceAll("_", " ")}</b>)}</p></div><div><small>PAYMENT METHODS · {item.manifest.payment_methods.length}</small><p>{item.manifest.payment_methods.map((method) => <b key={method}>{method.replaceAll("_", " ")}</b>)}</p></div></div>
              <div className="provider-app-scan"><span>✓</span><div><strong>{item.scan_report.checks.length} static checks passed</strong><small>{item.scan_report.file_count} files · {item.manifest.outbound_hosts.join(", ")} · ZIP {item.artifact_sha256.slice(0, 12)}… · binary {item.scan_report.entrypoint_sha256 ? `${item.scan_report.entrypoint_sha256.slice(0, 12)}…` : "legacy"}</small></div></div>
              {item.scan_report.warnings.map((warning) => <p className="provider-app-warning" key={warning}>{warning}</p>)}
              {item.review_note && <p className="provider-app-review"><strong>Review:</strong> {item.review_note}</p>}
              {transition && <form className="provider-app-transition" action={transitionAction}>
                <input type="hidden" name="id" value={item.id}/><input type="hidden" name="expected_status" value={item.status}/><input type="hidden" name="status" value={transition.status}/>
                {transition.note && <input name="review_note" minLength={8} maxLength={2000} placeholder={transition.status === "CERTIFIED" ? "Evidence/review certification…" : "Deployment evidence and runtime version…"} required/>}
                <button className={transition.status === "PUBLISHED" ? "secondary-button" : "dashboard-primary-button"} type="submit" disabled={transitionPending}>{transitionPending ? "Processing…" : transition.label}</button>
                {transition.status === "PUBLISHED" && <small>Publish hanya berhasil setelah runtime memuat versi dan SHA-256 binary yang sama.</small>}
              </form>}
            </article>
          </details>;
        })}
      </div>
      {apps.length === 0 && <div className="management-empty provider-app-empty"><span>◇</span><h3>Belum ada connector version</h3><p>Upload ZIP pertama untuk memulai validasi provider {provider.provider_name}.</p></div>}
    </section>
  </section>;
}
