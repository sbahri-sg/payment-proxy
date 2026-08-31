"use server";

import { revalidatePath } from "next/cache";
import { deactivatePaymentMethodAssignment, PaymentProxyError, upsertPaymentMethodAssignment } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type PaymentMethodActionState = { status: "idle" | "success" | "error"; message: string };

function clean(value: FormDataEntryValue | null, max = 128) {
  return typeof value === "string" ? value.trim().slice(0, max) : "";
}

function actionError(error: unknown): PaymentMethodActionState {
  if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
  return { status: "error", message: "Payment method assignment tidak dapat disimpan. Coba kembali." };
}

function refresh() {
  revalidatePath("/");
  revalidatePath("/payment-methods");
  revalidatePath("/installations");
}

export async function assignPaymentMethodAction(_: PaymentMethodActionState, form: FormData): Promise<PaymentMethodActionState> {
  const session = await requireDashboardSession();
  const environment = clean(form.get("environment"), 16).toLowerCase();
  const installationID = clean(form.get("installation_id"));
  const paymentMethodCode = clean(form.get("payment_method_code"), 64).toLowerCase();
  const label = clean(form.get("label"), 96);
  const version = Number(clean(form.get("version"), 20) || "0");
  if (!["sandbox", "live"].includes(environment) || !/^ins_[A-Za-z0-9]+$/.test(installationID) || !/^[a-z0-9_]{2,64}$/.test(paymentMethodCode) || !Number.isSafeInteger(version) || version < 0) {
    return { status: "error", message: "Environment, metode pembayaran, dan installation harus valid." };
  }
  try {
    const item = await upsertPaymentMethodAssignment(session.subject, environment as "sandbox" | "live", {
      installation_id: installationID,
      payment_method_code: paymentMethodCode,
      label,
      version,
    });
    refresh();
    return { status: "success", message: `${item.label} sekarang diproses melalui ${item.provider_name}.` };
  } catch (error) {
    return actionError(error);
  }
}

export async function deactivatePaymentMethodAction(_: PaymentMethodActionState, form: FormData): Promise<PaymentMethodActionState> {
  const session = await requireDashboardSession();
  const id = clean(form.get("assignment_id"));
  const version = Number(clean(form.get("version"), 20));
  if (!/^pmo_[A-Za-z0-9]+$/.test(id) || !Number.isSafeInteger(version) || version < 1) {
    return { status: "error", message: "Assignment atau version tidak valid." };
  }
  try {
    const item = await deactivatePaymentMethodAssignment(session.subject, id, version);
    refresh();
    return { status: "success", message: `${item.label} dinonaktifkan untuk checkout baru.` };
  } catch (error) {
    return actionError(error);
  }
}
