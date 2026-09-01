import { redirect } from "next/navigation";

// Compatibility route. Release management is now part of each Provider.
export default function ProviderAppsPage() {
  redirect("/providers");
}
