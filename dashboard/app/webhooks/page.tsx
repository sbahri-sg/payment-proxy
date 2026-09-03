import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { EmisellWebhookSettingsPanel } from "../components/webhooks/emisell-webhook-settings";
import { ReplayDelivery } from "../components/webhooks/replay-delivery";
import { OperationsRefresh } from "../components/operations-refresh";
import { getReadiness } from "../lib/readiness";
import { getEmisellWebhookSettings, listWebhookDeliveries, listWebhookInbox, type EmisellWebhookSettings, type WebhookDelivery, type WebhookDeliveryStatus, type WebhookInboxItem, type WebhookInboxStatus } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

type WebhookView = "inbox" | "deliveries" | "configuration";

const inboxStatuses: WebhookInboxStatus[] = ["RECEIVED", "PROCESSED", "IGNORED", "FAILED"];
const deliveryStatuses: WebhookDeliveryStatus[] = ["PENDING", "PROCESSING", "DELIVERED", "DEAD"];

function scalar(value: string | string[] | undefined) {
  return typeof value === "string" ? value.trim() : "";
}

function statusTone(status: string) {
  if (status === "PROCESSED" || status === "DELIVERED") return "success";
  if (status === "FAILED" || status === "DEAD") return "failed";
  if (status === "RECEIVED" || status === "PENDING" || status === "PROCESSING") return "pending";
  return "neutral";
}

function formatTime(value?: string) {
  return value ? new Intl.DateTimeFormat("id-ID", { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value)) : "—";
}

function outboundDestination(settings: EmisellWebhookSettings) {
  const raw = settings.callback_url.trim();
  if (!raw) return { configured: false, enabled: false, development: false, label: "Not configured" };
  try {
    const parsed = new URL(raw);
    const development = parsed.hostname === "emisell-receiver" || parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1";
    return { configured: true, enabled: settings.enabled, development, label: `${parsed.protocol}//${parsed.host}${parsed.pathname}` };
  } catch {
    return { configured: false, enabled: false, development: false, label: "Invalid destination URL" };
  }
}

function aggregateLink(type: string, id: string) {
  return type === "payment" ? `/payments/${id}` : undefined;
}

function hrefFor(input: { view: string; q: string; status: string; merchantID: string }, offset = 0) {
  const query = new URLSearchParams({ view: input.view });
  if (input.merchantID) query.set("merchant_id", input.merchantID);
  if (input.q) query.set("q", input.q);
  if (input.status) query.set("status", input.status);
  if (offset > 0) query.set("offset", String(offset));
  return `/webhooks?${query}`;
}

function InboxRecord({ item }: { item: WebhookInboxItem }) {
  const link = aggregateLink(item.aggregate_type, item.aggregate_id);
  return (
    <details className="webhook-record">
      <summary className="webhook-record-row">
        <span><strong>{item.event_type || "Unknown event"}</strong><small>{item.external_event_id}</small><small>Merchant: {item.merchant_id || "Unassigned"}</small></span>
        <span><b>{item.aggregate_type || "unmatched"}</b><small>{item.aggregate_id || "No tenant aggregate"}</small></span>
        <span><code>{item.payload_sha256.slice(0, 14)}…</code><small>SHA-256 verified</small></span>
        <span><b className={`status-badge ${statusTone(item.status)}`}><i/>{item.status}</b><small>Event outcome: {item.canonical_status || "UNKNOWN"}</small></span>
        <span>{formatTime(item.received_at)}</span><span className="webhook-chevron">⌄</span>
      </summary>
      <div className="webhook-record-detail">
        <div className="webhook-detail-grid"><span><small>Inbox ID</small><code>{item.id}</code></span><span><small>Source</small><strong>{item.source}</strong></span><span><small>Processed</small><strong>{formatTime(item.processed_at)}</strong></span><span><small>Payload fingerprint</small><code>{item.payload_sha256}</code></span></div>
        <div className="encrypted-payload-note"><Icon name="check" size={16}/><div><strong>Encrypted raw payload</strong><p>Dashboard hanya menampilkan fingerprint SHA-256. Ciphertext, webhook signature, dan raw provider payload tidak dikirim ke browser.</p></div>{link && <Link href={link}>Open {item.aggregate_type} <Icon name="arrow" size={14}/></Link>}</div>
        {item.error_message && <div className="webhook-error"><strong>Processing error</strong><span>{item.error_message}</span></div>}
        {!item.merchant_id && <div className="webhook-error"><strong>Unassigned event</strong><span>Event ini belum terhubung ke merchant. Tampil hanya untuk admin; status payment tidak diubah otomatis.</span></div>}
      </div>
    </details>
  );
}

function DeliveryRecord({ item }: { item: WebhookDelivery }) {
  const link = aggregateLink(item.aggregate_type, item.aggregate_id);
  const canonical = item.payload.api_version === "2026-08-28" && item.payload.id === item.id && item.payload.type === item.event_type;
  return (
    <details className="webhook-record">
      <summary className="webhook-record-row">
        <span><strong>{item.event_type}</strong><small>{item.id}</small><small>Merchant: {item.merchant_id || "Unassigned"}</small></span>
        <span><b>{item.aggregate_type}</b><small>{item.aggregate_id}</small></span>
        <span><strong>{item.attempt_count} / {item.max_attempts}</strong><small>{item.last_http_status ? `HTTP ${item.last_http_status}` : "No response yet"}</small></span>
        <span><b className={`status-badge ${statusTone(item.status)}`}><i/>{item.status}</b></span>
        <span>{formatTime(item.updated_at)}</span><span className="webhook-chevron">⌄</span>
      </summary>
      <div className="webhook-record-detail">
        <div className="webhook-detail-grid delivery"><span><small>Available at</small><strong>{formatTime(item.available_at)}</strong></span><span><small>Delivered at</small><strong>{formatTime(item.delivered_at)}</strong></span><span><small>Replay count</small><strong>{item.replay_count}</strong></span><span><small>Last replay actor</small><strong>{item.last_replayed_by || "Never replayed"}</strong></span></div>
        {item.last_error && <div className="webhook-error"><strong>Last delivery error</strong><span>{item.last_error}</span></div>}
        {!canonical && <div className="webhook-error"><strong>Legacy payload</strong><span>Event ini dibuat sebelum kontrak Emisell Backend v2026-08-28. Riwayat delivery tidak diubah karena payload yang telah ditandatangani bersifat immutable.</span></div>}
        <section className="canonical-payload"><div><span>{canonical ? "EMISELL BACKEND EVENT · 2026-08-28" : "LEGACY OUTBOX EVENT"}</span>{link && <Link href={link}>Open {item.aggregate_type} <Icon name="arrow" size={13}/></Link>}</div><pre><code>{JSON.stringify(item.payload, null, 2)}</code></pre></section>
        {item.status === "DEAD" && <div className="replay-zone"><div><strong>Manual replay</strong><p>Replay mereset attempt window dan menjadwalkan event yang sama. Hanya delivery DEAD yang dapat direplay.</p></div><ReplayDelivery id={item.id} replayCount={item.replay_count}/></div>}
      </div>
    </details>
  );
}

export default async function WebhooksPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const session = await requireDashboardSession("/webhooks");
  const query = await searchParams;
  const requestedView = scalar(query.view);
  const view: WebhookView = requestedView === "deliveries" ? "deliveries" : requestedView === "configuration" ? "configuration" : "inbox";
  const q = scalar(query.q).slice(0, 128);
  const merchantInput = scalar(query.merchant_id).slice(0, 128);
  const merchantID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(merchantInput) ? merchantInput : "";
  const requestedStatus = scalar(query.status).toUpperCase();
  const allowedStatuses = view === "inbox" ? inboxStatuses : view === "deliveries" ? deliveryStatuses : [];
  const status = allowedStatuses.includes(requestedStatus as never) ? requestedStatus : "";
  const offsetNumber = Number(scalar(query.offset));
  const offset = Number.isSafeInteger(offsetNumber) && offsetNumber >= 0 ? Math.min(offsetNumber, 10000) : 0;
  const limit = 25;
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [settingsResult, inboxResult, deliveryResult] = await Promise.allSettled([
    getEmisellWebhookSettings(session.subject),
    listWebhookInbox(session.subject, { merchantID, q: view === "inbox" ? q : "", status: view === "inbox" ? status : "", limit, offset: view === "inbox" ? offset : 0 }),
    listWebhookDeliveries(session.subject, { merchantID, q: view === "deliveries" ? q : "", status: view === "deliveries" ? status : "", limit, offset: view === "deliveries" ? offset : 0 }),
  ]);
  const settings: EmisellWebhookSettings = settingsResult.status === "fulfilled" ? settingsResult.value : {
    configured: false, callback_url: "", enabled: false, secret_configured: false,
    secret_hint: "", source: "database",
  };
  const destination = outboundDestination(settings);
  const inbox = inboxResult.status === "fulfilled" ? inboxResult.value : { items: [], counts: {}, total: 0, limit, offset: 0, has_more: false };
  const deliveries = deliveryResult.status === "fulfilled" ? deliveryResult.value : { items: [], counts: {}, total: 0, limit, offset: 0, has_more: false };
  const active = view === "deliveries" ? deliveries : inbox;
  const first = active.total ? offset + 1 : 0;
  const last = Math.min(offset + active.items.length, active.total);
  const filters = { view, q, status, merchantID };
  const tabHref = (nextView: WebhookView) => hrefFor({ view: nextView, q: "", status: "", merchantID });
  const inboxTotal = Object.values(inbox.counts).reduce((total, value) => total + (value ?? 0), 0);
  const deliveryTotal = Object.values(deliveries.counts).reduce((total, value) => total + (value ?? 0), 0);
  const dataFailed = settingsResult.status === "rejected" || (view === "inbox" && inboxResult.status === "rejected") || (view === "deliveries" && deliveryResult.status === "rejected");

  return (
    <div className="dashboard-app">
      <AppSidebar active="webhooks" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search event, aggregate, or delivery ID..."/>
        <main className="dashboard-content management-content webhooks-content">
          <section className="dashboard-heading"><div><p className="breadcrumb">Operations / Webhooks</p><h1>Webhook operations</h1><p>Monitor provider events and delivery to Emisell Backend · {merchantID || "All merchants"}.</p></div><div className="operations-heading-actions">{view !== "configuration" && <OperationsRefresh refreshedAt={new Date().toISOString()}/>}<Link className="secondary-button" href="/docs#webhooks">API documentation <Icon name="arrow" size={15}/></Link></div></section>

          <nav className="webhook-tabs webhook-primary-tabs" aria-label="Webhook view">
            <Link className={view === "inbox" ? "active" : ""} href={tabHref("inbox")}><Icon name="webhook" size={15}/><span>Incoming</span><b>{inboxTotal}</b></Link>
            <Link className={view === "deliveries" ? "active" : ""} href={tabHref("deliveries")}><Icon name="logs" size={15}/><span>Emisell deliveries</span><b>{deliveryTotal}</b></Link>
            <Link className={view === "configuration" ? "active" : ""} href={tabHref("configuration")}><Icon name="settings" size={15}/><span>Configuration</span><b className={settings.enabled ? "enabled" : "disabled"}>{settings.enabled ? "ON" : "OFF"}</b></Link>
          </nav>

          {dataFailed && <div className="dashboard-alert error"><strong>Webhook data belum lengkap.</strong><span>Periksa migrasi database, ADMIN_API_KEY, dan koneksi Payment Proxy API.</span></div>}

          {view === "configuration" ? (
            <>
              <EmisellWebhookSettingsPanel initialSettings={settings} />
              <div className="webhook-configuration-note"><Icon name="docs" size={18}/><div><strong>Integration contract</strong><p>Header HMAC, contoh payload, aturan idempotency, dan response receiver tersedia di API Documentation.</p></div><Link href="/docs#webhooks">Open documentation <Icon name="arrow" size={14}/></Link></div>
            </>
          ) : (
            <>
              {(!destination.enabled || destination.development) && <div className="dashboard-alert warning webhook-connection-alert"><span className="alert-icon"><Icon name={destination.configured ? "webhook" : "logs"} size={16}/></span><strong>{!destination.enabled ? "Outbound delivery disabled" : "Development receiver"}</strong><span>{destination.label}{destination.development ? " · not a production Emisell endpoint" : ""}</span><Link href="/webhooks?view=configuration">Configure <Icon name="arrow" size={14}/></Link></div>}
              <section className="webhook-health-strip" aria-label="Webhook operational summary">
                <article><span>Inbox events</span><strong>{inboxTotal}</strong></article>
                <article><span>Processed</span><strong>{inbox.counts.PROCESSED ?? 0}</strong></article>
                <article><span>Awaiting delivery</span><strong>{(deliveries.counts.PENDING ?? 0) + (deliveries.counts.PROCESSING ?? 0)}</strong></article>
                <article className={(deliveries.counts.DEAD ?? 0) > 0 ? "danger" : ""}><span>Dead letter</span><strong>{deliveries.counts.DEAD ?? 0}</strong></article>
              </section>
              <form className="webhook-filter-bar" method="get"><input type="hidden" name="view" value={view}/><label><Icon name="search" size={15}/><input name="q" defaultValue={q} placeholder="Event ID, aggregate, or event type"/></label><label><Icon name="provider" size={15}/><input name="merchant_id" defaultValue={merchantID} placeholder="Merchant ID (all merchants)" aria-label="Filter merchant ID"/></label><select name="status" defaultValue={status} aria-label="Filter webhook status"><option value="">All statuses</option>{allowedStatuses.map((item) => <option value={item} key={item}>{item}</option>)}</select><button className="dashboard-primary-button" type="submit">Apply filters</button>{(q || status || merchantID) && <Link className="secondary-button" href={`/webhooks?view=${view}`}>Reset</Link>}</form>
              <section className="dashboard-panel webhook-list-panel">
                <div className="panel-heading"><div><p className="panel-kicker">{view === "inbox" ? "PROVIDER INBOX" : "EMISELL OUTBOX"}</p><h2>{view === "inbox" ? "Incoming events" : "Delivery attempts"}</h2><p>{view === "inbox" ? "Raw payload remains encrypted; only safe metadata is displayed." : "Canonical events are signed and delivered at least once."}</p></div><span>{first}–{last} of {active.total}</span></div>
                <div className="webhook-record-head"><span>Event</span><span>Aggregate</span><span>{view === "inbox" ? "Integrity" : "Attempts"}</span><span>Status</span><span>{view === "inbox" ? "Received" : "Updated"}</span><span/></div>
                <div className="webhook-records">{view === "inbox" ? inbox.items.map((item) => <InboxRecord item={item} key={item.id}/>) : deliveries.items.map((item) => <DeliveryRecord item={item} key={item.id}/>)}</div>
                {active.items.length === 0 && <div className="management-empty webhook-empty"><span><Icon name="webhook" size={25}/></span><h3>No webhook records found</h3><p>Ubah filter atau jalankan alur payment yang menghasilkan webhook provider.</p></div>}
                <footer className="payment-pagination"><span>Showing {first}–{last}</span><nav>{offset > 0 ? <Link className="secondary-button" href={hrefFor(filters, Math.max(0, offset - limit))}>Previous</Link> : <span className="secondary-button disabled">Previous</span>}{active.has_more ? <Link className="secondary-button" href={hrefFor(filters, offset + limit)}>Next</Link> : <span className="secondary-button disabled">Next</span>}</nav></footer>
              </section>
              {view === "deliveries" && <div className="webhook-safety-note"><Icon name="check" size={18}/><div><strong>Replay safety</strong><p>Event delivery bersifat at-least-once. Replay hanya membuka attempt window baru untuk status DEAD dengan audit log dan idempotency key.</p></div></div>}
            </>
          )}
        </main>
      </div>
    </div>
  );
}
