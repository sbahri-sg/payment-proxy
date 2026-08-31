"use server";

import { revalidatePath } from "next/cache";
import { PaymentProxyError, runConnectorCertification } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type CertificationActionState = { status: "idle" | "success" | "error" | "blocked"; message: string };

function clean(value: FormDataEntryValue | null, max = 128) {
  return typeof value === "string" ? value.trim().slice(0, max) : "";
}

export async function runCertificationAction(_: CertificationActionState, form: FormData): Promise<CertificationActionState> {
  const session = await requireDashboardSession();
  const environment = clean(form.get("environment"), 16).toLowerCase();
  const providerCode = clean(form.get("provider_code"), 48).toLowerCase();
  const installationID = clean(form.get("installation_id"));
  const paymentMethodCode = clean(form.get("payment_method_code"), 64).toLowerCase();
  const paymentID = clean(form.get("payment_id"));
  if (environment !== "sandbox" || !/^[a-z0-9_-]{2,48}$/.test(providerCode) || !/^ins_[A-Za-z0-9]+$/.test(installationID) || !/^[a-z0-9_]{2,64}$/.test(paymentMethodCode) || (paymentID !== "" && !/^pay_[A-Za-z0-9]+$/.test(paymentID))) {
    return { status: "error", message: "Certification hanya dapat dijalankan pada installation sandbox yang aktif." };
  }
  try {
    const run = await runConnectorCertification(session.subject, "sandbox", { installation_id: installationID, payment_method_code: paymentMethodCode, ...(paymentID ? { payment_id: paymentID } : {}) });
    revalidatePath(`/providers/${providerCode}`);
    revalidatePath("/providers");
    revalidatePath("/payment-methods");
    if (run.status === "BLOCKED") return { status: "blocked", message: run.message || "Capability masih diblokir oleh execution engine." };
    if (run.status === "FAILED") return { status: "error", message: run.message || "Sandbox certification gagal." };
    return { status: "success", message: `${run.payment_method_name} lulus sandbox smoke test.` };
  } catch (error) {
    if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
    return { status: "error", message: "Certification run tidak dapat diselesaikan." };
  }
}
