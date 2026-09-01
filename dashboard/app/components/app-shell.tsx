import Link from "next/link";

export type IconName = "overview" | "payment" | "refund" | "provider" | "install" | "route" | "reconcile" | "chart" | "webhook" | "logs" | "docs" | "settings" | "key" | "search" | "bell" | "arrow" | "check" | "activity" | "wallet";

export function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  const common = { width: size, height: size, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round" as const, strokeLinejoin: "round" as const, "aria-hidden": true };
  const paths: Record<IconName, React.ReactNode> = {
    overview: <><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/></>,
    payment: <><rect x="3" y="5" width="18" height="14" rx="3"/><path d="M3 10h18M7 15h3"/></>,
    refund: <><path d="M4 7h11a5 5 0 0 1 0 10H9"/><path d="m8 3-4 4 4 4"/></>,
    provider: <><path d="M8 3v4M16 3v4M5 7h14v4a7 7 0 0 1-14 0V7Z"/><path d="M9 21v-3h6v3"/></>,
    install: <><path d="M12 3v12m0 0 4-4m-4 4-4-4"/><path d="M5 21h14"/></>,
    route: <><circle cx="6" cy="5" r="2"/><circle cx="18" cy="19" r="2"/><path d="M8 5h4a4 4 0 0 1 4 4v8M8 17H6a2 2 0 0 1-2-2v-3"/></>,
    reconcile: <><path d="M20 7h-7a5 5 0 0 0-5 5v1"/><path d="m17 4 3 3-3 3M4 17h7a5 5 0 0 0 5-5v-1"/><path d="m7 20-3-3 3-3"/></>,
    chart: <><path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/></>,
    webhook: <><circle cx="6" cy="12" r="3"/><circle cx="18" cy="6" r="3"/><circle cx="18" cy="18" r="3"/><path d="m8.5 10.5 7-3M8.5 13.5l7 3"/></>,
    logs: <><path d="M6 3h12a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z"/><path d="M8 8h8M8 12h8M8 16h5"/></>,
    docs: <><path d="M6 3h9l4 4v14H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z"/><path d="M14 3v5h5M8 13h8M8 17h6"/></>,
    settings: <><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1-1.6v-.2h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/></>,
    key: <><circle cx="8" cy="15" r="4"/><path d="m11 12 8-8M16 7l3 3M13.5 9.5l2 2"/></>,
    search: <><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></>,
    bell: <><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/></>,
    arrow: <><path d="M5 12h14m-5-5 5 5-5 5"/></>,
    check: <path d="m5 12 4 4L19 6"/>,
    activity: <path d="M3 12h4l2-7 4 14 2-7h6"/>,
    wallet: <><path d="M4 6h14a2 2 0 0 1 2 2v10H4a2 2 0 0 1-2-2V6a3 3 0 0 1 3-3h12"/><path d="M15 11h5v4h-5a2 2 0 0 1 0-4Z"/></>,
  };
  return <svg {...common}>{paths[name]}</svg>;
}

type ActivePage = "overview" | "payments" | "providers" | "payment-methods" | "webhooks" | "api-keys" | "docs";

const navigation: { label: string; items: { name: string; icon: IconName; href?: string; badge?: string; page?: ActivePage }[] }[] = [
  { label: "Workspace", items: [
    { name: "Overview", icon: "overview", href: "/", page: "overview" },
    { name: "Payments", icon: "payment", href: "/payments", page: "payments" },
  ]},
  { label: "Payment setup", items: [
    { name: "Providers", icon: "provider", href: "/providers", page: "providers" },
    { name: "Checkout methods", icon: "wallet", href: "/payment-methods", page: "payment-methods" },
  ]},
  { label: "Operations", items: [
    { name: "Webhooks", icon: "webhook", href: "/webhooks", page: "webhooks" },
  ]},
  { label: "Developers", items: [
    { name: "API keys", icon: "key", href: "/api-keys", page: "api-keys" },
    { name: "API documentation", icon: "docs", href: "/docs", page: "docs" },
  ]},
];

export function AppSidebar({ active, healthy, engineStatus }: { active: ActivePage; healthy: boolean; engineStatus: string }) {
  return (
    <aside className="app-sidebar">
      <div className="sidebar-brand"><span className="brand-mark">E</span><div><strong>Emisell</strong><small>Payment Platform</small></div></div>
      <button className="workspace-switcher" type="button"><span className="workspace-avatar">EM</span><span><strong>Emisell</strong><small>Production workspace</small></span><b>⌄</b></button>
      <nav className="app-navigation" aria-label="Dashboard navigation">
        {navigation.map((group) => <div className="nav-group" key={group.label}>
          <p>{group.label}</p>
          {group.items.map((item) => item.href ? (
            <Link className={`app-nav-item ${item.page === active ? "active" : ""}`} href={item.href} key={item.name}><Icon name={item.icon}/><span>{item.name}</span>{item.badge && <em>{item.badge}</em>}</Link>
          ) : (
            <div className="app-nav-item disabled" aria-disabled="true" key={item.name}><Icon name={item.icon}/><span>{item.name}</span>{item.badge && <em>{item.badge}</em>}</div>
          ))}
        </div>)}
      </nav>
      <div className="sidebar-footer">
        <div className="engine-mini-status"><i className={healthy ? "online" : "offline"}/><span><strong>Emisell engine</strong><small>{healthy ? "Isolated connectors operational" : engineStatus}</small></span></div>
        <div className="profile"><span>SB</span><div><strong>Operator</strong><small>Admin workspace</small></div><form action="/api/auth/logout" method="post"><button type="submit" aria-label="Sign out">↗</button></form></div>
      </div>
    </aside>
  );
}

export function AppTopbar({ healthy, searchPlaceholder = "Search payment, reference, provider..." }: { healthy: boolean; searchPlaceholder?: string }) {
  return (
    <header className="app-topbar">
      <div className="mobile-brand"><span className="brand-mark">E</span><strong>Emisell</strong></div>
      <label className="dashboard-search"><Icon name="search" size={17}/><input aria-label="Search dashboard" placeholder={searchPlaceholder}/><kbd>⌘ K</kbd></label>
      <div className="topbar-actions"><button type="button" aria-label="Notifications"><Icon name="bell"/></button><span className="topbar-separator"/><div className={`live-pill ${healthy ? "is-live" : "is-down"}`}><i/> {healthy ? "Live systems" : "Needs attention"}</div></div>
    </header>
  );
}
