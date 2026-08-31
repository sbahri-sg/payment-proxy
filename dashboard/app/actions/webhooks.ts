"use server";

import { revalidatePath } from "next/cache";
import { PaymentProxyError, replayWebhookDelivery } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type WebhookActionState = { status: "idle" | "success" | "error"; message: string };

function clean(value: FormDataEntryValue | null, max = 256) {
  return typeof value === "string" ? value.trim().slice(0, max) : "";
}

export async function replayWebhookAction(_: WebhookActionState, form: FormData): Promise<WebhookActionState> {
  const session = await requireDashboardSession();
  const deliveryID = clean(form.get("delivery_id"), 128);
  const replayCount = Number(clean(form.get("replay_count"), 12));
  if (!/^evt_[A-Za-z0-9]+$/.test(deliveryID) || !Number.isSafeInteger(replayCount) || replayCount < 0) {
    return { status: "error", message: "Delivery ID atau replay version tidak valid." };
  }
  try {
    await replayWebhookDelivery(session.subject, deliveryID, replayCount, `dashboard-replay-${deliveryID}-${replayCount}`);
    revalidatePath("/");
    revalidatePath("/webhooks");
    return { status: "success", message: "Delivery dijadwalkan ulang. Worker akan mengirimnya dengan signature baru." };
  } catch (error) {
    if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
    return { status: "error", message: "Replay belum dapat dijadwalkan. Periksa status delivery dan koneksi API." };
  }
}
