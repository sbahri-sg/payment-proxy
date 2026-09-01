import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { PaymentMethodCatalog } from "../components/management/payment-method-catalog";
import { getReadiness } from "../lib/readiness";
import { listPaymentMethods } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function PaymentMethodsPage() {
  const session = await requireDashboardSession("/payment-methods");
  const health = await getReadiness();
  const healthy = health.status === "ready";

  let dataError = false;
  const catalog = await listPaymentMethods(session.subject).catch(() => {
    dataError = true;
    return [];
  });

  const providerCodes = new Set(catalog.flatMap((method) => method.providers.map((provider) => provider.provider_code)));
  const categories = new Set(catalog.map((method) => method.category));
  const verifiedCapabilities = catalog.reduce(
    (total, method) => total + method.providers.filter((provider) => provider.support_status === "CERTIFIED").length,
    0,
  );

  return (
    <div className="dashboard-app">
      <AppSidebar active="payment-methods" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search payment method or provider..."/>
        <main className="dashboard-content management-content">
          <section className="dashboard-heading">
            <div>
              <p className="breadcrumb">Provider platform / Payment methods</p>
              <h1>Payment methods</h1>
              <p>Global canonical catalog dan capability provider yang tersedia di seluruh platform.</p>
            </div>
            <Link className="secondary-button" href="/providers">Manage providers <Icon name="arrow" size={15}/></Link>
          </section>

          {dataError && <div className="dashboard-alert error"><strong>Payment method catalog belum dapat dimuat.</strong><span>Periksa koneksi API dan migration database.</span></div>}

          <section className="management-metrics method-metrics">
            <article><span>MASTER METHODS</span><strong>{catalog.length}</strong><small>Canonical catalog</small></article>
            <article><span>PROVIDERS</span><strong>{providerCodes.size}</strong><small>Gateway capabilities</small></article>
            <article><span>VERIFIED</span><strong>{verifiedCapabilities}</strong><small>Certified capabilities</small></article>
            <article><span>CATEGORIES</span><strong>{categories.size}</strong><small>Payment method groups</small></article>
          </section>

          <PaymentMethodCatalog methods={catalog}/>
        </main>
      </div>
    </div>
  );
}
