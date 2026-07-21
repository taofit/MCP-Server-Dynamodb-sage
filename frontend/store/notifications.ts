import { create } from "zustand";
import { persist } from "zustand/middleware";

export interface Notification {
  title: string;
  jobId: string;
  table: string;
  severity: "success" | "error" | "warning" | "info";
  operation: string;
  message: string;
  inputHash: string;
  timestamp: number;
}

export function notifId(n: Notification, index?: number): string {
  if (n.jobId) return `${n.jobId}-${n.timestamp}`;
  return `${n.operation}-${n.table}-${n.timestamp}-${index ?? 0}`;
}

interface NotificationsState {
  notifications: Notification[];
  readIds: string[];
  fetchNotifications: () => Promise<void>;
  markAsRead: (id: string) => void;
  markAllAsRead: () => void;
}

export const useNotificationsStore = create<NotificationsState>()(
  persist(
    (set) => ({
      notifications: [],
      readIds: [],

      fetchNotifications: async () => {
        try {
          const res = await fetch("/api/notifications");
          if (!res.ok) return;
          const data: Notification[] = await res.json();
          set({ notifications: data });
        } catch {
          // silently ignore
        }
      },

      markAsRead: (id: string) => {
        set((state) => {
          if (state.readIds.includes(id)) return state;
          return { readIds: [...state.readIds, id] };
        });
      },

      markAllAsRead: () => {
        set((state) => ({
          readIds: state.notifications.map((n, i) => notifId(n, i)),
        }));
      },
    }),
    {
      name: "dynamo-sage-notifications",
      partialize: (state) => ({ readIds: state.readIds }),
    }
  )
);
