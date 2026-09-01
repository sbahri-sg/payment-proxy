"use server";

import { revalidatePath } from "next/cache";
import { createProviderAppProvider, PaymentProxyError, transitionProviderApp, transitionProviderAppProvider, uploadProviderApp, type ProviderAppProvider, type ProviderAppVersion } from "../lib/payment-proxy";
import { requireDashboardSession } from "../lib/session";

export type ProviderAppActionState = {
  status: "idle" | "success" | "error";
  message: string;
  providerApp?: ProviderAppVersion;
  provider?: ProviderAppProvider;
};

function actionError(error: unknown): ProviderAppActionState {
  if (error instanceof PaymentProxyError) return { status: "error", message: `${error.code}: ${error.message}` };
  return { status: "error", message: "Provider App tidak dapat diproses. Periksa bundle lalu coba kembali." };
}

export async function uploadProviderAppAction(_: ProviderAppActionState, form: FormData): Promise<ProviderAppActionState> {
  const session = await requireDashboardSession();
  const providerCode = String(form.get("provider_code") ?? "").trim().toLowerCase();
  const bundle = form.get("bundle");
  if (!/^[a-z0-9_-]{2,48}$/.test(providerCode)) return { status: "error", message: "Provider belum dipilih atau tidak valid." };
  if (!(bundle instanceof File) || bundle.size === 0 || bundle.size > 25 * 1024 * 1024 || !bundle.name.toLowerCase().endsWith(".zip")) {
    return { status: "error", message: "Pilih bundle .zip dengan ukuran maksimum 25 MB." };
  }
  try {
    const providerApp = await uploadProviderApp(session.subject, providerCode, bundle);
    revalidatePath("/provider-apps");
    revalidatePath(`/provider-apps/${providerCode}`);
    revalidatePath(`/providers/${providerCode}`);
    return { status: "success", message: `${providerApp.provider_name} ${providerApp.version} berhasil di-upload dan lolos pemeriksaan awal.`, providerApp };
  } catch (error) {
    return actionError(error);
  }
}

export async function createProviderAppProviderAction(_: ProviderAppActionState, form: FormData): Promise<ProviderAppActionState> {
  const session = await requireDashboardSession();
  const providerCode = String(form.get("provider_code") ?? "").trim().toLowerCase();
  const providerName = String(form.get("provider_name") ?? "").trim();
  const description = String(form.get("description") ?? "").trim();
  const websiteURL = String(form.get("website_url") ?? "").trim();
  const documentationURL = String(form.get("documentation_url") ?? "").trim();
  const supportEmail = String(form.get("support_email") ?? "").trim().toLowerCase();
  const logoEntry = form.get("logo");
  const logo = logoEntry instanceof File && logoEntry.size > 0 ? logoEntry : undefined;
  if (!/^[a-z0-9_-]{2,48}$/.test(providerCode) || providerName.length < 2 || providerName.length > 120 || description.length > 500) {
    return { status: "error", message: "Kode, nama, atau deskripsi provider tidak valid." };
  }
  if (logo && (logo.size > 512 * 1024 || !["image/png", "image/jpeg"].includes(logo.type))) {
    return { status: "error", message: "Logo harus berupa PNG/JPG dengan ukuran maksimum 512 KB." };
  }
  try {
    const provider = await createProviderAppProvider(session.subject, {
      provider_code: providerCode,
      provider_name: providerName,
      description,
      website_url: websiteURL,
      documentation_url: documentationURL,
      support_email: supportEmail,
    }, logo);
    revalidatePath("/provider-apps");
    revalidatePath("/providers");
    revalidatePath(`/providers/${providerCode}`);
    return { status: "success", message: `${provider.provider_name} berhasil dibuat. Upload versi connector dari detail provider.`, provider };
  } catch (error) {
    return actionError(error);
  }
}

export async function transitionProviderAppProviderAction(_: ProviderAppActionState, form: FormData): Promise<ProviderAppActionState> {
  const session = await requireDashboardSession();
  const providerCode = String(form.get("provider_code") ?? "").trim().toLowerCase();
  const expectedStatus = String(form.get("expected_status") ?? "").trim().toUpperCase() as ProviderAppProvider["status"];
  const status = String(form.get("status") ?? "").trim().toUpperCase() as ProviderAppProvider["status"];
  const validStatuses: ProviderAppProvider["status"][] = ["DRAFT", "ACTIVE", "DISABLED"];
  if (!/^[a-z0-9_-]{2,48}$/.test(providerCode) || !validStatuses.includes(expectedStatus) || !validStatuses.includes(status)) {
    return { status: "error", message: "Status provider tidak valid." };
  }
  try {
    const provider = await transitionProviderAppProvider(session.subject, providerCode, expectedStatus, status);
    revalidatePath("/providers");
    revalidatePath(`/providers/${providerCode}`);
    return {
      status: "success",
      message: provider.status === "DISABLED"
        ? `${provider.provider_name} dinonaktifkan untuk installation baru.`
        : provider.status === "ACTIVE"
          ? `${provider.provider_name} kembali tersedia untuk installation baru.`
          : `${provider.provider_name} kembali aktif sebagai draft. Publish release agar tersedia untuk merchant.`,
      provider,
    };
  } catch (error) {
    return actionError(error);
  }
}

export async function transitionProviderAppAction(_: ProviderAppActionState, form: FormData): Promise<ProviderAppActionState> {
  const session = await requireDashboardSession();
  const id = String(form.get("id") ?? "").trim();
  const expectedStatus = String(form.get("expected_status") ?? "").trim().toUpperCase() as ProviderAppVersion["status"];
  const status = String(form.get("status") ?? "").trim().toUpperCase() as ProviderAppVersion["status"];
  if (!/^papp_[A-Za-z0-9]+$/.test(id)) return { status: "error", message: "Provider App ID tidak valid." };
  try {
    const providerApp = await transitionProviderApp(session.subject, id, expectedStatus, status);
    revalidatePath("/provider-apps");
    revalidatePath(`/provider-apps/${providerApp.provider_code}`);
    revalidatePath("/providers");
    revalidatePath(`/providers/${providerApp.provider_code}`);
    const displayStatus = providerApp.status === "CERTIFIED" ? "VERIFIED" : providerApp.status;
    return { status: "success", message: `${providerApp.provider_name} ${providerApp.version} sekarang berstatus ${displayStatus}.`, providerApp };
  } catch (error) {
    return actionError(error);
  }
}
