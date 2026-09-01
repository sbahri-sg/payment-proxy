import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { BrandLogo } from "../components/brand-logo";
import { getReadiness } from "../lib/readiness";
import { getProviderAvailability, listProviders, type OfficialProviderEvent, type OfficialProviderStatus, type Provider, type ProviderAvailabilityItem, type ProviderAvailabilityOverview } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";
import { StatusAutoRefresh } from "./status-auto-refresh";

export const dynamic = "force-dynamic";

type OperationalStatus = ProviderAvailabilityItem["status"];
type ProviderGroup = {
  provider: Provider;
  official?: OfficialProviderStatus;
  connections: ProviderAvailabilityItem[];
  status: OperationalStatus;
};

const emptyOverview: ProviderAvailabilityOverview = {
  generated_at: new Date(0).toISOString(),
  summary: { connections: 0, available: 0, degraded: 0, unavailable: 0, unknown: 0, affected_methods: 0 },
  items: [],
  official_providers: [],
};

function statusTone(status: OperationalStatus) {
  if (status === "AVAILABLE") return "success";
  if (status === "DEGRADED") return "pending";
  if (status === "UNAVAILABLE") return "failed";
  return "neutral";
}

function statusLabel(status: OperationalStatus) {
  if (status === "AVAILABLE") return "Operational";
  if (status === "DEGRADED") return "Partial disruption";
  if (status === "UNAVAILABLE") return "Unavailable";
  return "Awaiting data";
}

function formatTime(value?: string) {
  if (!value) return "Unknown";
  return new Intl.DateTimeFormat("id-ID", { timeZone: "Asia/Jakarta", timeZoneName: "short", day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}

function paymentMethodName(code: string) {
  return code.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function internalStatus(connections: ProviderAvailabilityItem[]): OperationalStatus {
  const fresh = connections.filter((item) => item.status !== "UNKNOWN");
  if (fresh.length === 0) return "UNKNOWN";
  const unavailable = fresh.filter((item) => item.status === "UNAVAILABLE").length;
  if (unavailable === fresh.length) return "UNAVAILABLE";
  if (unavailable > 0 || fresh.some((item) => item.status === "DEGRADED")) return "DEGRADED";
  return "AVAILABLE";
}

function effectiveStatus(provider: Provider, official: OfficialProviderStatus | undefined, connections: ProviderAvailabilityItem[]): OperationalStatus {
  if (!provider.available) return "UNKNOWN";
  const local = internalStatus(connections);
  if (official?.status === "UNAVAILABLE" || local === "UNAVAILABLE") return "UNAVAILABLE";
  if (official?.status === "DEGRADED" || local === "DEGRADED") return "DEGRADED";
  if (official?.status === "AVAILABLE") return "AVAILABLE";
  return local;
}

function overallStatus(groups: ProviderGroup[]): OperationalStatus {
  const monitored = groups.filter((group) => group.provider.available && group.status !== "UNKNOWN");
  if (monitored.some((group) => group.status === "UNAVAILABLE")) return "UNAVAILABLE";
  if (monitored.some((group) => group.status === "DEGRADED")) return "DEGRADED";
  if (monitored.length > 0 && monitored.every((group) => group.status === "AVAILABLE")) return "AVAILABLE";
  return "UNKNOWN";
}

function overallCopy(status: OperationalStatus) {
  if (status === "AVAILABLE") return { title: "All monitored payment systems operational", detail: "Seluruh provider yang memiliki status resmi atau probe fresh sedang beroperasi normal." };
  if (status === "DEGRADED") return { title: "Some payment services are disrupted", detail: "Checkout otomatis hanya menampilkan provider dan channel yang masih tersedia." };
  if (status === "UNAVAILABLE") return { title: "Payment provider outage detected", detail: "Satu atau lebih provider sedang tidak tersedia dan telah ditahan dari checkout." };
  return { title: "Status monitoring is collecting data", detail: "Sebagian provider belum mempunyai sumber status resmi atau bukti pemeriksaan terbaru." };
}

function eventTime(event: OfficialProviderEvent) {
  if (event.type === "MAINTENANCE" && event.scheduled_for) {
    const end = event.scheduled_until ? ` – ${formatTime(event.scheduled_until)}` : "";
    return `${formatTime(event.scheduled_for)}${end}`;
  }
  if (event.schedule) return event.schedule;
  return formatTime(event.updated_at ?? event.started_at);
}

export default async function ProviderStatusPage() {
  const session = await requireDashboardSession("/provider-status");
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [availabilityResult, providersResult] = await Promise.allSettled([
    getProviderAvailability(session.subject),
    listProviders(session.subject),
  ]);
  const availability = availabilityResult.status === "fulfilled" ? availabilityResult.value : emptyOverview;
  const providers = providersResult.status === "fulfilled" ? providersResult.value : [];
  const dataError = availabilityResult.status === "rejected" || providersResult.status === "rejected";
  const officialMap = new Map(availability.official_providers.map((item) => [item.provider_code, item]));
  const providerMap = new Map(providers.map((provider) => [provider.code, provider]));
  for (const connection of availability.items) {
    if (!providerMap.has(connection.provider_code)) {
      providerMap.set(connection.provider_code, {
        code: connection.provider_code, name: connection.provider_name, description: "Payment provider",
        available: true, has_logo: false, connector_code: connection.provider_code,
        credential_schema: [], environments: [connection.environment], payment_methods: [],
        created_at: "", updated_at: "",
      });
    }
  }
  const groups: ProviderGroup[] = [...providerMap.values()].map((provider) => {
    const connections = availability.items.filter((item) => item.provider_code === provider.code);
    const official = officialMap.get(provider.code);
    return { provider, official, connections, status: effectiveStatus(provider, official, connections) };
  }).sort((left, right) => Number(right.provider.available) - Number(left.provider.available) || left.provider.name.localeCompare(right.provider.name));
  const status = overallStatus(groups);
  const statusCopy = overallCopy(status);
  const activeIncidents = availability.official_providers.flatMap((provider) => provider.active_incidents.map((event) => ({ provider, event })));
  const maintenance = availability.official_providers.flatMap((provider) => provider.scheduled_maintenance.map((event) => ({ provider, event })));
  const recentIncidents = availability.official_providers.flatMap((provider) => provider.recent_incidents.map((event) => ({ provider, event })))
    .sort((left, right) => (right.event.updated_at ?? "").localeCompare(left.event.updated_at ?? "")).slice(0, 8);

  return (
    <div className="dashboard-app">
      <StatusAutoRefresh intervalMs={60_000}/>
      <AppSidebar active="provider-status" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search provider or payment service..."/>
        <main className="dashboard-content management-content status-center-content">
          <section className="dashboard-heading status-center-heading">
            <div><p className="breadcrumb">Operations / Payment status</p><h1>Payment system status</h1><p>Status resmi provider dipadukan dengan availability probe internal Payment Proxy.</p></div>
            <span className="provider-status-updated"><Icon name="activity" size={15}/> Updated {formatTime(availability.generated_at)} · Auto-refresh 60s</span>
          </section>

          {dataError && <div className="dashboard-alert error"><strong>Status center belum dapat dimuat lengkap.</strong><span>Periksa koneksi dashboard ke Payment Proxy.</span></div>}

          <section className={`status-center-banner ${statusTone(status)}`}>
            <span><Icon name={status === "AVAILABLE" ? "check" : "activity"} size={23}/></span>
            <div><h2>{statusCopy.title}</h2><p>{statusCopy.detail}</p></div>
            <b><i/>{statusLabel(status)}</b>
          </section>

          <section className="dashboard-panel status-component-panel">
            <div className="panel-heading"><div><p className="panel-kicker">SYSTEM COMPONENTS</p><h2>Payment providers</h2><p>Buka provider untuk melihat layanan resmi dan bukti probe koneksi merchant.</p></div><span>{availability.official_providers.filter((item) => item.source_available).length} official sources connected</span></div>
            <div className="status-component-list">{groups.map((group) => <details key={group.provider.code}>
              <summary>
                <BrandLogo code={group.provider.code} label={group.provider.name} customSrc={group.provider.has_logo ? `/api/provider-assets/${encodeURIComponent(group.provider.code)}/logo` : undefined} className={`catalog-logo ${group.provider.code}`}/>
                <span><strong>{group.provider.name}</strong><small>{group.official?.description ?? "Official status source not configured"} · {group.connections.length} active connections</small></span>
                <b className={`status-badge ${statusTone(group.status)}`}><i/>{group.provider.available ? statusLabel(group.status) : "Not published"}</b>
                <em>⌄</em>
              </summary>
              <div className="status-component-detail">
                {group.official && <div className="status-source-strip"><span><i className={group.official.source_available ? "online" : "offline"}/><strong>Official provider status</strong><small>{group.official.source_available ? group.official.updated_at ? `Updated ${formatTime(group.official.updated_at)}` : "Live official page" : "Source temporarily unavailable"}</small></span><a href={group.official.status_page_url} target="_blank" rel="noreferrer">Open official page <Icon name="arrow" size={12}/></a></div>}
                <div className="status-service-head"><span>Provider service</span><span>Status</span></div>
                {(group.official?.components.length ? group.official.components : group.provider.payment_methods.map((method) => ({ name: paymentMethodName(method), group: "", status: "UNKNOWN" as const }))).map((component) => <div className="status-service-row" key={`${component.group}:${component.name}`}><span><strong>{component.name}</strong><small>{component.group || "Payment service"}</small></span><b className={`status-badge ${statusTone(component.status)}`}><i/>{statusLabel(component.status)}</b></div>)}
                {!group.official?.components.length && !group.provider.payment_methods.length && <p className="status-no-evidence">Belum ada komponen status resmi yang dapat dipetakan untuk provider ini.</p>}
                <div className="status-evidence-title">INTERNAL CHECKOUT EVIDENCE</div>
                <div className="status-evidence-grid">{group.connections.map((connection) => <span key={`${connection.merchant_id}:${connection.installation_id}`}><i className={statusTone(connection.status)}/><span><strong>{connection.environment} · {connection.merchant_id}</strong><small>{statusLabel(connection.status)} · {formatTime(connection.checked_at)}</small></span></span>)}</div>
                {group.connections.length === 0 && <p className="status-no-evidence">Belum ada active merchant connection untuk availability probe internal.</p>}
                <Link href={`/providers/${encodeURIComponent(group.provider.code)}`}>Manage provider <Icon name="arrow" size={13}/></Link>
              </div>
            </details>)}</div>
          </section>

          <section className="status-event-grid">
            <article className="dashboard-panel status-event-panel">
              <div className="panel-heading"><div><p className="panel-kicker">ACTIVE INCIDENTS</p><h2>Current incidents</h2></div><span>{activeIncidents.length}</span></div>
              <div className="status-event-list">{activeIncidents.map(({ provider, event }) => <a href={event.url || provider.status_page_url} target="_blank" rel="noreferrer" key={`${provider.provider_code}:${event.id}`}><span className="incident"><Icon name="activity" size={15}/></span><span><strong>{provider.provider_name} · {event.name}</strong><small>{event.summary || event.components.join(", ") || "Provider incident in progress."}</small><em>{event.status} · {eventTime(event)}</em></span></a>)}</div>
              {activeIncidents.length === 0 && <div className="status-event-empty"><Icon name="check" size={20}/><span><strong>No active incidents</strong><small>Tidak ada incident aktif pada sumber status resmi provider.</small></span></div>}
            </article>

            <article className="dashboard-panel status-event-panel">
              <div className="panel-heading"><div><p className="panel-kicker">MAINTENANCE</p><h2>Scheduled &amp; active maintenance</h2></div><span>{maintenance.length}</span></div>
              <div className="status-event-list">{maintenance.slice(0, 8).map(({ provider, event }) => <a href={event.url || provider.status_page_url} target="_blank" rel="noreferrer" key={`${provider.provider_code}:${event.id}`}><span className="maintenance"><Icon name="settings" size={15}/></span><span><strong>{provider.provider_name} · {event.name}</strong><small>{event.components.join(", ") || event.summary || "Provider scheduled maintenance."}</small><em>{eventTime(event)}</em></span></a>)}</div>
              {maintenance.length === 0 && <div className="status-event-empty"><Icon name="check" size={20}/><span><strong>No maintenance reported</strong><small>Tidak ada maintenance aktif atau terjadwal yang relevan dari provider.</small></span></div>}
            </article>
          </section>

          <section className="dashboard-panel status-history-panel">
            <div className="panel-heading"><div><p className="panel-kicker">PAST INCIDENTS</p><h2>Recent provider history</h2><p>Riwayat resmi terbaru yang relevan dengan layanan payment Emisell.</p></div><span>{recentIncidents.length} records</span></div>
            <div className="status-history-list">{recentIncidents.map(({ provider, event }) => <a href={event.url || provider.status_page_url} target="_blank" rel="noreferrer" key={`${provider.provider_code}:${event.id}`}><span><BrandLogo code={provider.provider_code} label={provider.provider_name}/><span><strong>{event.name}</strong><small>{provider.provider_name} · {event.components.slice(0, 3).join(", ") || event.impact}</small></span></span><b className="status-badge success"><i/>{event.status}</b><time>{eventTime(event)}</time><Icon name="arrow" size={13}/></a>)}</div>
            {recentIncidents.length === 0 && <div className="status-event-empty history"><Icon name="logs" size={20}/><span><strong>No incident history available</strong><small>Riwayat akan muncul ketika sumber resmi provider mengembalikannya.</small></span></div>}
          </section>

          <div className="dashboard-alert provider-status-rule"><span className="alert-icon"><Icon name="check" size={14}/></span><strong>Two-layer protection.</strong><span>Status resmi menahan outage global atau channel yang dapat dipetakan; probe internal tetap memeriksa credential dan channel setiap merchant. Sumber resmi yang tidak dapat diakses tidak otomatis mematikan checkout.</span></div>
        </main>
      </div>
    </div>
  );
}
