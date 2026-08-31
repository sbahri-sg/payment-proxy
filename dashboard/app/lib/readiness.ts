import "server-only";

export type Readiness = { status: string; checks?: Record<string, string> };

export async function getReadiness(): Promise<Readiness> {
  const base = process.env.BACKEND_API_URL ?? "http://127.0.0.1:8080";
  try {
    const response = await fetch(`${base}/health/ready`, { cache: "no-store", signal: AbortSignal.timeout(2500) });
    return (await response.json()) as Readiness;
  } catch {
    return { status: "unreachable", checks: { api: "unavailable" } };
  }
}
