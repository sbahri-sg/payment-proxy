import Link from "next/link";
import { notFound } from "next/navigation";
import { AppSidebar, AppTopbar, Icon } from "../../components/app-shell";
import { ProviderAppVersionManager } from "../../components/management/provider-app-manager";
import { getReadiness } from "../../lib/readiness";
import { getProviderAppProvider, listProviderAppVersions } from "../../lib/payment-proxy";
import { requireDashboardSession } from "../../lib/session";

export const dynamic = "force-dynamic";

function cleanProviderCode(value: string) {
  const code = value.toLowerCase().trim();
  return /^[a-z0-9_-]{2,48}$/.test(code) ? code : "";
}

export default async function ProviderAppDetailPage({ params }: { params: Promise<{ code: string }> }) {
  const route = await params;
  const providerCode = cleanProviderCode(route.code);
  if (!providerCode) notFound();
  const session = await requireDashboardSession(`/provider-apps/${providerCode}`);
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [providerResult, versionsResult] = await Promise.allSettled([
    getProviderAppProvider(session.subject, providerCode),
    listProviderAppVersions(session.subject, providerCode),
  ]);
  if (providerResult.status === "rejected") notFound();
  const provider = providerResult.value;
  const versions = versionsResult.status === "fulfilled" ? versionsResult.value : [];

  return <div className="dashboard-app">
    <AppSidebar active="provider-apps" healthy={healthy} engineStatus={health.status}/>
    <div className="dashboard-main">
      <AppTopbar healthy={healthy} searchPlaceholder={`Search ${provider.provider_name} versions...`}/>
      <main className="dashboard-content management-content provider-app-content">
        <section className="provider-detail-heading">
          <div className="provider-detail-identity">
            <Link className="provider-back-link" href="/provider-apps"><Icon name="arrow" size={14}/> All Connector Apps</Link>
            <div><span className="provider-detail-logo provider-app-detail-logo">{provider.provider_name.slice(0, 1).toUpperCase()}</span><span><span className={`provider-app-status ${provider.status.toLowerCase()}`}><i/>{provider.status}</span><h1>{provider.provider_name}</h1><p>{provider.description || "Provider connector identity and immutable version registry."}</p></span></div>
          </div>
          <div className="heading-actions">
            {provider.documentation_url && <a className="secondary-button" href={provider.documentation_url} target="_blank" rel="noreferrer">Provider docs <Icon name="arrow" size={14}/></a>}
            <Link className="secondary-button" href="/docs?contract=internal#provider-apps">API documentation <Icon name="arrow" size={14}/></Link>
          </div>
        </section>
        {versionsResult.status === "rejected" && <div className="dashboard-alert error"><strong>Riwayat versi belum dapat dimuat.</strong><span>Provider tetap aman dan tidak ada perubahan state yang dilakukan.</span></div>}
        <section className="provider-app-security"><Icon name="settings" size={18}/><div><strong>Provider identity locked</strong><p>Manifest ZIP wajib menggunakan code dan name yang sama dengan registry. Uploaded code tidak pernah dijalankan di Payment Kernel.</p></div><b>25 MB MAX</b></section>
        <ProviderAppVersionManager provider={provider} initialApps={versions}/>
      </main>
    </div>
  </div>;
}
