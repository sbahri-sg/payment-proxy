"use client";

import { useActionState } from "react";
import { paymentOperationAction, type PaymentActionState } from "../../actions/payments";
import type { PaymentSession } from "../../lib/payment-proxy";

export function PaymentActions({ payment }: { payment: PaymentSession }) {
  const initialState: PaymentActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(paymentOperationAction, initialState);
  const cancellable = payment.status === "PENDING" || payment.status === "PROCESSING";

  function confirmOperation(event: React.FormEvent<HTMLFormElement>) {
    const submitter = (event.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null;
    if (submitter?.value === "cancel" && !window.confirm(`Batalkan payment ${payment.merchant_reference}? Tindakan ini akan diteruskan ke payment engine.`)) event.preventDefault();
  }

  return (
    <form action={action} onSubmit={confirmOperation} className="payment-actions">
      <input type="hidden" name="payment_id" value={payment.id}/>
      <input type="hidden" name="merchant_id" value={payment.merchant_id}/>
      <input type="hidden" name="reason" value="requested_by_operator"/>
      <div>
        <button className="dashboard-primary-button" name="operation" value="sync" disabled={pending}>{pending ? "Processing..." : "Sync status"}</button>
        {cancellable && <button className="danger-button" name="operation" value="cancel" disabled={pending}>Cancel payment</button>}
      </div>
      {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
    </form>
  );
}
