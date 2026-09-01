import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { BrandLogo } from "../components/brand-logo";
import { getReadiness } from "../lib/readiness";
import { listProviders } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function ProvidersPage() {
  const session = await requireDashboardSession("/providers");
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const providersResult = await listProviders(session.subject).then((value) => ({ status: "fulfilled" as const, value })).catch((reason: unknown) => ({ status: "rejected" as const, reason }));
  const providers = providersResult.status === "fulfilled" ? providersResult.value : [];
  const providerCatalog = [...providers].sort((left, right) => Number(right.available) - Number(left.available) || left.name.localeCompare(right.name));
  const paymentMethods = new Set(providers.flatMap((provider) => provider.payment_methods));
  const dataError = providersResult.status === "rejected";
  return (
    <div className="dashboard-app">
      <AppSidebar active="providers" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search providers and capabilities..."/>
        <main className="dashboard-content management-content">
          <section className="dashboard-heading">
            <div><p className="breadcrumb">Control plane / Providers</p><h1>Providers</h1><p>Kelola catalog, shared runtime, payment capability, certification, dan release provider yang tersedia untuk merchant.</p></div>
          </section>
          {dataError && <div className="dashboard-alert error"><strong>Provider registry belum dapat dimuat.</strong><span>Periksa koneksi server dashboard ke Payment Proxy.</span></div>}
          <section className="management-metrics">
            <article><span>PROVIDERS</span><strong>{providers.length}</strong><small>payment connectors</small></article>
            <article><span>AVAILABLE</span><strong>{providers.filter((provider) => provider.available).length}</strong><small>certified for use</small></article>
            <article><span>PAYMENT METHODS</span><strong>{paymentMethods.size}</strong><small>canonical capabilities</small></article>
            <article><span>SETUP FIELDS</span><strong>{providers.reduce((total, provider) => total + provider.credential_schema.length, 0)}</strong><small>merchant credential schema</small></article>
          </section>
          <section className="provider-catalog">
            {providerCatalog.map((provider) => {
              return <article className={`catalog-card ${provider.available ? "available" : "planned"}`} key={provider.code}>
                <div className="catalog-card-head"><BrandLogo code={provider.code} label={provider.name} className={`catalog-logo ${provider.code}`}/><div><h2>{provider.name}</h2><span className={`availability-badge ${provider.available ? "available" : "planned"}`}><i/>{provider.available ? "Available" : "Planned"}</span></div><Link aria-label={`Open ${provider.name}`} href={`/providers/${provider.code}`}><Icon name="arrow" size={15}/></Link></div>
                <p>{provider.description}</p>
                <div className="catalog-section"><span>PAYMENT METHODS · {provider.payment_methods.length}</span><div>{provider.payment_methods.slice(0, 6).map((method) => <b key={method}>{method.replaceAll("_", " ").toUpperCase()}</b>)}{provider.payment_methods.length > 6 && <b>+{provider.payment_methods.length - 6} MORE</b>}</div></div>
                <div className="catalog-details"><span><small>Connector</small><strong>{provider.connector_code || provider.code}</strong></span><span><small>Merchant setup schema</small><strong>{provider.credential_schema.length} fields</strong></span><span><small>Catalog status</small><strong>{provider.available ? "Installable" : "Not published"}</strong></span></div>
                <div className="catalog-certification"><Icon name={provider.available ? "check" : "activity"} size={15}/><span><strong>{provider.available ? "Certified scope" : "Certification pending"}</strong><small>{provider.available ? "Conformance evidence and release gates are available in provider detail" : "Unavailable until connector conformance passes"}</small></span>{provider.available && <Link href={`/providers/${provider.code}?tab=certification`}>Evidence →</Link>}</div>
                <Link className="catalog-action" href={`/providers/${encodeURIComponent(provider.code)}`}>{provider.available ? "Manage provider" : "View provider roadmap"}<Icon name="arrow" size={15}/></Link>
              </article>;
            })}
          </section>
          <section className="security-note"><Icon name="settings" size={19}/><div><strong>Extension-managed routing</strong><p>Admin mempublikasikan provider dan schema credential. Connector extension menentukan endpoint secara otomatis; merchant cukup memasukkan API key provider ketika melakukan installation.</p></div><Link href="/docs#installations">Review contract <Icon name="arrow" size={14}/></Link></section>
        </main>
      </div>
    </div>
  );
}
