import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { AppSidebar, AppTopbar, Icon } from "../../components/app-shell";
import { BrandLogo } from "../../components/brand-logo";
import { ProviderAppVersionManager, ProviderReleaseWorkspaceSetup } from "../../components/management/provider-app-manager";
import { getReadiness } from "../../lib/readiness";
import { getEngineCapabilities, listPaymentMethods, listProviderAppProviders, listProviderApps, listProviders, type Provider, type ProviderAppProvider, type ProviderAppVersion, type RuntimeConnector } from "../../lib/payment-proxy";
import { requireDashboardSession } from "../../lib/session";

export const dynamic = "force-dynamic";

type ProviderTab = "overview" | "methods" | "releases";

function cleanProviderCode(value: string) {
  const code = value.toLowerCase().trim();
  return /^[a-z0-9_-]{2,48}$/.test(code) ? code : "";
}

function providerHref(code: string, tab: ProviderTab) {
  const query = new URLSearchParams({ tab });
  return `/providers/${encodeURIComponent(code)}?${query}`;
}

function fallbackProvider(code: string): Provider {
  return {
    code,
    name: code.replaceAll("_", " ").replaceAll("-", " ").toUpperCase(),
    description: "Provider detail is temporarily unavailable.",
    available: false,
    connector_code: code,
    credential_schema: [],
    environments: [],
    payment_methods: [],
    created_at: "",
    updated_at: "",
  };
}

export default async function ProviderDetailPage({
  params,
  searchParams,
}: {
  params: Promise<{ code: string }>;
  searchParams: Promise<{ tab?: string; environment?: string; connect?: string }>;
}) {
  const route = await params;
  const query = await searchParams;
  const providerCode = cleanProviderCode(route.code);
  if (!providerCode) notFound();
  if (query.tab === "connection") redirect(`/providers/${encodeURIComponent(providerCode)}?tab=overview`);
  if (query.tab === "certification") redirect(`/providers/${encodeURIComponent(providerCode)}?tab=methods`);
  const tab: ProviderTab = query.tab === "methods" || query.tab === "releases" ? query.tab : "overview";
  const session = await requireDashboardSession(`/providers/${providerCode}?tab=${tab}`);
  const health = await getReadiness();
  const healthy = health.status === "ready";
  const [providersResult, methodsResult, releaseProvidersResult, releaseVersionsResult, runtimeResult] = await Promise.allSettled([
    listProviders(session.subject),
    listPaymentMethods(session.subject),
    listProviderAppProviders(session.subject),
    listProviderApps(session.subject),
    getEngineCapabilities(session.subject),
  ]);
  const providers = providersResult.status === "fulfilled" ? providersResult.value : [];
  const matchedProvider = providers.find((item) => item.code === providerCode);
  if (providersResult.status === "fulfilled" && !matchedProvider) notFound();
  const provider = matchedProvider ?? fallbackProvider(providerCode);
  const methods = methodsResult.status === "fulfilled" ? methodsResult.value : [];
  const capabilities = methods.flatMap((method) => method.providers
    .filter((item) => item.provider_code === providerCode)
    .map((item) => ({ method, provider: item })))
    .sort((left, right) => left.method.sort_order - right.method.sort_order || left.method.name.localeCompare(right.method.name));
  const verified = capabilities.filter(({ provider: item }) => item.support_status === "CERTIFIED").length;
  const documented = capabilities.filter(({ provider: item }) => item.support_status === "DOCUMENTED").length;
  const releaseProvider: ProviderAppProvider | undefined = releaseProvidersResult.status === "fulfilled" ? releaseProvidersResult.value.find((item) => item.provider_code === providerCode) : undefined;
  const releaseVersions: ProviderAppVersion[] = releaseVersionsResult.status === "fulfilled" ? releaseVersionsResult.value.filter((item) => item.provider_code === providerCode) : [];
  const runtimeConnector: RuntimeConnector | undefined = runtimeResult.status === "fulfilled" ? runtimeResult.value.connectors.find((item) => item.code === providerCode) : undefined;
  const releaseError = releaseProvidersResult.status === "rejected" || releaseVersionsResult.status === "rejected" || runtimeResult.status === "rejected";
  const dataError = providersResult.status === "rejected" || methodsResult.status === "rejected" || (tab === "releases" && releaseError);

  return (
    <div className="dashboard-app">
      <AppSidebar active="providers" healthy={healthy} engineStatus={health.status}/>
      <div className="dashboard-main">
        <AppTopbar healthy={healthy} searchPlaceholder={tab === "releases" ? `Search ${provider.name} release versions...` : `Search ${provider.name} capabilities...`}/>
        <main className="dashboard-content management-content provider-detail-content">
          <section className="provider-detail-heading">
            <div className="provider-detail-identity">
              <Link className="provider-back-link" href="/providers"><Icon name="arrow" size={14}/> All providers</Link>
              <div><BrandLogo code={provider.code} label={provider.name} className={`provider-detail-logo ${provider.code}`}/><span><span className={`availability-badge ${provider.available ? "available" : "planned"}`}><i/>{provider.available ? "Available connector" : "Planned connector"}</span><h1>{provider.name}</h1><p>{provider.description}</p></span></div>
            </div>
            <Link className="dashboard-primary-action" href={providerHref(provider.code, "releases")}>Manage releases<Icon name="arrow" size={16}/></Link>
          </section>

          {dataError && <div className="dashboard-alert error"><strong>Provider detail belum lengkap.</strong><span>Sebagian data Payment Proxy tidak dapat dimuat. Tidak ada perubahan state yang dilakukan.</span></div>}

          <nav className="provider-detail-tabs" aria-label={`${provider.name} detail sections`}>
            <Link className={tab === "overview" ? "active" : ""} href={providerHref(provider.code, "overview")}><Icon name="overview" size={15}/><span>Overview</span></Link>
            <Link className={tab === "methods" ? "active" : ""} href={providerHref(provider.code, "methods")}><Icon name="wallet" size={15}/><span>Payment methods</span><b>{capabilities.length}</b></Link>
            <Link className={tab === "releases" ? "active" : ""} href={providerHref(provider.code, "releases")}><Icon name="route" size={15}/><span>Releases</span><b>{releaseVersions.length}</b></Link>
          </nav>

          {tab === "overview" && <>
            <section className="management-metrics provider-detail-metrics">
              <article><span>CAPABILITIES</span><strong>{capabilities.length}</strong><small>canonical payment methods</small></article>
              <article><span>VERIFIED</span><strong>{verified}</strong><small>sandbox evidence complete</small></article>
              <article><span>SHARED RUNTIME</span><strong>{runtimeConnector ? "Running" : "Not loaded"}</strong><small>{runtimeConnector?.version || "no published runtime"}</small></article>
              <article><span>MERCHANT SETUP</span><strong>{provider.credential_schema.length}</strong><small>credential fields requested on install</small></article>
            </section>
            <section className="provider-overview-grid">
              <article className="dashboard-panel provider-overview-card">
                <div className="panel-heading"><div><p className="panel-kicker">CONNECTOR PROFILE</p><h2>Provider integration</h2><p>Provider-specific behavior remains isolated inside the Emisell connector.</p></div><em>{provider.connector_code || provider.code}</em></div>
                <dl><div><dt>Runtime status</dt><dd>{runtimeConnector ? "Running" : "Not loaded"}</dd></div><div><dt>Connector code</dt><dd>{provider.connector_code || provider.code}</dd></div><div><dt>Verified methods</dt><dd>{verified} of {capabilities.length}</dd></div><div><dt>Documented backlog</dt><dd>{documented}</dd></div></dl>
                <div className="provider-credential-fields"><span>MERCHANT INSTALL SCHEMA</span>{provider.credential_schema.map((field) => <article key={field.code}><Icon name={field.secret ? "key" : "settings"} size={15}/><span><strong>{field.label}</strong><small>{field.code} · requested from merchant during installation</small></span><b>{field.required ? "REQUIRED" : "OPTIONAL"}</b></article>)}{provider.credential_schema.length === 0 && <p>No credential schema has been registered.</p>}</div>
              </article>
              <article className="dashboard-panel provider-readiness-card">
                <div className="panel-heading"><div><p className="panel-kicker">RELEASE READINESS</p><h2>Verified capabilities</h2><p>Backend verification determines which methods can be assigned to checkout.</p></div><span>{capabilities.length ? Math.round(verified / capabilities.length * 100) : 0}%</span></div>
                <div className="provider-progress"><i style={{ width: `${capabilities.length ? verified / capabilities.length * 100 : 0}%` }}/></div>
                <div className="provider-readiness-counts"><span><i className="certified"/><strong>{verified}</strong><small>Verified</small></span><span><i className="documented"/><strong>{documented}</strong><small>Documented</small></span><span><i className="disabled"/><strong>{capabilities.length - verified - documented}</strong><small>Disabled</small></span></div>
                <Link className="catalog-action" href={providerHref(provider.code, "methods")}>Open verified capabilities<Icon name="arrow" size={15}/></Link>
              </article>
            </section>
          </>}

          {tab === "releases" && <>
            <section className="provider-app-context-grid">
              <article><small>PROVIDER CATALOG</small><strong className={provider.available ? "is-ready" : "is-pending"}>{provider.available ? "Available" : "Unavailable"}</strong><span>Merchant-facing provider identity.</span></article>
              <article><small>SHARED RUNTIME</small><strong className={runtimeConnector ? "is-ready" : "is-pending"}>{runtimeConnector ? "Running" : "Not loaded"}</strong><span>{runtimeConnector?.version || "No runtime version loaded"}</span></article>
              <article><small>PUBLISHED RELEASE</small><strong className={releaseProvider?.status === "ACTIVE" ? "is-ready" : "is-pending"}>{releaseProvider?.status === "ACTIVE" ? "Published" : "Awaiting publish"}</strong><span>{releaseProvider?.active_version || "No release published"}</span></article>
            </section>
            <section className="provider-app-security"><Icon name="settings" size={18}/><div><strong>One provider, separate lifecycle controls</strong><p>Submission review and runtime publication belong to {provider.name}. Publishing a release never duplicates the provider or silently changes an active merchant connection.</p></div><b>25 MB MAX</b></section>
            {releaseProvider ? <ProviderAppVersionManager provider={releaseProvider} initialApps={releaseVersions}/> : <ProviderReleaseWorkspaceSetup provider={provider}/>}
          </>}

          {tab === "methods" && <section className="dashboard-panel provider-method-panel">
            <div className="panel-heading"><div><p className="panel-kicker">CANONICAL MAPPING</p><h2>{provider.name} payment methods</h2><p>One canonical method maps to one provider channel without leaking provider payload into checkout.</p></div><span>{capabilities.length} methods</span></div>
            <div className="provider-method-head"><span>Payment method</span><span>Provider mapping</span><span>Channel</span><span>Verification status</span></div>
            {capabilities.map(({ method, provider: item }) => <article className="provider-method-row" key={method.code}><span><BrandLogo code={method.code} label={method.name} kind="payment-method" className={`method-category-icon ${method.category.toLowerCase()}`}/><span><strong>{method.name}</strong><small>{method.code} · {method.category.replaceAll("_", " ")}</small></span></span><span><code>{item.provider_method}</code><small>{item.provider_method_type}</small></span><span><strong>{item.provider_channel_code || "—"}</strong><small>{item.metadata?.conformance_profile || "No backend test profile"}</small></span><span><em className={`certification-status ${item.support_status.toLowerCase()}`}>{item.support_status === "CERTIFIED" ? "VERIFIED" : item.support_status}</em>{item.metadata?.blocker_code && <small>{item.metadata.blocker_code}</small>}</span></article>)}
            {capabilities.length === 0 && <div className="management-empty"><span><Icon name="wallet" size={24}/></span><h3>No method mapping</h3><p>This provider has no canonical capability registered yet.</p></div>}
          </section>}

        </main>
      </div>
    </div>
  );
}
