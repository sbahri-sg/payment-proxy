import { notFound, redirect } from "next/navigation";

function cleanProviderCode(value: string) {
  const code = value.toLowerCase().trim();
  return /^[a-z0-9_-]{2,48}$/.test(code) ? code : "";
}

// Compatibility route for old release-registry bookmarks.
export default async function ProviderAppDetailPage({ params }: { params: Promise<{ code: string }> }) {
  const route = await params;
  const providerCode = cleanProviderCode(route.code);
  if (!providerCode) notFound();
  redirect(`/providers/${encodeURIComponent(providerCode)}?tab=releases`);
}
