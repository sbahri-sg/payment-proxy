import { Icon } from "../app-shell";
import type { Installation } from "../../lib/payment-proxy";

type MonitorProfile = {
  delivery: string;
  security: string;
};

function monitorProfile(providerCode: string): MonitorProfile {
  if (providerCode === "xendit") return {
    delivery: "Provider callback",
    security: "x-callback-token + webhook-id",
  };
  if (providerCode === "midtrans") return {
    delivery: "Per-transaction override",
    security: "SHA-512 + Server Key",
  };
  return {
    delivery: "Connector callback",
    security: "Connector signature policy",
  };
}

export function ProviderWebhookStatus({ installation }: { installation: Installation }) {
  if (installation.status === "UNINSTALLED") return null;

  const profile = monitorProfile(installation.provider_code);
  const endpoint = installation.public_webhook_url?.trim() ?? "";
  const registeredEndpoint = installation.credential_metadata.public_webhook_url?.trim() ?? "";
  const endpointChanged = Boolean(endpoint && registeredEndpoint && endpoint !== registeredEndpoint);
  const automaticDelivery = installation.provider_code === "midtrans";
  const waitingForEmisell = Boolean(endpoint && !automaticDelivery && !registeredEndpoint);
  const ready = Boolean(endpoint && installation.credential_metadata.webhook_ready && !endpointChanged && !waitingForEmisell);

  return <details className={`provider-webhook-setup ${ready ? "is-ready" : "needs-setup"}`} open={!ready}>
    <summary>
      <span className="provider-webhook-icon"><Icon name="webhook" size={16}/></span>
      <span><strong>Provider webhook ingress</strong><small>{ready ? "Status provider diterima dan diverifikasi Payment Proxy" : "Perlu perhatian operasional"}</small></span>
      <em className={ready ? "ready" : "pending"}><i/>{ready ? "HEALTHY" : "ATTENTION"}</em>
      <b>⌄</b>
    </summary>
    <div className="provider-webhook-body provider-webhook-monitor">
      <header><div><span>INTERNAL MONITORING</span><h4>Webhook delivery path</h4><p>Konfigurasi seller dikelola di Dashboard Emisell. Halaman Payment Proxy hanya memonitor penerimaan, verifikasi, normalisasi, dan penerusan status.</p></div></header>
      {!endpoint && <div className="provider-webhook-missing"><Icon name="activity" size={16}/><span><strong>Public webhook ingress belum tersedia.</strong><small>Operator infrastruktur perlu mengatur domain publik Payment Proxy.</small></span></div>}
      {waitingForEmisell && <div className="provider-webhook-missing"><Icon name="activity" size={16}/><span><strong>Menunggu sinkronisasi Dashboard Emisell.</strong><small>Endpoint sudah tersedia, tetapi konfigurasi callback seller belum dikonfirmasi melalui alur koneksi Emisell.</small></span></div>}
      {endpointChanged && <div className="provider-webhook-missing"><Icon name="activity" size={16}/><span><strong>Domain webhook berubah.</strong><small>Dashboard Emisell perlu menyinkronkan ulang koneksi provider sebelum transaksi live.</small></span></div>}
      <div className="provider-webhook-runtime-grid">
        <span><small>INGRESS</small><strong>{installation.provider_name} → Payment Proxy</strong></span>
        <span><small>DELIVERY</small><strong>{profile.delivery}</strong></span>
        <span><small>SECURITY</small><strong>{profile.security}</strong></span>
        <span><small>EGRESS</small><strong>Payment Proxy → Emisell Backend</strong></span>
      </div>
      <details className="provider-webhook-technical">
        <summary>Technical details <b>⌄</b></summary>
        <div className="provider-webhook-endpoint"><span><small>PUBLIC INGRESS — READ ONLY</small><code>{endpoint || "Not configured"}</code></span></div>
      </details>
      <footer><Icon name="key" size={14}/><span>Payload provider diverifikasi dan dinormalisasi oleh Payment Proxy. Payload mentah tidak diteruskan ke Emisell Backend.</span></footer>
    </div>
  </details>;
}
