import { NextResponse } from "next/server";

const providerCodePattern = /^[a-z0-9_-]{2,48}$/;
const allowedContentTypes = new Set(["image/png", "image/jpeg"]);

export async function GET(_: Request, context: { params: Promise<{ code: string }> }) {
  const { code } = await context.params;
  if (!providerCodePattern.test(code)) return new NextResponse("Not found", { status: 404 });

  const baseURL = process.env.BACKEND_API_URL?.trim().replace(/\/$/, "");
  const serviceKey = process.env.SERVICE_API_KEY?.trim();
  const merchantID = process.env.DASHBOARD_MERCHANT_ID?.trim();
  if (!baseURL || !serviceKey || !merchantID) return new NextResponse("Unavailable", { status: 503 });

  const response = await fetch(`${baseURL}/api/v1/provider-assets/${encodeURIComponent(code)}/logo`, {
    cache: "no-store",
    headers: { Authorization: `Bearer ${serviceKey}`, "X-Emisell-Merchant-ID": merchantID },
    signal: AbortSignal.timeout(10_000),
  });
  const contentType = response.headers.get("content-type")?.split(";")[0] ?? "";
  if (!response.ok || !allowedContentTypes.has(contentType)) return new NextResponse("Not found", { status: 404 });
  const logo = await response.arrayBuffer();
  if (logo.byteLength === 0 || logo.byteLength > 512 * 1024) return new NextResponse("Not found", { status: 404 });

  return new NextResponse(logo, {
    headers: {
      "Content-Type": contentType,
      "Cache-Control": "private, max-age=300",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
