"use client";

import { useActionState } from "react";
import { installationOperationAction, type InstallationActionState } from "../../actions/installations";
import type { Installation } from "../../lib/payment-proxy";

export function InstallationActions({ installation }: { installation: Installation }) {
  const initialState: InstallationActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(installationOperationAction, initialState);
  const canActivate = installation.status === "READY" || installation.status === "INACTIVE";
  const canDeactivate = installation.status === "ACTIVE";
  const canUninstall = !["ACTIVE", "VERIFYING", "UNINSTALLED"].includes(installation.status);
  function confirmDanger(event: React.FormEvent<HTMLFormElement>) {
    const submitter = (event.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null;
    if (submitter?.value === "uninstall" && !window.confirm(`Uninstall ${installation.provider_name} ${installation.environment}? Credential terenkripsi connector akan dihapus.`)) event.preventDefault();
    if (submitter?.value === "activate" && installation.environment === "live" && !window.confirm(`Activate ${installation.provider_name} LIVE? Pembayaran nyata dapat diproses melalui connector ini.`)) event.preventDefault();
  }
  if (installation.status === "UNINSTALLED") return <div className="installation-closed">Installation sudah di-uninstall.</div>;
  return (
    <form action={action} onSubmit={confirmDanger} className="installation-actions">
      <input type="hidden" name="installation_id" value={installation.id}/><input type="hidden" name="version" value={installation.version}/>
      <div>
        {canActivate && <button className="dashboard-primary-button" name="operation" value="activate" disabled={pending}>{pending ? "Processing..." : "Activate"}</button>}
        {canDeactivate && <button className="secondary-button" name="operation" value="deactivate" disabled={pending}>{pending ? "Processing..." : "Deactivate"}</button>}
        {canUninstall && <button className="danger-button" name="operation" value="uninstall" disabled={pending}>Uninstall</button>}
      </div>
      {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
    </form>
  );
}
