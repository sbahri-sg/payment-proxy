"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useActionState, useEffect } from "react";
import {
  createProviderAppProviderAction,
  transitionProviderAppAction,
  uploadProviderAppAction,
  type ProviderAppActionState,
} from "../../actions/provider-apps";
import type { Provider, ProviderAppProvider, ProviderAppVersion, ProviderReleaseVerificationReport, RuntimeConnector } from "../../lib/payment-proxy";

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
    timeZone: "Asia/Jakarta",
  }).format(new Date(value));
}

function nextAction(item: ProviderAppVersion) {
  if (item.status === "UPLOADED") return { status: "VALIDATED" as const, label: "Validate bundle" };
  if (item.status === "VALIDATED") return { status: "CERTIFIED" as const, label: "Run backend verification" };
  if (item.status === "CERTIFIED") return { status: "PUBLISHED" as const, label: "Publish release" };
  return null;
}

function releaseStatusLabel(status: ProviderAppVersion["status"]) {
  return status === "CERTIFIED" ? "VERIFIED" : status;
}

export function ProviderAppRegistry({ initialProviders, catalogProviders, runtimeConnectors }: { initialProviders: ProviderAppProvider[]; catalogProviders: Provider[]; runtimeConnectors: RuntimeConnector[] }) {
  const [state, action, pending] = useActionState(createProviderAppProviderAction, idle);
  const providers = state.provider && !initialProviders.some((item) => item.provider_code === state.provider?.provider_code)
    ? [state.provider, ...initialProviders]
    : initialProviders;
  const totalVersions = providers.reduce((total, item) => total + item.version_count, 0);

  return <section className="provider-app-workspace">
    <section className="provider-app-summary provider-app-registry-summary">
      <article><span>RELEASE WORKSPACES</span><strong>{providers.length}</strong><small>registered connector identities</small></article>
      <article><span>PUBLISHED</span><strong>{providers.filter((item) => item.status === "ACTIVE").length}</strong><small>active connector releases</small></article>
      <article><span>AWAITING RELEASE</span><strong>{providers.filter((item) => item.status === "DRAFT").length}</strong><small>draft release workspaces</small></article>
      <article><span>SUBMISSIONS</span><strong>{totalVersions}</strong><small>immutable review packages</small></article>
    </section>

    <details className="dashboard-panel provider-app-create" open={providers.length === 0}>
      <summary>
        <span><b>＋</b><span><strong>Register connector identity</strong><small>Buat workspace release; ini tidak meng-install gateway untuk merchant.</small></span></span>
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
          <button className="dashboard-primary-button" type="submit" disabled={pending}>{pending ? "Creating…" : "Register identity"}</button>
          <small>Provider yang sudah ada tetap memakai code yang sama. Tidak ada merchant connection yang dibuat atau diduplikasi.</small>
        </div>
        {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
      </form>
    </details>

    <section className="dashboard-panel provider-app-list-panel">
      <div className="panel-heading"><div><p className="panel-kicker">RELEASE REGISTRY</p><h2>Connector release workspaces</h2><p>Satu provider dapat memiliki banyak submission, tetapi hanya satu release yang dipublish.</p></div><span>{providers.length} providers</span></div>
      <div className="provider-app-list provider-app-provider-list">
        {providers.map((item) => {
          const catalog = catalogProviders.find((provider) => provider.code === item.provider_code);
          const runtime = runtimeConnectors.find((connector) => connector.code === item.provider_code);
          return <Link className="provider-app-card provider-app-provider-card" href={`/providers/${encodeURIComponent(item.provider_code)}?tab=releases`} key={item.provider_code}>
          <header>
            <div className="provider-app-identity"><span>{item.provider_name.slice(0, 1).toUpperCase()}</span><div><h3>{item.provider_name}</h3><code>{item.provider_code}</code></div></div>
            <b className={`provider-app-status ${item.status.toLowerCase()}`}><i/>RELEASE {item.status}</b>
          </header>
          <p className="provider-app-provider-description">{item.description || "Belum ada deskripsi provider."}</p>
          <div className="provider-app-operational-state">
            <span><small>RUNTIME</small><strong className={runtime ? "is-ready" : "is-pending"}>{runtime ? `Running · ${runtime.version}` : "Not loaded"}</strong></span>
            <span><small>CATALOG</small><strong className={catalog?.available ? "is-ready" : "is-pending"}>{catalog?.available ? "Available" : "Unavailable"}</strong></span>
          </div>
          <div className="provider-app-metadata">
            <span><small>SUBMISSIONS</small><strong>{item.version_count}</strong></span>
            <span><small>PUBLISHED RELEASE</small><strong>{item.active_version || "—"}</strong></span>
            <span><small>LATEST VERSION</small><strong>{item.latest_version || "—"}</strong></span>
            <span><small>UPDATED</small><strong>{formatTime(item.updated_at)}</strong></span>
          </div>
          <footer className="provider-app-provider-footer"><span>{item.latest_status ? `Latest submission: ${item.latest_status}` : runtime ? "Runtime aktif · submission pertama belum di-upload" : "Ready for first submission"}</span><b>Manage releases →</b></footer>
        </Link>})}
      </div>
      {providers.length === 0 && <div className="management-empty provider-app-empty"><span>◇</span><h3>Belum ada provider</h3><p>Klik Add provider untuk mendaftarkan identitas connector pertama.</p></div>}
    </section>
  </section>;
}

export function ProviderReleaseWorkspaceSetup({ provider }: { provider: Provider }) {
  const [state, action, pending] = useActionState(createProviderAppProviderAction, idle);
  const router = useRouter();
  useEffect(() => { if (state.status === "success" && state.provider) router.refresh(); }, [router, state]);
  return <section className="dashboard-panel provider-app-create">
    <div className="panel-heading"><div><p className="panel-kicker">RELEASE WORKSPACE</p><h2>Initialize connector releases</h2><p>Identitas release dikunci ke provider yang sama; ini tidak membuat provider baru atau merchant connection.</p></div><span>ONE-TIME SETUP</span></div>
    <form action={action}>
      <input type="hidden" name="provider_code" value={provider.code}/>
      <input type="hidden" name="provider_name" value={provider.name}/>
      <input type="hidden" name="description" value={provider.description}/>
      <input type="hidden" name="website_url" value=""/>
      <input type="hidden" name="documentation_url" value=""/>
      <input type="hidden" name="support_email" value=""/>
      <div className="provider-app-provider-lock"><small>LOCKED PROVIDER</small><strong>{provider.name}</strong><code>{provider.code}</code></div>
      <div className="provider-app-create-actions"><button className="dashboard-primary-button" type="submit" disabled={pending}>{pending ? "Initializing…" : "Initialize releases"}</button><small>Setelah dibuat, submission dan runtime version dikelola dari tab ini.</small></div>
      {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
    </form>
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
      <article><span>BACKEND VERIFIED</span><strong>{apps.filter((item) => ["CERTIFIED", "PUBLISHED", "DEPRECATED"].includes(item.status)).length}</strong><small>runtime contract passed</small></article>
      <article><span>PUBLISHED RELEASE</span><strong>{provider.active_version || "—"}</strong><small>release registry only</small></article>
    </section>

    <section className="provider-app-onboarding-grid">
      <form className="dashboard-panel provider-app-upload provider-app-upload-locked" action={uploadAction}>
        <input type="hidden" name="provider_code" value={provider.provider_code}/>
        <div><p>RELEASE SUBMISSION</p><h2>Upload new version</h2><span>Submission ini milik provider yang sama dan diverifikasi terhadap identitas serta kontrak runtime-nya.</span></div>
        <div className="provider-app-provider-lock"><small>LOCKED PROVIDER</small><strong>{provider.provider_name}</strong><code>{provider.provider_code}</code></div>
        <label><strong>Provider submission</strong><input type="file" name="bundle" accept=".zip,application/zip" required disabled={uploadPending}/><small>ZIP maksimum 25 MB · root emisell-extension.yaml + openapi.yaml · tanpa runtime binary</small></label>
        <button className="dashboard-primary-button" type="submit" disabled={uploadPending}>{uploadPending ? "Scanning bundle…" : "Upload and scan"}</button>
        {uploadState.message && <div className={`form-message ${uploadState.status}`} role="status">{uploadState.message}</div>}
      </form>
      <aside className="dashboard-panel provider-app-guidance">
        <p>SUBMISSION FLOW</p><h2>Release checklist</h2>
        <ol><li><b>1</b><span><strong>Upload</strong><small>Submission dan manifest diterima immutable.</small></span></li><li><b>2</b><span><strong>Validate</strong><small>Schema, OpenAPI, secret, dan keamanan ZIP diperiksa.</small></span></li><li><b>3</b><span><strong>Verify</strong><small>Backend mencocokkan bundle, runtime, credential schema, dan seluruh method mapping.</small></span></li><li><b>4</b><span><strong>Publish</strong><small>Hanya release yang sudah verified dapat diaktifkan.</small></span></li></ol>
      </aside>
    </section>

    {transitionState.message && <div className={`form-message provider-app-global-message ${transitionState.status}`} role="status">{transitionState.message}</div>}

    <section className="dashboard-panel provider-app-list-panel">
      <div className="panel-heading"><div><p className="panel-kicker">SUBMISSION HISTORY</p><h2>Connector versions</h2><p>Versi lama tetap tersimpan sebagai audit trail dan tidak tampil sebagai provider duplikat.</p></div><span>{apps.length} versions</span></div>
      <div className="provider-app-version-list">
        {apps.map((item, index) => {
          const transition = nextAction(item);
          const verification = (item.verification_report ?? {}) as Partial<ProviderReleaseVerificationReport>;
          return <details className="provider-app-version" key={item.id} open={item.status === "PUBLISHED" || index === 0}>
            <summary>
              <div className="provider-app-version-title"><b>v{item.version}</b><span><strong>{item.file_name}</strong><small>{item.runtime.replaceAll("_", " ")} · {formatBytes(item.artifact_size)} · {formatTime(item.created_at)}</small></span></div>
              <span className={`provider-app-status ${item.status.toLowerCase()}`}><i/>{releaseStatusLabel(item.status)}</span><em>⌄</em>
            </summary>
            <article className="provider-app-version-body">
              <div className="provider-app-contract"><div><small>OPERATIONS · {item.manifest.operations.length}</small><p>{item.manifest.operations.map((operation) => <b key={operation}>{operation.replaceAll("_", " ")}</b>)}</p></div><div><small>PAYMENT METHODS · {item.manifest.payment_methods.length}</small><p>{item.manifest.payment_methods.map((method) => <b key={method}>{method.replaceAll("_", " ")}</b>)}</p></div></div>
              <div className="provider-app-scan"><span>✓</span><div><strong>{item.scan_report.checks.length} static checks passed</strong><small>{item.scan_report.file_count} files · {item.manifest.outbound_hosts.join(", ")} · ZIP {item.artifact_sha256.slice(0, 12)}… · {item.scan_report.package_format === "provider_submission_v1" ? "review package" : `legacy binary ${item.scan_report.entrypoint_sha256 ? `${item.scan_report.entrypoint_sha256.slice(0, 12)}…` : "unknown"}`}</small></div></div>
              {verification.passed && <div className="provider-app-scan"><span>✓</span><div><strong>Backend runtime verification passed</strong><small>{verification.verified_capabilities?.length ?? 0} capabilities · runtime {verification.runtime_version || item.version} · immutable digest {verification.runtime_digest?.slice(0, 12) || "legacy"}…</small></div></div>}
              {item.scan_report.warnings.map((warning) => <p className="provider-app-warning" key={warning}>{warning}</p>)}
              {item.review_note && <p className="provider-app-review"><strong>Audit:</strong> {item.review_note}</p>}
              {transition && <form className="provider-app-transition provider-app-transition-direct" action={transitionAction}>
                <input type="hidden" name="id" value={item.id}/><input type="hidden" name="expected_status" value={item.status}/><input type="hidden" name="status" value={transition.status}/>
                <button className={transition.status === "PUBLISHED" ? "secondary-button" : "dashboard-primary-button"} type="submit" disabled={transitionPending}>{transitionPending ? "Processing…" : transition.label}</button>
                {transition.status === "CERTIFIED" && <small>Tidak memakai credential merchant. Semua check dijalankan oleh backend terhadap shared runtime versi ini.</small>}
                {transition.status === "PUBLISHED" && <small>Publish hanya berhasil setelah backend verification PASS dan shared runtime tetap memuat digest immutable yang sama.</small>}
              </form>}
            </article>
          </details>;
        })}
      </div>
      {apps.length === 0 && <div className="management-empty provider-app-empty"><span>◇</span><h3>Belum ada connector version</h3><p>Upload ZIP pertama untuk memulai validasi provider {provider.provider_name}.</p></div>}
    </section>
  </section>;
}
