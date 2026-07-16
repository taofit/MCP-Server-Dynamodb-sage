"use client";

import { useEffect } from "react";
import { useNotificationsStore, Notification } from "@/store/notifications";

export function SSEProvider({ children }: { children: React.ReactNode }) {
  const pushToast = useNotificationsStore((s) => s.pushToast);
  const fetchNotifications = useNotificationsStore((s) => s.fetchNotifications);

  useEffect(() => {
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      const sseBase = typeof window !== "undefined" && window.location.hostname === "localhost"
        ? (process.env.NEXT_PUBLIC_SSE_URL || "http://localhost:8081")
        : "";
      const evtSource = new EventSource(sseBase + "/api/events");

      evtSource.onmessage = (event) => {
        try {
          const data: Notification = JSON.parse(event.data);
          pushToast(data);
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
  }, [pushToast, fetchNotifications]);

  return <>{children}</>;
}
