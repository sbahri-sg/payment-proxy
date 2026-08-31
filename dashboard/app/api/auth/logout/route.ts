import { NextResponse } from "next/server";
import { dashboardSessionCookie } from "../../../lib/session";

export async function POST(request: Request) {
  const origin = request.headers.get("origin");
  const host = request.headers.get("x-forwarded-host") ?? request.headers.get("host") ?? new URL(request.url).host;
  const protocol = request.headers.get("x-forwarded-proto") ?? new URL(request.url).protocol.replace(":", "");
  const publicOrigin = `${protocol}://${host}`;
  if (origin && origin !== publicOrigin) return new NextResponse("Forbidden", { status: 403 });
  const response = NextResponse.redirect(new URL("/login", publicOrigin), 303);
  response.cookies.set(dashboardSessionCookie, "", { httpOnly: true, sameSite: "strict", path: "/", maxAge: 0 });
  return response;
}
