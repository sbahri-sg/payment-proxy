import Link from "next/link";
import { BrandLogo } from "./components/brand-logo";
import { AppSidebar, AppTopbar, Icon, type IconName } from "./components/app-shell";
import { requireDashboardSession } from "./lib/session";

export const dynamic = "force-dynamic";

type Readiness = {
  status: string;
  checks?: Record<string, string>;
};

type Observability = {
  uptime_seconds: number;
  requests_total: number;
  latency: { average_ms: number; p95_ms: number };
  connector_outcomes: { unknown_outcome: number; not_supported: number; rejected: number };
  provider_webhooks: { accepted: number; duplicate: number; invalid: number };
  slo: {
    status: "NO_DATA" | "MEETING" | "BREACHED";
    availability_target_percent: number;
    availability_percent: number;
    latency_p95_target_ms: number;
    latency_p95_ms: number;
  };
};

type StatusMetric = { status: string; count: number };
type VolumeMetric = { date: string; amount: number; count: number };
type ProviderMetric = {
  code: string;
  name: string;
  available: boolean;
  payment_methods: string[];
  payments_24h: number;
  succeeded_24h: number;
  failed_24h: number;
  volume_24h: number;
};
type RecentPayment = {
  id: string;
  merchant_reference: string;
  provider_code: string;
  environment: string;
  amount: number;
  currency: string;
  status: string;
  created_at: string;
};
type Overview = {
  generated_at: string;
  summary: {
    payments_24h: number;
    previous_payments_24h: number;
    succeeded_volume_24h: number;
    previous_volume_24h: number;
    success_rate_24h: number;
    webhook_success_rate_24h: number;
    active_installations: number;
  };
  payment_statuses: StatusMetric[];
  volume_daily: VolumeMetric[];
  providers: ProviderMetric[];
  recent_payments: RecentPayment[];
  operational_backlog: {
    unknown_payments: number;
    pending_outbox: number;
    dead_outbox: number;
    failed_webhooks: number;
  };
};

async function getReadiness(): Promise<Readiness> {
  const base = process.env.BACKEND_API_URL ?? "http://127.0.0.1:8080";
  try {
    const response = await fetch(`${base}/health/ready`, {
      cache: "no-store",
      signal: AbortSignal.timeout(2500),
    });
    return (await response.json()) as Readiness;
  } catch {
    return { status: "unreachable", checks: { api: "unavailable" } };
  }
}

async function getOverview(): Promise<Overview | null> {
  const base = process.env.BACKEND_API_URL ?? "http://127.0.0.1:8080";
  const adminKey = process.env.ADMIN_API_KEY ?? "";
  if (!adminKey) return null;
  try {
    const response = await fetch(`${base}/api/v1/admin/dashboard/overview`, {
      cache: "no-store",
      headers: { "X-Admin-API-Key": adminKey },
      signal: AbortSignal.timeout(3500),
    });
    if (!response.ok) return null;
    return ((await response.json()) as { data: Overview }).data;
  } catch {
    return null;
  }
}

async function getObservability(): Promise<Observability | null> {
  const base = process.env.BACKEND_API_URL ?? "http://127.0.0.1:8080";
  const adminKey = process.env.ADMIN_API_KEY ?? "";
  if (!adminKey) return null;
  try {
    const response = await fetch(`${base}/api/v1/admin/observability`, {
      cache: "no-store",
      headers: { "X-Admin-API-Key": adminKey },
      signal: AbortSignal.timeout(2500),
    });
    if (!response.ok) return null;
    return ((await response.json()) as { data: Observability }).data;
  } catch {
    return null;
  }
}

function formatIDR(minorAmount: number, compact = false) {
  const amount = minorAmount / 100;
  return new Intl.NumberFormat("id-ID", {
    style: "currency",
    currency: "IDR",
    maximumFractionDigits: 0,
    notation: compact ? "compact" : "standard",
  }).format(amount);
}

function delta(current: number, previous: number) {
  if (previous === 0) return current > 0 ? { label: "Aktivitas baru", positive: true } : { label: "Belum ada data", positive: true };
  const value = ((current - previous) / previous) * 100;
  return { label: `${value >= 0 ? "+" : ""}${value.toFixed(1)}%`, positive: value >= 0 };
}

function statusClass(status: string) {
  const value = status.toLowerCase();
  if (["succeeded", "delivered", "ready", "active"].includes(value)) return "success";
  if (["failed", "dead", "error"].includes(value)) return "failed";
  if (["unknown", "pending", "processing"].includes(value)) return "pending";
  return "neutral";
}

function VolumeChart({ data }: { data: VolumeMetric[] }) {
  const values = data.map((item) => item.amount);
  const max = Math.max(...values, 1);
  const chartWidth = 720;
  const chartHeight = 210;
  const top = 18;
  const bottom = 188;
  const step = data.length > 1 ? chartWidth / (data.length - 1) : chartWidth;
  const points = data.map((item, index) => {
    const x = index * step;
    const y = bottom - (item.amount / max) * (bottom - top);
    return { x, y };
  });
  const line = points.map((point) => `${point.x},${point.y}`).join(" ");
  const area = points.length ? `M 0 ${bottom} L ${points.map((point) => `${point.x} ${point.y}`).join(" L ")} L ${chartWidth} ${bottom} Z` : "";
  return (
    <div className="volume-chart">
      <div className="chart-y-label top">{formatIDR(max, true)}</div>
      <div className="chart-y-label middle">{formatIDR(max / 2, true)}</div>
      <div className="chart-y-label bottom">Rp0</div>
      <svg viewBox={`0 0 ${chartWidth} ${chartHeight}`} preserveAspectRatio="none" role="img" aria-label="Volume pembayaran berhasil tujuh hari terakhir">
        <defs>
          <linearGradient id="volumeFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#6d5dfc" stopOpacity="0.32"/>
            <stop offset="1" stopColor="#6d5dfc" stopOpacity="0"/>
          </linearGradient>
        </defs>
        <path className="chart-grid-line" d={`M0 ${top}H${chartWidth} M0 ${(top + bottom) / 2}H${chartWidth} M0 ${bottom}H${chartWidth}`}/>
        <path d={area} fill="url(#volumeFill)"/>
        {line && <polyline points={line} className="chart-line"/>}
        {points.map((point, index) => <circle key={data[index]?.date} cx={point.x} cy={point.y} r="4.5" className="chart-dot"/>)}
      </svg>
      <div className="chart-dates">{data.map((item) => <span key={item.date}>{new Intl.DateTimeFormat("id-ID", { weekday: "short" }).format(new Date(`${item.date}T00:00:00`))}</span>)}</div>
    </div>
  );
}

export default async function Home() {
  await requireDashboardSession();
  const [health, overview, observability] = await Promise.all([getReadiness(), getOverview(), getObservability()]);
  const healthy = health.status === "ready";
  const summary = overview?.summary ?? {
    payments_24h: 0, previous_payments_24h: 0, succeeded_volume_24h: 0,
    previous_volume_24h: 0, success_rate_24h: 0, webhook_success_rate_24h: 0,
    active_installations: 0,
  };
  const volumeDelta = delta(summary.succeeded_volume_24h, summary.previous_volume_24h);
  const paymentDelta = delta(summary.payments_24h, summary.previous_payments_24h);
  const statusTotal = (overview?.payment_statuses ?? []).reduce((total, item) => total + item.count, 0);
  const succeeded = overview?.payment_statuses.find((item) => item.status === "SUCCEEDED")?.count ?? 0;
  const failed = overview?.payment_statuses.find((item) => item.status === "FAILED")?.count ?? 0;
  const successShare = statusTotal ? (succeeded / statusTotal) * 100 : 0;
  const failedShare = statusTotal ? (failed / statusTotal) * 100 : 0;
  const donutStyle = { background: `conic-gradient(#30b27b 0 ${successShare}%, #f06464 ${successShare}% ${successShare + failedShare}%, #f5b84b ${successShare + failedShare}% 100%)` };

  return (
    <div className="dashboard-app">
      <AppSidebar active="overview" healthy={healthy} engineStatus={health.status}/>

      <div className="dashboard-main">
        <AppTopbar healthy={healthy}/>

        <main className="dashboard-content">
          <section className="dashboard-heading">
            <div><p className="breadcrumb">Workspace / Overview</p><h1>Payment overview</h1><p>Monitor payment performance, providers, and operational health from one place.</p></div>
            <div className="heading-actions"><button className="period-button" type="button"><span>Last 24 hours</span><b>⌄</b></button><Link className="dashboard-primary-action" href="/docs">Open API docs <Icon name="arrow" size={16}/></Link></div>
          </section>

          {!overview && <div className="dashboard-alert error"><strong>Analytics belum dapat dimuat.</strong><span>Health service tetap dipantau; periksa ADMIN_API_KEY pada dashboard server.</span></div>}
          {overview && <div className="dashboard-alert"><span className="alert-icon"><Icon name="check" size={16}/></span><strong>Emisell engine observability active</strong><span>{observability ? `Process SLO ${observability.slo.status.toLowerCase().replace("_", " ")} · p95 ${observability.slo.latency_p95_ms} ms · ${observability.requests_total.toLocaleString("id-ID")} requests observed.` : "Payment analytics is active; process-level SLO snapshot is starting."}</span><Link href="/docs#observability">View SLO contract <Icon name="arrow" size={14}/></Link></div>}

          <section className="metric-grid" aria-label="Key payment metrics">
            <article className="metric-card"><div className="metric-icon purple"><Icon name="wallet"/></div><div className="metric-title"><span>Successful volume</span><small>24h</small></div><strong>{formatIDR(summary.succeeded_volume_24h, true)}</strong><p className={volumeDelta.positive ? "up" : "down"}><b>{volumeDelta.label}</b><span>vs previous 24h</span></p></article>
            <article className="metric-card"><div className="metric-icon blue"><Icon name="payment"/></div><div className="metric-title"><span>Payment attempts</span><small>24h</small></div><strong>{summary.payments_24h.toLocaleString("id-ID")}</strong><p className={paymentDelta.positive ? "up" : "down"}><b>{paymentDelta.label}</b><span>vs previous 24h</span></p></article>
            <article className="metric-card"><div className="metric-icon green"><Icon name="activity"/></div><div className="metric-title"><span>Success rate</span><small>Terminal</small></div><strong>{summary.success_rate_24h.toFixed(1)}%</strong><p className="up"><b>{summary.webhook_success_rate_24h.toFixed(1)}%</b><span>webhook processed</span></p></article>
            <article className="metric-card"><div className="metric-icon amber"><Icon name="provider"/></div><div className="metric-title"><span>Active installations</span><small>Now</small></div><strong>{summary.active_installations}</strong><p className="neutral"><b>{overview?.providers.filter((item) => item.available).length ?? 0} available</b><span>provider connector</span></p></article>
          </section>

          <section className="analytics-grid">
            <article className="dashboard-panel volume-panel">
              <div className="panel-heading"><div><p className="panel-kicker">PAYMENT ANALYTICS</p><h2>Successful volume</h2></div><div className="panel-total"><span>7-day total</span><strong>{formatIDR((overview?.volume_daily ?? []).reduce((total, item) => total + item.amount, 0), true)}</strong></div></div>
              <VolumeChart data={overview?.volume_daily ?? []}/>
            </article>
            <article className="dashboard-panel status-panel">
              <div className="panel-heading"><div><p className="panel-kicker">PAYMENT STATUS</p><h2>Distribution</h2></div><button type="button">•••</button></div>
              <div className="donut-wrap"><div className="donut" style={donutStyle}><div><strong>{statusTotal}</strong><span>attempts</span></div></div></div>
              <div className="status-legend">{(overview?.payment_statuses ?? []).slice(0, 4).map((item) => <div key={item.status}><i className={statusClass(item.status)}/><span>{item.status.toLowerCase().replaceAll("_", " ")}</span><strong>{item.count}</strong></div>)}{statusTotal === 0 && <p className="empty-copy">No payment activity in this period.</p>}</div>
            </article>
          </section>

          <section className="dashboard-panel provider-panel">
            <div className="panel-heading"><div><p className="panel-kicker">CONNECTIONS</p><h2>Provider health</h2><p>Verified capability scope and payment activity by connector.</p></div><Link className="secondary-button" href="/providers">Manage providers <Icon name="arrow" size={15}/></Link></div>
            <div className="provider-grid">{(overview?.providers ?? []).map((provider) => {
              const terminalPayments = provider.succeeded_24h + provider.failed_24h;
              const rate = terminalPayments ? Math.round((provider.succeeded_24h / terminalPayments) * 100) : 0;
              return <article className={`provider-card ${provider.available ? "available" : "planned"}`} key={provider.code}>
                <BrandLogo code={provider.code} label={provider.name} className={`provider-logo ${provider.code}`} priority={provider.code === "xendit"}/>
                <div className="provider-name"><strong>{provider.name}</strong><span><i/>{provider.available ? "Available" : "Planned"}</span></div>
                <div className="provider-methods">{provider.payment_methods.slice(0, 4).map((method) => <span key={method}>{method.toUpperCase()}</span>)}{provider.payment_methods.length > 4 && <span>+{provider.payment_methods.length - 4}</span>}</div>
                <div className="provider-stats"><span><small>Payments</small><strong>{provider.payments_24h}</strong></span><span><small>Success</small><strong>{provider.available ? `${rate}%` : "—"}</strong></span><span><small>Volume</small><strong>{provider.available ? formatIDR(provider.volume_24h, true) : "—"}</strong></span></div>
              </article>;
            })}</div>
          </section>

          <section className="bottom-grid">
            <article className="dashboard-panel transactions-panel">
              <div className="panel-heading"><div><p className="panel-kicker">LATEST ACTIVITY</p><h2>Recent payments</h2></div><Link className="secondary-button" href="/payments">View all <Icon name="arrow" size={15}/></Link></div>
              <div className="transaction-table" role="table">
                <div className="transaction-row table-head" role="row"><span>Reference</span><span>Provider</span><span>Amount</span><span>Status</span><span>Created</span></div>
                {(overview?.recent_payments ?? []).map((payment) => <Link className="transaction-row" role="row" href={`/payments/${payment.id}`} key={payment.id}>
                  <span><strong>{payment.merchant_reference}</strong><small>{payment.id.slice(0, 18)}… · {payment.environment}</small></span>
                  <span><i className={`provider-dot ${payment.provider_code}`}/>{payment.provider_code}</span>
                  <span><strong>{payment.currency === "IDR" ? formatIDR(payment.amount) : `${payment.currency} ${payment.amount}`}</strong></span>
                  <span><b className={`status-badge ${statusClass(payment.status)}`}><i/>{payment.status}</b></span>
                  <span>{new Intl.DateTimeFormat("id-ID", { hour: "2-digit", minute: "2-digit", day: "2-digit", month: "short" }).format(new Date(payment.created_at))}</span>
                </Link>)}
                {(overview?.recent_payments ?? []).length === 0 && <div className="table-empty"><Icon name="payment" size={24}/><strong>No payments yet</strong><span>New payment attempts will appear here.</span></div>}
              </div>
            </article>
            <article className="dashboard-panel operations-panel">
              <div className="panel-heading"><div><p className="panel-kicker">OPERATIONS</p><h2>Needs attention</h2></div></div>
              {[
                { label: "Unknown payments", value: overview?.operational_backlog.unknown_payments ?? 0, icon: "reconcile" as IconName, tone: "amber" },
                { label: "Pending outbox", value: overview?.operational_backlog.pending_outbox ?? 0, icon: "webhook" as IconName, tone: "blue" },
                { label: "Dead-letter events", value: overview?.operational_backlog.dead_outbox ?? 0, icon: "logs" as IconName, tone: "red" },
                { label: "Failed webhooks", value: overview?.operational_backlog.failed_webhooks ?? 0, icon: "activity" as IconName, tone: "purple" },
              ].map((item) => <div className="operation-row" key={item.label}><span className={`operation-icon ${item.tone}`}><Icon name={item.icon} size={17}/></span><div><strong>{item.label}</strong><small>{item.value === 0 ? "No action required" : "Review required"}</small></div><b className={item.value > 0 ? "has-items" : "clear"}>{item.value}</b></div>)}
              <div className="engine-checks">
                {Object.entries(health.checks ?? {}).map(([name, state]) => <div key={name}><span><i className={state === "ok" ? "online" : "offline"}/>{name}</span><strong>{state}</strong></div>)}
                <div><span><i className={observability?.slo.status === "BREACHED" ? "offline" : "online"}/>API availability</span><strong>{observability ? `${observability.slo.availability_percent.toFixed(3)}%` : "collecting"}</strong></div>
                <div><span><i className={(observability?.slo.latency_p95_ms ?? 0) > 500 ? "offline" : "online"}/>HTTP latency p95</span><strong>{observability ? `${observability.slo.latency_p95_ms} ms` : "collecting"}</strong></div>
                <div><span><i className={(observability?.connector_outcomes.unknown_outcome ?? 0) > 0 ? "offline" : "online"}/>Unknown outcomes</span><strong>{observability?.connector_outcomes.unknown_outcome ?? 0}</strong></div>
                <div><span><i className={(observability?.provider_webhooks.invalid ?? 0) > 0 ? "offline" : "online"}/>Invalid webhooks</span><strong>{observability?.provider_webhooks.invalid ?? 0}</strong></div>
              </div>
            </article>
          </section>

          <section className="roadmap-panel">
            <div><p className="panel-kicker">EMISELL ROADMAP</p><h2>What comes next</h2><p>Fokus berikutnya adalah connector gateway dan capability pembayaran yang benar-benar dibutuhkan merchant.</p></div>
            <div className="roadmap-steps"><article className="done"><span>01</span><strong>Native foundation</strong><p>Overview, payments, Xendit connector, webhook, API docs.</p><em>Available now</em></article><article><span>02</span><strong>Gateway extensions</strong><p>Midtrans, DOKU, and Duitku through the same connector contract.</p><em>Next milestone</em></article><article><span>03</span><strong>Capability expansion</strong><p>E-wallet, cards, paylater, and refunds after provider conformance.</p><em>Planned</em></article></div>
          </section>
        </main>
      </div>
    </div>
  );
}
