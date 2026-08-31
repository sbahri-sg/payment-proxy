import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { BrandLogo } from "../components/brand-logo";
import { PaymentMethodAssignmentActions } from "../components/management/payment-method-assignment-actions";
import { PaymentMethodAssignmentForm } from "../components/management/payment-method-assignment-form";
import { PaymentMethodCatalog } from "../components/management/payment-method-catalog";
import { getReadiness } from "../lib/readiness";
import { listInstallations, listPaymentMethodAssignments, listPaymentMethods, listPaymentOptions } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function PaymentMethodsPage({ searchParams }: { searchParams: Promise<{ environment?: string }> }) {
  const session = await requireDashboardSession("/payment-methods");
  const query = await searchParams;
  const environment = query.environment === "live" ? "live" : "sandbox";
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [assignmentsResult, installationsResult, optionsResult, catalogResult] = await Promise.allSettled([
    listPaymentMethodAssignments(session.subject, environment),
    listInstallations(session.subject, environment),
    listPaymentOptions(session.subject, environment),
    listPaymentMethods(session.subject),
  ]);
  const assignments = assignmentsResult.status === "fulfilled" ? assignmentsResult.value : [];
  const installations = installationsResult.status === "fulfilled" ? installationsResult.value.filter((item) => item.status === "ACTIVE") : [];
  const options = optionsResult.status === "fulfilled" ? optionsResult.value : [];
  const catalog = catalogResult.status === "fulfilled" ? catalogResult.value : [];
  const dataError = assignmentsResult.status === "rejected" || installationsResult.status === "rejected" || optionsResult.status === "rejected" || catalogResult.status === "rejected";
  return (
    <div className="dashboard-app">
      <AppSidebar active="payment-methods" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search payment method or gateway..."/>
        <main className="dashboard-content management-content">
          <section className="dashboard-heading">
            <div><p className="breadcrumb">Connections / Checkout methods</p><h1>Checkout payment methods</h1><p>Choose which installed gateway processes each payment method. No automatic routing or failover.</p></div>
            <Link className="secondary-button" href="/installations">Manage installations <Icon name="arrow" size={15}/></Link>
          </section>
          {dataError && <div className="dashboard-alert error"><strong>Payment method data belum dapat dimuat.</strong><span>Periksa koneksi API dan migration database.</span></div>}
          <section className="installation-toolbar method-toolbar">
            <nav aria-label="Environment filter"><Link className={environment === "sandbox" ? "active" : ""} href="/payment-methods?environment=sandbox">Sandbox</Link><Link className={environment === "live" ? "active" : ""} href="/payment-methods?environment=live">Live</Link></nav>
            <span><i className={healthy ? "online" : "offline"}/>{options.length} options available at checkout</span>
          </section>
          <section className="management-metrics method-metrics">
            <article><span>CHECKOUT OPTIONS</span><strong>{options.length}</strong><small>Active and ready</small></article>
            <article><span>MASTER METHODS</span><strong>{catalog.length}</strong><small>Canonical catalog</small></article>
            <article><span>ACTIVE GATEWAYS</span><strong>{installations.length}</strong><small>Eligible installations</small></article>
            <article><span>SELECTION MODE</span><strong>Explicit</strong><small>Merchant-controlled mapping</small></article>
          </section>
          <PaymentMethodCatalog methods={catalog}/>
          {installations.length === 0 ? <div className="dashboard-alert error"><strong>No active {environment} gateway.</strong><span>Configure and activate an installation before assigning checkout methods.</span><Link href={`/installations?environment=${environment}`}>Open installations <Icon name="arrow" size={14}/></Link></div> : (
            <details className="install-composer method-composer" open={assignments.length === 0}>
              <summary><span className="composer-icon"><Icon name="wallet"/></span><span><strong>Assign a payment method</strong><small>Map one checkout method to one active gateway in {environment}.</small></span><b>⌄</b></summary>
              <div className="composer-body"><PaymentMethodAssignmentForm environment={environment} installations={installations} assignments={assignments} catalog={catalog}/></div>
            </details>
          )}
          <section className="dashboard-panel method-assignment-panel">
            <div className="panel-heading"><div><p className="panel-kicker">MERCHANT-CONTROLLED GATEWAY SELECTION</p><h2>Method assignments</h2><p>Existing payments keep their original gateway binding when this mapping changes.</p></div><span>{assignments.length} records</span></div>
            <div className="method-assignment-head"><span>Checkout option</span><span>Gateway</span><span>Environment</span><span>Status</span><span>Option ID</span><span>Action</span></div>
            {assignments.map((assignment) => {
              const installation = installations.find((item) => item.id === assignment.installation_id);
              const available = assignment.status === "ACTIVE" && Boolean(installation);
              return <article className="method-assignment-row" key={assignment.id}>
                <span><strong>{assignment.label}</strong><small>{assignment.payment_method_code} · connector: {assignment.payment_method}/{assignment.payment_method_type}</small></span>
                <span><BrandLogo code={assignment.provider_code} label={assignment.provider_name} className={`catalog-logo ${assignment.provider_code}`}/><span><strong>{assignment.provider_name}</strong><small>{assignment.installation_id}</small></span></span>
                <span><em className={`environment-badge ${assignment.environment}`}>{assignment.environment}</em></span>
                <span><span className={`status-badge ${available ? "success" : assignment.status === "INACTIVE" ? "neutral" : "failed"}`}><i/>{available ? "AVAILABLE" : assignment.status === "INACTIVE" ? "INACTIVE" : "GATEWAY OFFLINE"}</span></span>
                <span><code>{assignment.id}</code><small>v{assignment.version}</small></span>
                <PaymentMethodAssignmentActions assignment={assignment}/>
              </article>;
            })}
            {assignments.length === 0 && <div className="management-empty method-empty"><span><Icon name="wallet" size={26}/></span><h3>No payment method assigned</h3><p>Assign QRIS to the active Xendit installation. The resulting option ID can be used by checkout.</p></div>}
          </section>
          <section className="security-note method-guardrail"><Icon name="route" size={21}/><div><strong>Assignment, not smart routing</strong><p>A new payment is bound to the selected installation. Changing this page only affects future payments and never reroutes an existing or UNKNOWN payment.</p></div><Link href="/docs">View API contract <Icon name="arrow" size={14}/></Link></section>
        </main>
      </div>
    </div>
  );
}
