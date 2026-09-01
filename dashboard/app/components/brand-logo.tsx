import Image from "next/image";

const providerAssets: Record<string, string[]> = {
  xendit: ["/brands/providers/xendit.svg"],
  midtrans: ["/brands/providers/midtrans.svg"],
  doku: ["/brands/providers/doku.svg"],
  duitku: ["/brands/providers/duitku.png"],
};

const paymentMethodAssets: Record<string, string[]> = {
  qris: ["/brands/payment-methods/qris.png"],
  card: [
    "/brands/payment-methods/visa.png",
    "/brands/payment-methods/mastercard.png",
    "/brands/payment-methods/jcb.png",
  ],
  va_bca: ["/brands/payment-methods/bca.png"],
  va_mandiri: ["/brands/payment-methods/mandiri.png"],
  va_bni: ["/brands/payment-methods/bni.png"],
  va_bri: ["/brands/payment-methods/bri.png"],
  va_permata: ["/brands/payment-methods/permata.png"],
  va_cimb: ["/brands/payment-methods/cimb-niaga.png"],
  va_danamon: ["/brands/payment-methods/danamon.png"],
  va_bsi: ["/brands/payment-methods/bsi.webp"],
  va_maybank: ["/brands/payment-methods/maybank.png"],
  va_bnc: ["/brands/payment-methods/bnc.png"],
  va_btn: ["/brands/payment-methods/btn.webp"],
  va_atm_bersama: ["/brands/payment-methods/atm-bersama.png"],
  va_arta_graha: ["/brands/payment-methods/artha-graha.png"],
  va_sahabat_sampoerna: ["/brands/payment-methods/bank-sampoerna.png"],
  va_muamalat: ["/brands/payment-methods/muamalat.webp"],
  va_doku: ["/brands/payment-methods/doku.svg"],
  ewallet_gopay: ["/brands/payment-methods/gopay.svg"],
  ewallet_ovo: ["/brands/payment-methods/ovo.png"],
  ewallet_dana: ["/brands/payment-methods/dana.png"],
  ewallet_shopeepay: ["/brands/payment-methods/shopeepay.png"],
  ewallet_linkaja: ["/brands/payment-methods/linkaja.jpg"],
  ewallet_astrapay: ["/brands/payment-methods/astrapay.png"],
  ewallet_doku: ["/brands/payment-methods/doku.svg"],
  retail_alfamart: ["/brands/payment-methods/alfamart.png"],
  retail_indomaret: ["/brands/payment-methods/indomaret.png"],
  paylater_kredivo: ["/brands/payment-methods/kredivo.png"],
  paylater_akulaku: ["/brands/payment-methods/akulaku.svg"],
  paylater_indodana: ["/brands/payment-methods/indodana.png"],
  paylater_atome: ["/brands/payment-methods/atome.png"],
  direct_debit_bri: ["/brands/payment-methods/bri.png"],
  digital_banking_jenius: ["/brands/payment-methods/jenius.svg"],
};

function fallbackLabel(label: string) {
  const tokens = label.replace(/virtual account|direct debit|wallet/gi, "").trim().split(/[\s/]+/).filter(Boolean);
  const first = tokens[0] ?? label;
  if (/^[A-Z0-9]{2,5}$/.test(first)) return first.slice(0, 3);
  return tokens.slice(0, 2).map((token) => token[0]).join("").toUpperCase() || "PM";
}

export function BrandLogo({
  code,
  label,
  kind = "provider",
  className = "",
  priority = false,
  customSrc,
}: {
  code: string;
  label: string;
  kind?: "provider" | "payment-method";
  className?: string;
  priority?: boolean;
  customSrc?: string;
}) {
  const assets = customSrc ? [customSrc] : kind === "provider" ? providerAssets[code] : paymentMethodAssets[code];
  const classes = ["brand-logo", `brand-logo-${kind}`, assets?.length ? "has-asset" : "fallback", assets?.length > 1 ? "stacked" : "", className].filter(Boolean).join(" ");

  return (
    <span className={classes} title={label} aria-hidden="true">
      {assets?.map((src) => <Image key={src} src={src} alt="" width={64} height={40} sizes="64px" priority={priority}/>) ?? <b>{fallbackLabel(label)}</b>}
    </span>
  );
}
