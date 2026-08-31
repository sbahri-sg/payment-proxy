import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { APIKeyManager } from "../components/api-keys/api-key-manager";
import { getReadiness } from "../lib/readiness";
import { listServiceAPIKeys } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function APIKeysPage() {
  const session = await requireDashboardSession("/api-keys");
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const result = await listServiceAPIKeys(session.subject).then((items) => ({ items, error: false })).catch(() => ({ items: [], error: true }));
  return (
    <div className="dashboard-app">
      <AppSidebar active="api-keys" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search API keys and service access..."/>
        <main className="dashboard-content management-content api-keys-content">
          <section className="dashboard-heading"><div><p className="breadcrumb">Developers / API keys</p><h1>Emisell Backend access</h1><p>Generate dan rotasi credential server-to-server untuk seluruh Internal Payment Gateway.</p></div><Link className="secondary-button" href="/docs#service-api-keys">API documentation <Icon name="arrow" size={15}/></Link></section>
          {result.error && <div className="dashboard-alert error"><strong>API key belum dapat dimuat.</strong><span>Periksa admin API credential dan koneksi dashboard ke Payment Proxy.</span></div>}
          <APIKeyManager initialKeys={result.items}/>
        </main>
      </div>
    </div>
  );
}
