import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

export default async function CertificationsRedirect({
  searchParams,
}: {
  searchParams: Promise<{ environment?: string; provider?: string }>;
}) {
  const query = await searchParams;
  const requestedProvider = query.provider?.toLowerCase().trim() ?? "xendit";
  const provider = /^[a-z0-9_-]{2,48}$/.test(requestedProvider) ? requestedProvider : "xendit";
  redirect(`/providers/${encodeURIComponent(provider)}?tab=methods`);
}
