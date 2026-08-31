"use client";

import { useMemo, useState } from "react";
import type { PaymentMethodCatalogItem, PaymentMethodCategory } from "../../lib/payment-proxy";
import { BrandLogo } from "../brand-logo";

const categoryLabels: Record<PaymentMethodCategory, string> = {
  QR_CODE: "QR Code",
  CARD: "Cards",
  VIRTUAL_ACCOUNT: "Virtual Accounts",
  E_WALLET: "E-Wallets",
  RETAIL: "Retail Outlets",
  PAYLATER: "PayLater",
  DIRECT_DEBIT: "Direct Debit",
  DIGITAL_BANKING: "Digital Banking",
};

export function PaymentMethodCatalog({ methods }: { methods: PaymentMethodCatalogItem[] }) {
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<PaymentMethodCategory | "ALL">("ALL");
  const categories = useMemo(() => Array.from(new Set(methods.map((item) => item.category))), [methods]);
  const filtered = methods.filter((method) => {
    const needle = query.trim().toLowerCase();
    const matchesQuery = !needle || `${method.name} ${method.code} ${method.description} ${method.providers.map((item) => item.provider_name).join(" ")}`.toLowerCase().includes(needle);
    return matchesQuery && (category === "ALL" || method.category === category);
  });
  return (
    <section className="dashboard-panel master-method-panel">
      <div className="panel-heading"><div><p className="panel-kicker">EMISELL CANONICAL REGISTRY</p><h2>Master payment methods</h2><p>Satu identitas metode dipakai bersama untuk Xendit, Midtrans, Duitku, dan DOKU.</p></div><span>{filtered.length} of {methods.length}</span></div>
      <div className="master-method-toolbar">
        <input aria-label="Search master payment methods" placeholder="Search QRIS, BCA, OVO, gateway..." value={query} onChange={(event) => setQuery(event.target.value)}/>
        <div><button type="button" className={category === "ALL" ? "active" : ""} onClick={() => setCategory("ALL")}>All</button>{categories.map((item) => <button type="button" className={category === item ? "active" : ""} onClick={() => setCategory(item)} key={item}>{categoryLabels[item]}</button>)}</div>
      </div>
      <div className="master-method-grid">
        {filtered.map((method) => <article className="master-method-card" key={method.code}>
          <div className="master-method-head"><BrandLogo code={method.code} label={method.name} kind="payment-method" className={`method-category-icon ${method.category.toLowerCase()}`}/><div><strong>{method.name}</strong><code>{method.code}</code></div><em>{categoryLabels[method.category]}</em></div>
          <p>{method.description}</p>
          <div className="method-provider-list">{method.providers.map((provider) => {
            const engineBlocked = provider.metadata?.engine_support === "UNSUPPORTED";
            return <a href={provider.source_url} target="_blank" rel="noreferrer" className={`method-provider-badge ${engineBlocked ? "blocked" : provider.support_status.toLowerCase()}`} title={`${provider.provider_name}: ${engineBlocked ? provider.metadata.blocker_code : provider.provider_channel_code || "documented"}`} key={provider.provider_code}><BrandLogo code={provider.provider_code} label={provider.provider_name} className="provider-badge-logo"/>{provider.provider_name}<small>{provider.support_status === "CERTIFIED" ? "Certified" : engineBlocked ? "Engine blocked" : "Supported"}</small></a>;
          })}</div>
          <footer><span>{method.currencies.join(" · ")}</span><span>{method.providers.length} gateways</span></footer>
        </article>)}
        {filtered.length === 0 && <div className="management-empty master-method-empty"><h3>No matching payment method</h3><p>Try another method, category, or gateway name.</p></div>}
      </div>
      <div className="catalog-legend"><span><i className="certified"/>Certified connector — dapat dipetakan sekarang</span><span><i className="documented"/>Supported gateway — menunggu connector conformance</span><span><i className="blocked"/>Engine blocked — membutuhkan partner connector</span></div>
    </section>
  );
}
