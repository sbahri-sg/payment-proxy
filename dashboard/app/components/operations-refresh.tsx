"use client";

import { useCallback, useEffect, useTransition } from "react";
import { useRouter } from "next/navigation";
import styles from "./operations-refresh.module.css";

// Refresh the dashboard's read-only admin projections, never provider APIs.
export function OperationsRefresh({ refreshedAt }: { refreshedAt: string }) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const refresh = useCallback(() => {
    if (!pending) startTransition(() => router.refresh());
  }, [pending, router]);

  useEffect(() => {
    function refreshWhenIdle() {
      if (document.visibilityState !== "visible") return;
      if (document.activeElement?.closest("form, input, textarea, select, [contenteditable='true']")) return;
      if (document.querySelector('[data-auto-refresh-pause="true"]')) return;
      refresh();
    }
    const interval = window.setInterval(refreshWhenIdle, 15_000);
    document.addEventListener("visibilitychange", refreshWhenIdle);
    return () => {
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", refreshWhenIdle);
    };
  }, [refresh]);

  const updated = new Intl.DateTimeFormat("id-ID", {
    hour: "2-digit", minute: "2-digit", second: "2-digit", timeZone: "Asia/Jakarta",
  }).format(new Date(refreshedAt));

  return (
    <div className={styles.control}>
      <span title="Refreshes every 15 seconds while visible; pauses during editing or confirmation.">
        Auto-refresh · 15s<small>Updated {updated} WIB</small>
      </span>
      <button type="button" className="secondary-button" onClick={refresh} disabled={pending}>
        {pending ? "Refreshing..." : "Refresh"}
      </button>
    </div>
  );
}
