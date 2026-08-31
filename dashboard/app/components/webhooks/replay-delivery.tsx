"use client";

import { useActionState, useState } from "react";
import { replayWebhookAction, type WebhookActionState } from "../../actions/webhooks";
import styles from "./replay-delivery.module.css";

export function ReplayDelivery({ id, replayCount }: { id: string; replayCount: number }) {
  const initialState: WebhookActionState = { status: "idle", message: "" };
  const [state, action, pending] = useActionState(replayWebhookAction, initialState);
  const [confirming, setConfirming] = useState(false);

  return (
    <div className={styles.control}>
      {!confirming ? (
        <button className="danger-button" type="button" onClick={() => setConfirming(true)}>Replay delivery</button>
      ) : (
        <form action={action} className={styles.form}>
          <input type="hidden" name="delivery_id" value={id}/>
          <input type="hidden" name="replay_count" value={replayCount}/>
          <span className={styles.warning}>Event yang sama dapat diterima kembali. Lanjutkan?</span>
          <div className={styles.actions}>
            <button className="secondary-button" type="button" disabled={pending} onClick={() => setConfirming(false)}>Cancel</button>
            <button className="danger-button" type="submit" disabled={pending}>{pending ? "Scheduling..." : "Confirm replay"}</button>
          </div>
        </form>
      )}
      {state.message && <div className={`form-message ${state.status}`} role="status">{state.message}</div>}
    </div>
  );
}
