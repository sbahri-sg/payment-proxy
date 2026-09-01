"use server";

import { revalidatePath } from "next/cache";
import { configureInstallation, createInstallation, PaymentProxyError, transitionInstallation, uninstallInstallation } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type InstallationActionState = { status: "idle" | "success" | "error"; message: string };

function clean(value: FormDataEntryValue | null, max = 256) {
  return typeof value === "string" ? value.trim().slice(0, max) : "";
}

function actionError(error: unknown): InstallationActionState {
  if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
  return { status: "error", message: "Operasi tidak dapat diselesaikan. Periksa konfigurasi dashboard dan coba kembali." };
}

function refreshManagementPages(providerCode?: string) {
  revalidatePath("/");
  revalidatePath("/providers");
  if (providerCode && /^[a-z0-9_-]{2,48}$/.test(providerCode)) revalidatePath(`/providers/${providerCode}`);
}

export async function installProviderAction(_: InstallationActionState, form: FormData): Promise<InstallationActionState> {
  const session = await requireDashboardSession();
  const providerCode = clean(form.get("provider_code"), 48).toLowerCase();
  const environment = clean(form.get("environment"), 16).toLowerCase();
  if (!/^[a-z0-9_-]{2,48}$/.test(providerCode) || !["sandbox", "live"].includes(environment)) {
    return { status: "error", message: "Provider dan environment harus dipilih." };
  }
  try {
    await createInstallation(session.subject, {
      provider_code: providerCode,
      environment,
    });
    refreshManagementPages(providerCode);
    return { status: "success", message: `Connection ${providerCode.toUpperCase()} ${environment} berhasil dibuat. Lanjutkan dengan konfigurasi credential.` };
  } catch (error) {
    return actionError(error);
  }
}

export async function configureCredentialsAction(_: InstallationActionState, form: FormData): Promise<InstallationActionState> {
  const session = await requireDashboardSession();
  const installationID = clean(form.get("installation_id"), 128);
  if (!/^ins_[A-Za-z0-9]+$/.test(installationID)) return { status: "error", message: "Installation ID tidak valid." };
  const credentials: Record<string, string> = {};
  for (const [key, value] of form.entries()) {
    if (!key.startsWith("credential_") || typeof value !== "string") continue;
    const code = key.slice("credential_".length);
    if (/^[a-z0-9_]{2,64}$/.test(code)) credentials[code] = value.trim();
  }
  if (!Object.values(credentials).some(Boolean)) return { status: "error", message: "Credential wajib diisi." };
  try {
    const installation = await configureInstallation(session.subject, installationID, credentials);
    for (const key of Object.keys(credentials)) credentials[key] = "";
    refreshManagementPages(installation.provider_code);
    return { status: "success", message: "Credential diverifikasi, disimpan terenkripsi, dan installation berstatus READY." };
  } catch (error) {
    for (const key of Object.keys(credentials)) credentials[key] = "";
    return actionError(error);
  }
}

export async function installationOperationAction(_: InstallationActionState, form: FormData): Promise<InstallationActionState> {
  const session = await requireDashboardSession();
  const installationID = clean(form.get("installation_id"), 128);
  const operation = clean(form.get("operation"), 16);
  const version = Number(clean(form.get("version"), 20));
  if (!/^ins_[A-Za-z0-9]+$/.test(installationID) || !Number.isSafeInteger(version) || version < 1) {
    return { status: "error", message: "Installation atau version tidak valid." };
  }
  try {
    if (operation === "activate" || operation === "deactivate") {
      const installation = await transitionInstallation(session.subject, installationID, operation, version);
      refreshManagementPages(installation.provider_code);
    } else if (operation === "uninstall") {
      const installation = await uninstallInstallation(session.subject, installationID);
      refreshManagementPages(installation.provider_code);
    } else {
      return { status: "error", message: "Operasi tidak didukung." };
    }
    const message = operation === "activate" ? "Installation berhasil diaktifkan." : operation === "deactivate" ? "Installation berhasil dinonaktifkan." : "Installation berhasil di-uninstall dan credential connector dihapus.";
    return { status: "success", message };
  } catch (error) {
    return actionError(error);
  }
}
