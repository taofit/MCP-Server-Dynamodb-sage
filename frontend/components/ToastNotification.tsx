"use client";

import { useEffect, useState } from "react";
import { CheckCircle, XCircle, AlertTriangle, Info, X } from "lucide-react";
import { useNotificationsStore } from "@/store/notifications";

const severityConfig = {
  success: {
    icon: CheckCircle,
    bg: "bg-emerald-50 dark:bg-emerald-950",
    border: "border-emerald-200 dark:border-emerald-800",
    iconColor: "text-emerald-500",
  },
  error: {
    icon: XCircle,
    bg: "bg-red-50 dark:bg-red-950",
    border: "border-red-200 dark:border-red-800",
    iconColor: "text-red-500",
  },
  warning: {
    icon: AlertTriangle,
    bg: "bg-amber-50 dark:bg-amber-950",
    border: "border-amber-200 dark:border-amber-800",
    iconColor: "text-amber-500",
  },
  info: {
    icon: Info,
    bg: "bg-blue-50 dark:bg-blue-950",
    border: "border-blue-200 dark:border-blue-800",
    iconColor: "text-blue-500",
  },
};

export function ToastContainer() {
  const toasts = useNotificationsStore((s) => s.toasts);
  const dismissToast = useNotificationsStore((s) => s.dismissToast);

  if (toasts.length === 0) return null;

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm">
      {toasts.map((toast) => (
        <ToastItem
          key={toast.id}
          toast={toast}
          onDismiss={() => dismissToast(toast.id)}
        />
      ))}
    </div>
  );
}

function ToastItem({
  toast,
  onDismiss,
}: {
  toast: { id: string; notification: import("@/store/notifications").Notification };
  onDismiss: () => void;
}) {
  const [visible, setVisible] = useState(false);
  const { notification } = toast;
  const config = severityConfig[notification.severity] || severityConfig.info;
  const Icon = config.icon;

  useEffect(() => {
    requestAnimationFrame(() => setVisible(true));
  }, []);

  return (
    <div
      className={`${config.bg} ${config.border} border rounded-xl shadow-lg p-3 flex items-start gap-3 transition-all duration-300 ${
        visible ? "opacity-100 translate-x-0" : "opacity-0 translate-x-8"
      }`}
    >
      <Icon className={`w-4 h-4 mt-0.5 shrink-0 ${config.iconColor}`} />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium truncate">{notification.operation}</p>
        <p className="text-xs text-muted-foreground truncate mt-0.5">
          {notification.table && (
            <span className="font-mono">{notification.table}</span>
          )}
          {notification.table && notification.message ? " — " : ""}
          {notification.message}
        </p>
      </div>
      <button
        onClick={onDismiss}
        className="shrink-0 p-0.5 rounded-md hover:bg-black/5 dark:hover:bg-white/10 transition-colors"
      >
        <X className="w-3.5 h-3.5 text-muted-foreground" />
      </button>
    </div>
  );
}
