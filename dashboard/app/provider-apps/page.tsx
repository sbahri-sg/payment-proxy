import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { ProviderAppRegistry } from "../components/management/provider-app-manager";
import { getReadiness } from "../lib/readiness";
import { listProviderAppProviders } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function ProviderAppsPage() {
  const session = await requireDashboardSession("/provider-apps");
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const result = await listProviderAppProviders(session.subject).then((items) => ({ items, error: false })).catch(() => ({ items: [], error: true }));
  return <div className="dashboard-app">
    <AppSidebar active="provider-apps" healthy={healthy} engineStatus={health.status}/>
    <div className="dashboard-main">
      <AppTopbar healthy={healthy} searchPlaceholder="Search Provider Apps and versions..."/>
      <main className="dashboard-content management-content provider-app-content">
        <section className="dashboard-heading"><div><p className="breadcrumb">Developers / Connector Apps</p><h1>Provider App registry</h1><p>Create a provider identity, then upload and certify its immutable connector versions.</p></div><Link className="secondary-button" href="/docs?contract=internal#provider-apps">API documentation <Icon name="arrow" size={15}/></Link></section>
        {result.error && <div className="dashboard-alert error"><strong>Provider Apps belum dapat dimuat.</strong><span>Periksa migrasi database dan admin API connection.</span></div>}
        <section className="provider-app-security"><Icon name="settings" size={18}/><div><strong>Kernel isolation enforced</strong><p>Uploaded code is never loaded inside the Payment Kernel. Production publish requires a separately deployed runtime with the exact certified manifest version.</p></div><b>25 MB MAX</b></section>
        <ProviderAppRegistry initialProviders={result.items}/>
      </main>
    </div>
  </div>;
}
