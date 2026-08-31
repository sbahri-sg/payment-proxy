import { redirect } from "next/navigation";
import { getDashboardSession } from "../lib/session";

export const dynamic = "force-dynamic";

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ error?: string; next?: string }> }) {
  if (await getDashboardSession()) redirect("/");
  const query = await searchParams;
  const next = query.next?.startsWith("/") && !query.next.startsWith("//") ? query.next : "/";
  return (
    <main className="login-page">
      <section className="login-panel">
        <div className="login-brand"><span className="brand-mark">E</span><div><strong>Emisell</strong><small>Payment Platform</small></div></div>
        <div className="login-copy"><p>OPERATOR ACCESS</p><h1>Welcome back</h1><span>Sign in to manage payment provider connections and live installation state.</span></div>
        <form action="/api/auth/login" method="post" className="login-form">
          <input type="hidden" name="next" value={next}/>
          <label><span>Admin password</span><input name="password" type="password" autoComplete="current-password" required autoFocus placeholder="Enter your admin password"/></label>
          {query.error && <div className="form-message error" role="alert">Password tidak valid. Coba kembali.</div>}
          <button type="submit">Sign in to dashboard <span>→</span></button>
        </form>
        <div className="login-security"><i/>Protected operator session · 8 hour expiry</div>
      </section>
      <aside className="login-visual"><div className="login-orbit"><span>Emisell</span><i/><i/><i/></div><div><p>PAYMENT ORCHESTRATION</p><h2>One control plane.<br/>Every provider.</h2><span>Emisell-owned payment lifecycle with isolated, universal provider connectors.</span></div></aside>
    </main>
  );
}
