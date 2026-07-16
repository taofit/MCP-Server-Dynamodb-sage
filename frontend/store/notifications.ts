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

export function notifId(n: Notification): string {
  return `${n.jobId}-${n.timestamp}`;
}

export interface Toast {
  id: string;
  notification: Notification;
  createdAt: number;
}

interface NotificationsState {
  notifications: Notification[];
  readIds: string[];
  toasts: Toast[];
  fetchNotifications: () => Promise<void>;
  markAsRead: (id: string) => void;
  markAllAsRead: () => void;
  pushToast: (n: Notification) => void;
  dismissToast: (id: string) => void;
}

export const useNotificationsStore = create<NotificationsState>()(
  persist(
    (set) => ({
      notifications: [],
      readIds: [],
      toasts: [],

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
          readIds: state.notifications.map(notifId),
        }));
      },

      pushToast: (n: Notification) => {
        const id = notifId(n) + "-toast-" + Date.now();
        set((state) => ({
          toasts: [...state.toasts, { id, notification: n, createdAt: Date.now() }],
        }));
        setTimeout(() => {
          set((state) => ({
            toasts: state.toasts.filter((t) => t.id !== id),
          }));
        }, 5000);
      },

      dismissToast: (id: string) => {
        set((state) => ({
          toasts: state.toasts.filter((t) => t.id !== id),
        }));
      },
    }),
    {
      name: "dynamo-sage-notifications",
      partialize: (state) => ({ readIds: state.readIds }),
    }
  )
);
