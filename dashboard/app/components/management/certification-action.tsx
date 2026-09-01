"use client";

import { useActionState } from "react";
import { runCertificationAction, type CertificationActionState } from "../../actions/certifications";

export function CertificationAction({ providerCode, installationID, paymentMethodCode, blocked, paymentID, mobileApproval = false }: { providerCode: string; installationID: string; paymentMethodCode: string; blocked: boolean; paymentID?: string; mobileApproval?: boolean }) {
  const initialState: CertificationActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(runCertificationAction, initialState);
  return (
    <form action={action} className="certification-action">
      <input type="hidden" name="environment" value="sandbox"/>
      <input type="hidden" name="provider_code" value={providerCode}/>
      <input type="hidden" name="installation_id" value={installationID}/>
      <input type="hidden" name="payment_method_code" value={paymentMethodCode}/>
      {paymentID && <input type="hidden" name="payment_id" value={paymentID}/>}
      <button className={blocked || paymentID ? "secondary-button" : "dashboard-primary-button"} type="submit" disabled={pending}>{pending ? "Checking..." : paymentID && mobileApproval ? "Check OVO approval" : paymentID ? "Verify completed payment" : blocked ? "Verify blocker" : "Run connector test"}</button>
      {paymentID && mobileApproval && <small className="certification-action-help">Approve this payment in the OVO test app first. Xendit did not return a browser simulator URL.</small>}
      {state.message && <div className={`form-message ${state.status === "blocked" ? "error" : state.status}`} role="status">{state.message}</div>}
    </form>
  );
}
