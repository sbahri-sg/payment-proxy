import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { BrandLogo } from "../components/brand-logo";
import { getReadiness } from "../lib/readiness";
import { listInstallations, listProviders } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function ProvidersPage() {
  const session = await requireDashboardSession("/providers");
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [providersResult, installationsResult] = await Promise.allSettled([listProviders(session.subject), listInstallations(session.subject)]);
  const providers = providersResult.status === "fulfilled" ? providersResult.value : [];
  const providerCatalog = [...providers].sort((left, right) => Number(right.available) - Number(left.available) || left.name.localeCompare(right.name));
  const installations = installationsResult.status === "fulfilled" ? installationsResult.value : [];
  const dataError = providersResult.status === "rejected" || installationsResult.status === "rejected";
  return (
    <div className="dashboard-app">
      <AppSidebar active="providers" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search providers and capabilities..."/>
        <main className="dashboard-content management-content">
          <section className="dashboard-heading">
            <div><p className="breadcrumb">Connections / Payment providers</p><h1>Payment providers</h1><p>Provider registry, certified capability, and connector availability for this workspace.</p></div>
            <Link className="dashboard-primary-action" href="/installations">Manage installations <Icon name="arrow" size={16}/></Link>
          </section>
          {dataError && <div className="dashboard-alert error"><strong>Provider registry belum dapat dimuat.</strong><span>Periksa koneksi server dashboard ke Payment Proxy.</span></div>}
          <section className="management-metrics">
            <article><span>REGISTERED</span><strong>{providers.length}</strong><small>provider connectors</small></article>
            <article><span>AVAILABLE</span><strong>{providers.filter((provider) => provider.available).length}</strong><small>certified for use</small></article>
            <article><span>ACTIVE</span><strong>{installations.filter((item) => item.status === "ACTIVE").length}</strong><small>workspace installations</small></article>
            <article><span>ENGINE</span><strong>Emisell Kernel</strong><small>isolated connector runtime</small></article>
          </section>
          <section className="provider-catalog">
            {providerCatalog.map((provider) => {
              const current = installations.filter((item) => item.provider_code === provider.code && item.status !== "UNINSTALLED");
              return <article className={`catalog-card ${provider.available ? "available" : "planned"}`} key={provider.code}>
                <div className="catalog-card-head"><BrandLogo code={provider.code} label={provider.name} className={`catalog-logo ${provider.code}`}/><div><h2>{provider.name}</h2><span className={`availability-badge ${provider.available ? "available" : "planned"}`}><i/>{provider.available ? "Available" : "Planned"}</span></div><Link aria-label={`Open ${provider.name}`} href={`/providers/${provider.code}`}><Icon name="arrow" size={15}/></Link></div>
                <p>{provider.description}</p>
                <div className="catalog-section"><span>PAYMENT METHODS · {provider.payment_methods.length}</span><div>{provider.payment_methods.slice(0, 6).map((method) => <b key={method}>{method.replaceAll("_", " ").toUpperCase()}</b>)}{provider.payment_methods.length > 6 && <b>+{provider.payment_methods.length - 6} MORE</b>}</div></div>
                <div className="catalog-details"><span><small>Environments</small><strong>{provider.environments.join(" · ")}</strong></span><span><small>Credential fields</small><strong>{provider.credential_schema.length}</strong></span><span><small>Installations</small><strong>{current.length}</strong></span></div>
                <div className="catalog-certification"><Icon name={provider.available ? "check" : "activity"} size={15}/><span><strong>{provider.available ? "Certified scope" : "Certification pending"}</strong><small>{provider.available ? "Evidence and sandbox release gates are available in provider detail" : "Unavailable until connector conformance passes"}</small></span>{provider.available && <Link href={`/providers/${provider.code}?tab=certification&environment=sandbox`}>Evidence →</Link>}</div>
                <Link className="catalog-action" href={`/providers/${provider.code}`}>Open provider<Icon name="arrow" size={15}/></Link>
              </article>;
            })}
          </section>
          <section className="security-note"><Icon name="settings" size={19}/><div><strong>Credential isolation</strong><p>Provider credentials are encrypted with AES-GCM inside the Emisell vault, scoped per installation, and never returned to the browser.</p></div><Link href="/docs#installations">Review contract <Icon name="arrow" size={14}/></Link></section>
        </main>
      </div>
    </div>
  );
}
