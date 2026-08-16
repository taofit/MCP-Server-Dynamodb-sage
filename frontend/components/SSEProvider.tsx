"use client";

import { useEffect } from "react";
import { toast } from "sonner";
import { useNotificationsStore, Notification } from "@/store/notifications";
import { sseUrl } from "@/lib/api";

export function SSEProvider({ children }: { children: React.ReactNode }) {
  const fetchNotifications = useNotificationsStore((s) => s.fetchNotifications);

  useEffect(() => {
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      const sseBase =
        process.env.NEXT_PUBLIC_SSE_URL ||
        (typeof window !== "undefined" &&
        (window.location.hostname === "localhost" ||
          window.location.hostname === "127.0.0.1")
          ? "http://localhost:8081"
          : `http://${window.location.hostname}:8081`);
      const evtSource = new EventSource(sseUrl(sseBase + "/api/events"));

      evtSource.onmessage = (event) => {
        try {
          const data: Notification = JSON.parse(event.data);
          const desc = [data.table, data.message].filter(Boolean).join(" — ");
          if (data.severity === "error") {
            toast.error(data.operation, { description: desc });
          } else if (data.severity === "warning") {
            toast.warning(data.operation, { description: desc });
          } else if (data.severity === "success") {
            toast.success(data.operation, { description: desc });
          } else {
            toast.info(data.operation, { description: desc });
          }
          fetchNotifications();
        } catch {
          // ignore malformed events
        }
      };

      evtSource.onerror = () => {
        evtSource.close();
        reconnectTimer = setTimeout(connect, 3000);
      };

      return evtSource;
    }

    const source = connect();

    return () => {
      if (reconnectTimer) clearTimeout(reconnectTimer);
      source.close();
    };
  }, [fetchNotifications]);

  return <>{children}</>;
}
