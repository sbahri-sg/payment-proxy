"use server";

import { revalidatePath } from "next/cache";
import {
  generateEmisellWebhookSecret,
  PaymentProxyError,
  testEmisellWebhook,
  updateEmisellWebhookSettings,
  type EmisellWebhookSettings,
  type EmisellWebhookTestResult,
} from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type WebhookSettingsActionState = {
  status: "idle" | "success" | "error";
  message: string;
  settings?: EmisellWebhookSettings;
  secret?: string;
  test?: EmisellWebhookTestResult;
};

function actionError(error: unknown): WebhookSettingsActionState {
  if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
  return { status: "error", message: "Konfigurasi webhook belum dapat diproses. Periksa koneksi API dan coba kembali." };
}

function refreshWebhookPage() {
  revalidatePath("/");
  revalidatePath("/webhooks");
  revalidatePath("/docs");
}

export async function saveEmisellWebhookSettingsAction(_: WebhookSettingsActionState, form: FormData): Promise<WebhookSettingsActionState> {
  const session = await requireDashboardSession();
  const callbackURL = typeof form.get("callback_url") === "string" ? String(form.get("callback_url")).trim().slice(0, 2048) : "";
  const enabled = form.get("enabled") === "on";
  if (callbackURL) {
    try {
      const parsed = new URL(callbackURL);
      if (!['http:', 'https:'].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password || parsed.hash) {
        return { status: "error", message: "Callback URL tidak valid." };
      }
    } catch {
      return { status: "error", message: "Callback URL tidak valid." };
    }
  }
  try {
    const settings = await updateEmisellWebhookSettings(session.subject, callbackURL, enabled);
    refreshWebhookPage();
    return { status: "success", message: enabled ? "Callback URL tersimpan dan pengiriman event aktif." : "Callback URL tersimpan. Pengiriman event masih nonaktif.", settings };
  } catch (error) {
    return actionError(error);
  }
}

export async function generateEmisellWebhookSecretAction(_: WebhookSettingsActionState, _form: FormData): Promise<WebhookSettingsActionState> {
  const session = await requireDashboardSession();
  try {
    const generated = await generateEmisellWebhookSecret(session.subject);
    refreshWebhookPage();
    return {
      status: "success",
      message: "Secret baru dibuat. Salin sekarang ke Emisell Backend; secret ini tidak dapat ditampilkan kembali.",
      settings: generated.settings,
      secret: generated.secret,
    };
  } catch (error) {
    return actionError(error);
  }
}

export async function testEmisellWebhookAction(_: WebhookSettingsActionState, _form: FormData): Promise<WebhookSettingsActionState> {
  const session = await requireDashboardSession();
  try {
    const test = await testEmisellWebhook(session.subject);
    refreshWebhookPage();
    return {
      status: test.success ? "success" : "error",
      message: test.message,
      test,
    };
  } catch (error) {
    return actionError(error);
  }
}
