import type { Metadata } from "next";
import Link from "next/link";
import { Icon } from "../../components/app-shell";
import styles from "./return.module.css";

export const metadata: Metadata = {
  title: "Payment action received · Emisell",
  description: "Return page for an internal Emisell Payment Proxy sandbox verification.",
};

export default function CertificationReturnPage() {
  return (
    <div className={styles.page}>
      <main className={styles.card}>
        <header className={styles.brand}>
          <span>E</span>
          <div><strong>Emisell</strong><small>Payment Platform</small></div>
        </header>
        <section className={styles.content}>
          <div className={styles.success}><Icon name="check" size={27}/></div>
          <p className={styles.eyebrow}>PROVIDER ACTION COMPLETE</p>
          <h1>Payment action received</h1>
          <p className={styles.lead}>The customer action is complete. The final payment result is confirmed asynchronously from the signed provider webhook—not from this browser redirect.</p>
          <div className={styles.flow}>
            <article><span>1</span><div><strong>Provider action</strong><small>Customer authorization completed</small></div></article>
            <article><span>2</span><div><strong>Webhook verification</strong><small>Payment Proxy validates the provider event</small></div></article>
            <article><span>3</span><div><strong>Emisell delivery</strong><small>Canonical event is delivered durably</small></div></article>
          </div>
          <Link className={styles.action} href="/providers">Return to provider registry <Icon name="arrow" size={16}/></Link>
          <p className={styles.note}>This page is only a return target for internal sandbox checks. Merchant setup does not require a manual certification step.</p>
        </section>
      </main>
    </div>
  );
}
