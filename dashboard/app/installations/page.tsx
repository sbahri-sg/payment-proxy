import { redirect } from "next/navigation";

export const dynamic = "force-dynamic";

function cleanProviderCode(value: string | undefined) {
  const code = value?.toLowerCase().trim() ?? "";
  return /^[a-z0-9_-]{2,48}$/.test(code) ? code : "";
}

// Compatibility route for old admin bookmarks. Merchant installation and
// credential lifecycle belongs to the Emisell merchant dashboard, not here.
export default async function InstallationsPage({
  searchParams,
}: {
  searchParams: Promise<{ provider?: string }>;
}) {
  const query = await searchParams;
  const providerCode = cleanProviderCode(query.provider);
  if (providerCode) redirect(`/providers/${encodeURIComponent(providerCode)}`);
  redirect("/providers");
}
