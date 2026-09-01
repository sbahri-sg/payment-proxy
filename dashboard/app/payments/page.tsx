import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { getReadiness } from "../lib/readiness";
import { listPayments, listProviders, type PaymentSession, type PaymentStatus } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

const paymentStatuses: PaymentStatus[] = ["CREATED", "PROCESSING", "PENDING", "SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED", "UNKNOWN"];

function scalar(value: string | string[] | undefined) {
  return typeof value === "string" ? value.trim() : "";
}

function statusTone(status: PaymentStatus) {
  if (status === "SUCCEEDED") return "success";
  if (status === "FAILED") return "failed";
  if (["PENDING", "PROCESSING", "UNKNOWN"].includes(status)) return "pending";
  return "neutral";
}

function formatAmount(payment: PaymentSession) {
  if (payment.currency === "IDR") return new Intl.NumberFormat("id-ID", { style: "currency", currency: "IDR", maximumFractionDigits: 0 }).format(payment.amount);
  return `${payment.currency} ${payment.amount.toLocaleString("id-ID")}`;
}

function paymentHref(values: { q: string; merchantID: string; status: string; provider: string; environment: string }, offset: number) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value) query.set(key === "merchantID" ? "merchant_id" : key, value);
  if (offset > 0) query.set("offset", String(offset));
  return `/payments${query.size ? `?${query}` : ""}`;
}

export default async function PaymentsPage({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const session = await requireDashboardSession("/payments");
  const input = await searchParams;
  const q = scalar(input.q).slice(0, 128);
  const merchantInput = scalar(input.merchant_id).slice(0, 128);
  const merchantID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(merchantInput) ? merchantInput : "";
  const statusInput = scalar(input.status).toUpperCase();
  const status = paymentStatuses.includes(statusInput as PaymentStatus) ? statusInput : "";
  const provider = scalar(input.provider).toLowerCase().slice(0, 64);
  const environmentInput = scalar(input.environment).toLowerCase();
  const environment = environmentInput === "sandbox" || environmentInput === "live" ? environmentInput : "";
  const offsetValue = Number(scalar(input.offset));
  const offset = Number.isSafeInteger(offsetValue) && offsetValue >= 0 ? Math.min(offsetValue, 10000) : 0;
  const limit = 25;
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [paymentsResult, providersResult] = await Promise.allSettled([
    listPayments(session.subject, { q, merchantID, status, provider, environment, limit, offset }),
    listProviders(session.subject),
  ]);
  const result = paymentsResult.status === "fulfilled" ? paymentsResult.value : { items: [], total: 0, limit, offset, has_more: false };
  const providers = providersResult.status === "fulfilled" ? providersResult.value : [];
  const visibleSucceeded = result.items.filter((item) => item.status === "SUCCEEDED").length;
  const visiblePending = result.items.filter((item) => item.status === "PENDING" || item.status === "PROCESSING").length;
  const visibleUnknown = result.items.filter((item) => item.status === "UNKNOWN").length;
  const filters = { q, merchantID, status, provider, environment };
  const first = result.total ? offset + 1 : 0;
  const last = Math.min(offset + result.items.length, result.total);

  return (
    <div className="dashboard-app">
      <AppSidebar active="payments" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search payment or merchant reference..."/>
        <main className="dashboard-content management-content payments-content">
          <section className="dashboard-heading">
            <div><p className="breadcrumb">Workspace / Payments</p><h1>Payments</h1><p>Track every payment attempt, provider response, and customer next action.</p></div>
            <Link className="secondary-button" href="/docs#payments">Payment API <Icon name="arrow" size={15}/></Link>
          </section>
          {paymentsResult.status === "rejected" && <div className="dashboard-alert error"><strong>Payment data belum dapat dimuat.</strong><span>Periksa koneksi API dan service credential dashboard.</span></div>}
          <section className="management-metrics payment-metrics">
            <article><span>MATCHING RECORDS</span><strong>{result.total.toLocaleString("id-ID")}</strong><small>Semua payment sesuai filter</small></article>
            <article><span>SUCCEEDED ON PAGE</span><strong>{visibleSucceeded}</strong><small>Dari {result.items.length} payment terlihat</small></article>
            <article><span>AWAITING ACTION</span><strong>{visiblePending}</strong><small>Pending atau processing</small></article>
            <article><span>UNKNOWN ON PAGE</span><strong>{visibleUnknown}</strong><small>{visibleUnknown ? "Perlu sinkronisasi manual" : "Tidak ada anomali"}</small></article>
          </section>
          <form className="payment-filter-bar" method="get">
            <label className="payment-filter-search"><Icon name="search" size={15}/><input name="q" defaultValue={q} placeholder="Payment ID, reference, engine ID"/></label>
            <label className="payment-filter-search"><Icon name="provider" size={15}/><input name="merchant_id" defaultValue={merchantID} placeholder="Merchant ID"/></label>
            <select name="status" defaultValue={status} aria-label="Filter status"><option value="">All statuses</option>{paymentStatuses.map((item) => <option value={item} key={item}>{item}</option>)}</select>
            <select name="provider" defaultValue={provider} aria-label="Filter provider"><option value="">All providers</option>{providers.map((item) => <option value={item.code} key={item.code}>{item.name}</option>)}</select>
            <select name="environment" defaultValue={environment} aria-label="Filter environment"><option value="">All environments</option><option value="sandbox">Sandbox</option><option value="live">Live</option></select>
            <button className="dashboard-primary-button" type="submit">Apply filters</button>
            {(q || merchantID || status || provider || environment) && <Link className="secondary-button" href="/payments">Reset</Link>}
          </form>
          <section className="dashboard-panel payment-list-panel">
            <div className="panel-heading"><div><p className="panel-kicker">PAYMENT OPERATIONS</p><h2>Transaction activity</h2><p>Newest status changes are shown first.</p></div><span>{first}–{last} of {result.total}</span></div>
            <div className="payment-table" role="table">
              <div className="payment-table-row payment-table-head" role="row"><span>Payment</span><span>Provider</span><span>Amount</span><span>Status</span><span>Updated</span><span/></div>
              {result.items.map((payment) => <Link className="payment-table-row" href={`/payments/${payment.id}`} role="row" key={payment.id}>
                <span><strong>{payment.merchant_reference}</strong><small>{payment.merchant_id} · {payment.id}</small></span>
                <span><i className={`provider-dot ${payment.provider_code}`}/><b>{payment.provider_code}</b><small>{payment.environment}</small></span>
                <span><strong>{formatAmount(payment)}</strong><small>{payment.currency} amount: {payment.amount.toLocaleString("id-ID")}</small></span>
                <span><b className={`status-badge ${statusTone(payment.status)}`}><i/>{payment.status}</b></span>
                <span>{new Intl.DateTimeFormat("id-ID", { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(payment.updated_at))}</span>
                <span><Icon name="arrow" size={15}/></span>
              </Link>)}
              {result.items.length === 0 && <div className="management-empty payment-empty"><span><Icon name="payment" size={25}/></span><h3>No payments found</h3><p>Ubah filter atau buat payment baru melalui Payment API.</p></div>}
            </div>
            <footer className="payment-pagination"><span>Showing {first}–{last}</span><nav>{offset > 0 ? <Link className="secondary-button" href={paymentHref(filters, Math.max(0, offset - limit))}>Previous</Link> : <span className="secondary-button disabled">Previous</span>}{result.has_more ? <Link className="secondary-button" href={paymentHref(filters, offset + limit)}>Next</Link> : <span className="secondary-button disabled">Next</span>}</nav></footer>
          </section>
        </main>
      </div>
    </div>
  );
}
