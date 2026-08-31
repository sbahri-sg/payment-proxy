"use server";

import { revalidatePath } from "next/cache";
import { cancelPayment, getPayment, PaymentProxyError } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type PaymentActionState = { status: "idle" | "success" | "error"; message: string };

function clean(value: FormDataEntryValue | null, max = 256) {
  return typeof value === "string" ? value.trim().slice(0, max) : "";
}

function actionError(error: unknown): PaymentActionState {
  if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
  return { status: "error", message: "Payment belum dapat diproses. Periksa koneksi engine lalu coba lagi." };
}

export async function paymentOperationAction(_: PaymentActionState, form: FormData): Promise<PaymentActionState> {
  const session = await requireDashboardSession();
  const paymentID = clean(form.get("payment_id"), 128);
  const operation = clean(form.get("operation"), 16);
  if (!/^pay_[A-Za-z0-9]+$/.test(paymentID)) return { status: "error", message: "Payment ID tidak valid." };
  try {
    if (operation === "sync") {
      await getPayment(session.subject, paymentID);
    } else if (operation === "cancel") {
      const reason = clean(form.get("reason"), 256) || "requested_by_operator";
      await cancelPayment(session.subject, paymentID, reason, `dashboard-cancel-${paymentID}-${Date.now()}`);
    } else {
      return { status: "error", message: "Operasi payment tidak didukung." };
    }
    revalidatePath("/");
    revalidatePath("/payments");
    revalidatePath(`/payments/${paymentID}`);
    return { status: "success", message: operation === "sync" ? "Status terbaru berhasil disinkronkan dari payment engine." : "Permintaan pembatalan berhasil diproses." };
  } catch (error) {
    return actionError(error);
  }
}
