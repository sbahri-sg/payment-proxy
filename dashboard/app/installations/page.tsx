import Link from "next/link";
import { AppSidebar, AppTopbar, Icon } from "../components/app-shell";
import { BrandLogo } from "../components/brand-logo";
import { CredentialForm } from "../components/management/credential-form";
import { InstallProviderForm } from "../components/management/install-provider-form";
import { InstallationActions } from "../components/management/installation-actions";
import { ProviderWebhookStatus } from "../components/management/provider-webhook-status";
import { getReadiness } from "../lib/readiness";
import { listInstallations, listProviders, type Installation } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

function statusTone(status: Installation["status"]) {
  if (status === "ACTIVE" || status === "READY") return "success";
  if (status === "ERROR") return "failed";
  if (status === "VERIFYING" || status === "CONFIG_REQUIRED") return "pending";
  return "neutral";
}

function reached(status: Installation["status"], stage: number) {
  const progress: Record<Installation["status"], number> = { CONFIG_REQUIRED: 1, VERIFYING: 2, READY: 3, ACTIVE: 4, INACTIVE: 4, ERROR: 2, UNINSTALLED: 4 };
  return progress[status] >= stage;
}

export default async function InstallationsPage({ searchParams }: { searchParams: Promise<{ provider?: string; environment?: string }> }) {
  const session = await requireDashboardSession("/installations");
  const query = await searchParams;
  const environment = query.environment === "sandbox" || query.environment === "live" ? query.environment : undefined;
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [providersResult, installationsResult] = await Promise.allSettled([listProviders(session.subject), listInstallations(session.subject, environment)]);
  const providers = providersResult.status === "fulfilled" ? providersResult.value : [];
  const availableProviders = providers.filter((provider) => provider.available);
  const installations = installationsResult.status === "fulfilled" ? installationsResult.value : [];
  const dataError = providersResult.status === "rejected" || installationsResult.status === "rejected";
  const merchantID = process.env.DASHBOARD_MERCHANT_ID?.trim() ?? "";
  return (
    <div className="dashboard-app">
      <AppSidebar active="installations" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder="Search installation ID or provider..."/>
        <main className="dashboard-content management-content">
          <section className="dashboard-heading">
            <div><p className="breadcrumb">Connections / Merchant gateways</p><h1>Provider installations</h1><p>Manage sandbox and live connector lifecycle without exposing provider credentials.</p></div>
            <Link className="secondary-button" href="/providers">Provider registry <Icon name="arrow" size={15}/></Link>
          </section>
          {dataError && <div className="dashboard-alert error"><strong>Installation data belum dapat dimuat.</strong><span>Periksa service key dan tenant dashboard.</span></div>}
          <section className="installation-toolbar">
            <nav aria-label="Environment filter"><Link className={!environment ? "active" : ""} href="/installations">All</Link><Link className={environment === "sandbox" ? "active" : ""} href="/installations?environment=sandbox">Sandbox</Link><Link className={environment === "live" ? "active" : ""} href="/installations?environment=live">Live</Link></nav>
            <span><i className={healthy ? "online" : "offline"}/>{healthy ? "Engine connected" : "Engine unavailable"}</span>
          </section>
          <details className="install-composer" open={Boolean(query.provider) || installations.length === 0}>
            <summary><span className="composer-icon"><Icon name="install"/></span><span><strong>Install a provider</strong><small>Create an isolated connector for sandbox or live processing.</small></span><b>⌄</b></summary>
            <div className="composer-body"><InstallProviderForm providers={availableProviders} selectedProvider={query.provider} merchantID={merchantID}/></div>
          </details>
          <section className="installation-list">
            <div className="section-title"><div><p className="panel-kicker">WORKSPACE CONNECTIONS</p><h2>Installed providers</h2></div><span>{installations.length} records</span></div>
            {installations.map((installation) => {
              const provider = providers.find((item) => item.code === installation.provider_code);
              const installedAt = new Intl.DateTimeFormat("id-ID", { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(installation.updated_at));
              const configuredCredentials = installation.credential_metadata.configured_fields?.filter((field) => field.configured).length ?? 0;
              const credentialUpdatedAt = installation.credential_metadata.configured_at ? new Intl.DateTimeFormat("id-ID", { day: "2-digit", month: "short", year: "numeric" }).format(new Date(installation.credential_metadata.configured_at)) : "Not configured";
              return <article className={`installation-card ${installation.status === "UNINSTALLED" ? "is-uninstalled" : ""}`} key={installation.id}>
                <header className="installation-head"><BrandLogo code={installation.provider_code} label={installation.provider_name} className={`catalog-logo ${installation.provider_code}`}/><div className="installation-identity"><div><h3>{installation.provider_name}</h3><span className={`status-badge ${statusTone(installation.status)}`}><i/>{installation.status.replaceAll("_", " ")}</span><span className={`environment-badge ${installation.environment}`}>{installation.environment}</span></div><code>{installation.id}</code></div><div className="installation-version"><small>Version</small><strong>v{installation.version}</strong></div></header>
                <div className="merchant-connection-strip"><span><Icon name="provider" size={17}/></span><div><small>MERCHANT ID</small><code>{installation.merchant_id}</code></div><em>API key hidden</em></div>
                <div className="lifecycle-track">{["Installed","Credential","Ready","Active"].map((label, index) => <div className={reached(installation.status,index+1) ? "reached" : ""} key={label}><i>{reached(installation.status,index+1) ? "✓" : index+1}</i><span>{label}</span></div>)}</div>
                <div className="installation-meta"><span><small>Credential</small><strong>{configuredCredentials ? `${configuredCredentials} fields configured` : "Not configured"}</strong></span><span><small>Credential verified</small><strong>{credentialUpdatedAt}</strong></span><span><small>Connector</small><strong>{installation.connector_id ? `${installation.connector_id.slice(0,20)}${installation.connector_id.length > 20 ? "…" : ""}` : "Not configured"}</strong></span><span><small>Runtime</small><strong>{installation.execution_engine === "emisell_native" ? "Emisell native" : "Legacy read-only"}</strong></span><span><small>Updated</small><strong>{installedAt}</strong></span></div>
                {installation.last_error && <div className="installation-error"><Icon name="activity" size={16}/><span><strong>Setup requires attention</strong><small>{installation.last_error}</small></span></div>}
                <ProviderWebhookStatus installation={installation}/>
                {!['ACTIVE','VERIFYING','UNINSTALLED'].includes(installation.status) && provider && <CredentialForm installation={installation} schema={provider.credential_schema}/>}
                <InstallationActions installation={installation}/>
              </article>;
            })}
            {installations.length === 0 && <div className="management-empty"><span><Icon name="install" size={26}/></span><h3>No provider installed</h3><p>Start with the certified Xendit sandbox connector, then configure and verify its API key.</p></div>}
          </section>
        </main>
      </div>
    </div>
  );
}
