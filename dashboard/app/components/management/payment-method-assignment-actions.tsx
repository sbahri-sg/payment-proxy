"use client";

import { useActionState } from "react";
import { deactivatePaymentMethodAction, type PaymentMethodActionState } from "../../actions/payment-methods";
import type { PaymentMethodAssignment } from "../../lib/payment-proxy";

export function PaymentMethodAssignmentActions({ assignment }: { assignment: PaymentMethodAssignment }) {
  const initialState: PaymentMethodActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(deactivatePaymentMethodAction, initialState);
  if (assignment.status !== "ACTIVE") return <span className="method-inactive-note">Not shown at checkout</span>;
  return (
    <form action={action} className="method-assignment-actions" onSubmit={(event) => { if (!window.confirm(`Nonaktifkan ${assignment.label} untuk checkout baru?`)) event.preventDefault(); }}>
      <input type="hidden" name="assignment_id" value={assignment.id}/><input type="hidden" name="version" value={assignment.version}/>
      <button className="secondary-button" type="submit" disabled={pending}>{pending ? "Processing..." : "Deactivate"}</button>
      {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
    </form>
  );
}
