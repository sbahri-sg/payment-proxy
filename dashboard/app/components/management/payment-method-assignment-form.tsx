"use client";

import { useActionState, useMemo, useState } from "react";
import { assignPaymentMethodAction, type PaymentMethodActionState } from "../../actions/payment-methods";
import type { Installation, PaymentMethodAssignment, PaymentMethodCatalogItem, PaymentMethodCategory } from "../../lib/payment-proxy";

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

export function PaymentMethodAssignmentForm({ environment, installations, assignments, catalog }: { environment: "sandbox" | "live"; installations: Installation[]; assignments: PaymentMethodAssignment[]; catalog: PaymentMethodCatalogItem[] }) {
  const initialState: PaymentMethodActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(assignPaymentMethodAction, initialState);
  const defaultMethod = catalog.find((item) => item.code === "qris")?.code ?? catalog[0]?.code ?? "";
  const [selectedMethod, setSelectedMethod] = useState(defaultMethod);
  const definition = catalog.find((item) => item.code === selectedMethod) ?? catalog[0];
  const existing = assignments.find((item) => item.payment_method_code === definition?.code);
  const certifiedProviders = useMemo(() => new Set(definition?.providers.filter((item) => item.support_status === "CERTIFIED").map((item) => item.provider_code) ?? []), [definition]);
  const eligibleInstallations = installations.filter((item) => certifiedProviders.has(item.provider_code));
  const selectedInstallation = existing && eligibleInstallations.some((item) => item.id === existing.installation_id) ? existing.installation_id : eligibleInstallations[0]?.id;
  const grouped = useMemo(() => catalog.reduce((result, method) => {
    (result[method.category] ??= []).push(method);
    return result;
  }, {} as Partial<Record<PaymentMethodCategory, PaymentMethodCatalogItem[]>>), [catalog]);
  return (
    <form action={action} className="method-assignment-form">
      <input type="hidden" name="environment" value={environment}/>
      <input type="hidden" name="version" value={existing?.version ?? 0}/>
      <label><span>Master payment method</span><select name="payment_method_code" value={selectedMethod} onChange={(event) => setSelectedMethod(event.target.value)}>{Object.entries(grouped).map(([category, methods]) => <optgroup label={categoryLabels[category as PaymentMethodCategory]} key={category}>{methods?.map((method) => <option value={method.code} key={method.code}>{method.name}</option>)}</optgroup>)}</select><small>Canonical code: <code>{definition?.code}</code></small></label>
      <label><span>Certified active gateway</span><select name="installation_id" key={`${definition?.code}:${selectedInstallation}`} defaultValue={selectedInstallation} required disabled={eligibleInstallations.length === 0}>{eligibleInstallations.map((installation) => <option value={installation.id} key={installation.id}>{installation.provider_name} · {installation.environment}</option>)}</select><small>{eligibleInstallations.length ? `${eligibleInstallations.length} connector installation eligible` : "Belum ada connector aktif yang certified untuk metode ini."}</small></label>
      <label><span>Checkout label</span><input name="label" key={`${definition?.code}:${existing?.version ?? 0}`} defaultValue={existing?.label ?? definition?.name ?? ""} maxLength={96} required/></label>
      <div className="method-assignment-submit">
        <button className="dashboard-primary-button" type="submit" disabled={pending || eligibleInstallations.length === 0 || !definition}>{pending ? "Saving..." : existing ? "Update assignment" : "Assign gateway"}<span>→</span></button>
        {definition && <small className="method-certification-help">{definition.providers.length} gateway support this method · {definition.providers.filter((item) => item.support_status === "CERTIFIED").length} certified in Emisell</small>}
        {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
      </div>
    </form>
  );
}
