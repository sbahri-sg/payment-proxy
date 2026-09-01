import Link from "next/link";
import QRCode from "qrcode";
import { notFound } from "next/navigation";
import { AppSidebar, AppTopbar, Icon } from "../../components/app-shell";
import { BrandLogo } from "../../components/brand-logo";
import { PaymentActions } from "../../components/payments/payment-actions";
import { getPayment, getPaymentTimeline, PaymentProxyError, type PaymentSession, type PaymentStatus } from "../../lib/payment-proxy";
import { getReadiness } from "../../lib/readiness";
import { requireDashboardSession } from "../../lib/session";

export const dynamic = "force-dynamic";

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

function sourceLabel(source: string) {
  const labels: Record<string, string> = {
    "api.create": "Payment Proxy",
    "engine.create": "Payment engine",
    "engine.sync": "Manual sync",
    "operator.cancel": "Operator cancel",
    "webhook.xendit": "Xendit webhook",
    "migration.backfill": "Imported history",
  };
  return labels[source] ?? source;
}

export default async function PaymentDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  if (!/^pay_[A-Za-z0-9]+$/.test(id)) notFound();
  const session = await requireDashboardSession(`/payments/${id}`);
  const health = await getReadiness();
  const healthy = health.status === "ready";
  let payment: PaymentSession;
  try {
    payment = await getPayment(session.subject, id);
  } catch (error) {
    if (error instanceof PaymentProxyError && error.status === 404) notFound();
    throw error;
  }
  const timeline = await getPaymentTimeline(session.subject, id).catch(() => []);
  const action = payment.next_action;
  const redirectCandidate = payment.checkout_url || action?.redirect_url || "";
  const redirectURL = redirectCandidate.startsWith("https://") ? redirectCandidate : "";
  let qrImage = action?.image_data_url?.startsWith("data:image/png;base64,") ? action.image_data_url : action?.qr_code_url?.startsWith("https://") ? action.qr_code_url : "";
  if (!qrImage && action?.raw_qr_data) qrImage = await QRCode.toDataURL(action.raw_qr_data, { width: 460, margin: 2, errorCorrectionLevel: "M" }).catch(() => "");
  const created = new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "medium" }).format(new Date(payment.created_at));
  const updated = new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "medium" }).format(new Date(payment.updated_at));

  return (
    <div className="dashboard-app">
      <AppSidebar active="payments" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search payment or merchant reference..."/>
        <main className="dashboard-content management-content payment-detail-content">
          <section className="dashboard-heading payment-detail-heading">
            <div><p className="breadcrumb"><Link href="/payments">Payments</Link> / {payment.merchant_reference}</p><div className="payment-title-line"><h1>{payment.merchant_reference}</h1><b className={`status-badge ${statusTone(payment.status)}`}><i/>{payment.status}</b><span className={`environment-badge ${payment.environment}`}>{payment.environment}</span></div><p>{payment.id}</p></div>
            <PaymentActions payment={payment}/>
          </section>
          {payment.status === "UNKNOWN" && <div className="unknown-payment-alert"><Icon name="activity" size={19}/><div><strong>Outcome payment belum diketahui</strong><p>Jangan membuat payment pengganti atau melakukan failover otomatis. Gunakan Sync status sampai engine atau webhook memberikan status kanonis.</p></div></div>}
          {payment.flags?.includes("late_payment") && <div className="unknown-payment-alert"><Icon name="activity" size={19}/><div><strong>Pembayaran diterima setelah kedaluwarsa</strong><p>Status tetap SUCCEEDED dan ditandai <code>late_payment</code>. Emisell Backend harus menjalankan kebijakan order terlambat secara eksplisit, bukan mengabaikan pembayaran.</p></div></div>}
          {payment.flags?.includes("provider_delayed_confirmation") && <div className="dashboard-alert"><strong>Provider delayed confirmation</strong><span>Provider akhirnya mengonfirmasi SUCCEEDED setelah outcome sebelumnya UNKNOWN. Jangan membuat transaksi pengganti untuk payment ini.</span></div>}
          {payment.last_error && <div className="dashboard-alert error"><strong>Last engine error</strong><span>{payment.last_error}</span></div>}
          <section className="payment-detail-layout">
            <div className="payment-detail-main">
              <article className="dashboard-panel payment-summary-card">
                <div className="panel-heading"><div><p className="panel-kicker">PAYMENT SUMMARY</p><h2>{formatAmount(payment)}</h2><p>Canonical amount recorded by Payment Proxy.</p></div><BrandLogo code={payment.provider_code} label={payment.provider_code} className={`provider-logo ${payment.provider_code}`}/></div>
                <dl className="payment-data-grid">
                  <div><dt>Merchant ID</dt><dd><code>{payment.merchant_id}</code></dd></div><div><dt>Provider</dt><dd>{payment.provider_code.toUpperCase()}</dd></div>
                  <div><dt>Environment</dt><dd>{payment.environment}</dd></div>
                  <div><dt>Provider release</dt><dd><code>{payment.provider_version}</code></dd></div><div><dt>Checkout mode</dt><dd><code>{payment.checkout_mode}</code></dd></div>
                  <div><dt>Payment method</dt><dd><code>{payment.payment_method_code || "Selected on provider page"}</code></dd></div>
                  <div><dt>Installation ID</dt><dd><code>{payment.installation_id}</code></dd></div><div><dt>Currency / amount</dt><dd>{payment.currency} · {payment.amount.toLocaleString("id-ID")}</dd></div>
                  <div><dt>Created</dt><dd>{created}</dd></div><div><dt>Last updated</dt><dd>{updated}</dd></div>
                </dl>
              </article>
              <article className="dashboard-panel payment-reference-card">
                <div className="panel-heading"><div><p className="panel-kicker">TRACEABILITY</p><h2>Provider references</h2><p>Use these identifiers for connector and provider investigation.</p></div></div>
                <dl className="payment-reference-list"><div><dt>Local payment ID</dt><dd><code>{payment.id}</code></dd></div><div><dt>Provider payment ID</dt><dd><code>{payment.provider_payment_id || "Not assigned"}</code></dd></div><div><dt>Provider transaction ID</dt><dd><code>{payment.connector_transaction_id || "Not assigned"}</code></dd></div><div><dt>Merchant reference</dt><dd><code>{payment.merchant_reference}</code></dd></div></dl>
              </article>
              <article className="dashboard-panel timeline-card">
                <div className="panel-heading"><div><p className="panel-kicker">STATUS HISTORY</p><h2>Payment timeline</h2><p>Durable transitions recorded by create, sync, cancel, and webhook flows.</p></div></div>
                <div className="payment-timeline">{timeline.map((event, index) => <div className="timeline-event" key={event.id}><span className={`timeline-dot ${statusTone(event.status)}`}>{index + 1}</span><div><strong>{event.status}</strong><small>{sourceLabel(event.source)}</small><time>{new Intl.DateTimeFormat("id-ID", { dateStyle: "medium", timeStyle: "medium" }).format(new Date(event.created_at))}</time></div></div>)}{timeline.length === 0 && <p className="timeline-empty">Riwayat status belum tersedia.</p>}</div>
              </article>
            </div>
            <aside className="payment-detail-side">
              <article className="dashboard-panel next-action-card">
                <div className="panel-heading"><div><p className="panel-kicker">CUSTOMER NEXT ACTION</p><h2>{action?.type === "qr_code_information" ? "Scan QRIS" : action?.type === "virtual_account_information" ? "Virtual Account" : action?.type === "redirect" ? "Provider checkout" : action?.type === "mobile_authorization" ? "Approve in mobile app" : "Payment instruction"}</h2></div></div>
                {qrImage ? <div className="qris-preview"><img src={qrImage} alt={`QRIS untuk payment ${payment.merchant_reference}`}/><strong>QRIS sandbox</strong><span>Tampilkan QR ini ke customer hingga payment selesai atau kedaluwarsa.</span></div> : action?.display_text ? <div className="qris-preview"><strong>Nomor Virtual Account</strong><code>{action.display_text}</code><span>Nomor ini harus dibayar sesuai nominal sebelum kedaluwarsa.</span></div> : redirectURL ? <div className="next-action-empty redirect-action"><Icon name="arrow" size={23}/><strong>Hosted checkout ready</strong><span>Buka halaman aman milik {payment.provider_code} untuk memilih metode dan menyelesaikan pembayaran.</span><a className="dashboard-primary-button" href={redirectURL} target="_blank" rel="noreferrer">Open provider checkout</a></div> : action?.type === "mobile_authorization" ? <div className="next-action-empty"><Icon name="activity" size={23}/><strong>Approve in customer app</strong><span>Selesaikan approval pada aplikasi atau nomor ponsel customer, lalu sync status.</span></div> : <div className="next-action-empty"><Icon name="check" size={23}/><strong>No customer action</strong><span>{payment.status === "SUCCEEDED" ? "Payment sudah selesai." : "Connector tidak mengirim customer action yang dapat ditampilkan."}</span></div>}
                {action?.raw_qr_data && <details className="raw-action"><summary>Raw QR payload</summary><code>{action.raw_qr_data}</code></details>}
              </article>
              <article className="operational-guardrail"><Icon name="reconcile" size={18}/><div><strong>Canonical status guardrail</strong><p>Terminal status tidak diturunkan oleh polling yang terlambat. `UNKNOWN` harus direkonsiliasi, bukan langsung difailover.</p></div></article>
            </aside>
          </section>
        </main>
      </div>
    </div>
  );
}
