"use client";

import { useActionState, useEffect, useRef } from "react";
import { configureCredentialsAction, type InstallationActionState } from "../../actions/installations";
import type { CredentialField, Installation } from "../../lib/payment-proxy";

export function CredentialForm({ installation, schema }: { installation: Installation; schema: CredentialField[] }) {
  const initialState: InstallationActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(configureCredentialsAction, initialState);
  const formRef = useRef<HTMLFormElement>(null);
  useEffect(() => { if (state.status === "success") formRef.current?.reset(); }, [state]);
  const configured = new Set(installation.credential_metadata.configured_fields?.filter((field) => field.configured).map((field) => field.code));
  return (
    <details className="credential-panel" open={installation.status === "CONFIG_REQUIRED" || installation.status === "ERROR"}>
      <summary><span><strong>{configured.size ? "Rotate & verify credential" : "Configure & verify credential"}</strong><small>Credential disimpan terenkripsi di vault Emisell</small></span><b>⌄</b></summary>
      <form action={action} ref={formRef} className="management-form credential-form">
        <input type="hidden" name="installation_id" value={installation.id}/>
        {schema.map((field) => <label key={field.code}><span>{field.label}{field.required && <em>Required</em>}</span><input name={`credential_${field.code}`} type={field.secret ? "password" : "text"} required={field.required} autoComplete="new-password" spellCheck={false} placeholder={configured.has(field.code) ? "Configured · enter a new value to rotate" : `Enter ${field.label.toLowerCase()}`}/><small>{field.secret ? "Dienkripsi dengan AES-GCM dan tidak pernah ditampilkan kembali." : "Disimpan sebagai bagian konfigurasi connector."}</small></label>)}
        {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
        <button className="dashboard-primary-button" type="submit" disabled={pending}>{pending ? "Verifying provider..." : "Save & verify"}<span>→</span></button>
      </form>
    </details>
  );
}
