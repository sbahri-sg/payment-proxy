"use server";

import { revalidatePath } from "next/cache";
import {
  generateServiceAPIKey,
  PaymentProxyError,
  revokeServiceAPIKey,
  type ServiceAPIKey,
} from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type APIKeyActionState = {
  status: "idle" | "success" | "error";
  message: string;
  apiKey?: ServiceAPIKey;
  secret?: string;
};

function actionError(error: unknown): APIKeyActionState {
  if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
  return { status: "error", message: "API key belum dapat diproses. Periksa koneksi Payment Proxy dan coba kembali." };
}

function refresh() {
  revalidatePath("/api-keys");
  revalidatePath("/docs");
}

export async function generateServiceAPIKeyAction(_: APIKeyActionState, form: FormData): Promise<APIKeyActionState> {
  const session = await requireDashboardSession();
  const name = typeof form.get("name") === "string" ? String(form.get("name")).trim().replace(/\s+/g, " ").slice(0, 80) : "";
  if (name.length < 3) return { status: "error", message: "Nama API key minimal 3 karakter." };
  try {
    const generated = await generateServiceAPIKey(session.subject, name);
    refresh();
    return {
      status: "success",
      message: "API key aktif. Salin secret sekarang karena nilainya tidak dapat ditampilkan kembali.",
      apiKey: generated.api_key,
      secret: generated.secret,
    };
  } catch (error) {
    return actionError(error);
  }
}

export async function revokeServiceAPIKeyAction(_: APIKeyActionState, form: FormData): Promise<APIKeyActionState> {
  const session = await requireDashboardSession();
  const id = typeof form.get("id") === "string" ? String(form.get("id")).trim() : "";
  if (!id.startsWith("sak_")) return { status: "error", message: "API key tidak valid." };
  try {
    const apiKey = await revokeServiceAPIKey(session.subject, id);
    refresh();
    return { status: "success", message: `${apiKey.name} sudah dicabut dan tidak dapat digunakan lagi.`, apiKey };
  } catch (error) {
    return actionError(error);
  }
}
