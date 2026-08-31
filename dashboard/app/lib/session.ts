import "server-only";

import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

export const dashboardSessionCookie = "emisell_dashboard_session";

export type DashboardSession = {
  subject: string;
  role: "admin";
  expiresAt: number;
};

function sessionSecret() {
  const value = process.env.DASHBOARD_SESSION_SECRET?.trim();
  if (!value) throw new Error("DASHBOARD_SESSION_SECRET is required");
  return value;
}

function digest(value: string) {
  return createHash("sha256").update(value).digest();
}

export function validDashboardPassword(value: string) {
  const expected = process.env.DASHBOARD_ADMIN_PASSWORD ?? "";
  if (!expected || !value) return false;
  return timingSafeEqual(digest(value), digest(expected));
}

export function createDashboardSession(): string {
  const session: DashboardSession = {
    subject: "operator",
    role: "admin",
    expiresAt: Date.now() + 8 * 60 * 60 * 1000,
  };
  const payload = Buffer.from(JSON.stringify(session)).toString("base64url");
  const signature = createHmac("sha256", sessionSecret()).update(payload).digest("base64url");
  return `${payload}.${signature}`;
}

function decodeDashboardSession(token: string | undefined): DashboardSession | null {
  if (!token) return null;
  const [payload, signature, extra] = token.split(".");
  if (!payload || !signature || extra) return null;
  const expected = createHmac("sha256", sessionSecret()).update(payload).digest("base64url");
  const givenBuffer = Buffer.from(signature);
  const expectedBuffer = Buffer.from(expected);
  if (givenBuffer.length !== expectedBuffer.length || !timingSafeEqual(givenBuffer, expectedBuffer)) return null;
  try {
    const parsed = JSON.parse(Buffer.from(payload, "base64url").toString("utf8")) as DashboardSession;
    if (parsed.role !== "admin" || parsed.subject !== "operator" || parsed.expiresAt <= Date.now()) return null;
    return parsed;
  } catch {
    return null;
  }
}

export async function getDashboardSession() {
  return decodeDashboardSession((await cookies()).get(dashboardSessionCookie)?.value);
}

export async function requireDashboardSession(next = "/") {
  const session = await getDashboardSession();
  if (!session) redirect(`/login?next=${encodeURIComponent(next)}`);
  return session;
}
