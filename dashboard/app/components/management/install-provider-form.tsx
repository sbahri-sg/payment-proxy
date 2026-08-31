"use client";

import { useActionState, useEffect, useRef } from "react";
import { installProviderAction, type InstallationActionState } from "../../actions/installations";
import type { Provider } from "../../lib/payment-proxy";

export function InstallProviderForm({ providers, selectedProvider, merchantID }: { providers: Provider[]; selectedProvider?: string; merchantID: string }) {
  const initialState: InstallationActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(installProviderAction, initialState);
  const formRef = useRef<HTMLFormElement>(null);
  useEffect(() => { if (state.status === "success") formRef.current?.reset(); }, [state]);
  function confirmLive(event: React.FormEvent<HTMLFormElement>) {
    const form = new FormData(event.currentTarget);
    if (form.get("environment") === "live" && !window.confirm("Install provider untuk environment LIVE? Credential dan transaksi pada installation ini akan memproses pembayaran nyata.")) event.preventDefault();
  }
  return (
    <form action={action} ref={formRef} className="management-form" onSubmit={confirmLive}>
      <div className="form-grid">
        <label><span>Provider</span><select name="provider_code" defaultValue={selectedProvider ?? providers[0]?.code} required>{providers.map((provider) => <option value={provider.code} key={provider.code}>{provider.name}</option>)}</select></label>
        <fieldset><legend>Environment</legend><label className="radio-card"><input type="radio" name="environment" value="sandbox" defaultChecked/><span><strong>Sandbox</strong><small>Test credential and transaction</small></span></label><label className="radio-card live"><input type="radio" name="environment" value="live"/><span><strong>Live</strong><small>Real payment processing</small></span></label></fieldset>
      </div>
      <div className="merchant-scope-note"><span>Merchant ID</span><code>{merchantID}</code><small>Installation otomatis terikat ke Merchant ID dari tenant header.</small></div>
      {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
      <button className="dashboard-primary-button" type="submit" disabled={pending}>{pending ? "Installing..." : "Install provider"}<span>→</span></button>
    </form>
  );
}
