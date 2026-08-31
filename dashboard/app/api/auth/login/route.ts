import { NextResponse } from "next/server";
import { createDashboardSession, dashboardSessionCookie, validDashboardPassword } from "../../../lib/session";

function safeNext(value: FormDataEntryValue | null) {
  const path = typeof value === "string" ? value : "/";
  return path.startsWith("/") && !path.startsWith("//") ? path : "/";
}

function publicURL(request: Request, path: string) {
  const headers = request.headers;
  const host = headers.get("x-forwarded-host") ?? headers.get("host") ?? new URL(request.url).host;
  const protocol = headers.get("x-forwarded-proto") ?? new URL(request.url).protocol.replace(":", "");
  return new URL(path, `${protocol}://${host}`);
}

export async function POST(request: Request) {
  const form = await request.formData();
  const next = safeNext(form.get("next"));
  const password = String(form.get("password") ?? "");
  if (!validDashboardPassword(password)) {
    return NextResponse.redirect(publicURL(request, `/login?error=1&next=${encodeURIComponent(next)}`), 303);
  }
  const response = NextResponse.redirect(publicURL(request, next), 303);
  response.cookies.set(dashboardSessionCookie, createDashboardSession(), {
    httpOnly: true,
    sameSite: "strict",
    secure: (request.headers.get("x-forwarded-proto") ?? new URL(request.url).protocol.replace(":", "")) === "https",
    path: "/",
    maxAge: 8 * 60 * 60,
  });
  return response;
}
