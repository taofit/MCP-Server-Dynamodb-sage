"use client";

import { useEffect, useRef } from "react";
import { Bell, CheckCircle, XCircle, AlertTriangle, Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useNotificationsStore, notifId } from "@/store/notifications";
import Link from "next/link";

const severityIcon: Record<string, React.ReactNode> = {
  success: <CheckCircle className="w-4 h-4 text-emerald-500 shrink-0" />,
  error: <XCircle className="w-4 h-4 text-red-500 shrink-0" />,
  warning: <AlertTriangle className="w-4 h-4 text-amber-500 shrink-0" />,
  info: <Info className="w-4 h-4 text-blue-500 shrink-0" />,
};

function timeAgo(ts: number): string {
  const now = Date.now() / 1000;
  const diff = now - ts;
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

const MAX_DISPLAYED = 10;

export function NotificationBell() {
  const notifications = useNotificationsStore((s) => s.notifications);
  const readIds = useNotificationsStore((s) => s.readIds);
  const fetchNotifications = useNotificationsStore((s) => s.fetchNotifications);
  const markAsRead = useNotificationsStore((s) => s.markAsRead);
  const markAllAsRead = useNotificationsStore((s) => s.markAllAsRead);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    fetchNotifications();
    intervalRef.current = setInterval(fetchNotifications, 15000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchNotifications]);

  const unreadCount = (notifications ?? []).filter(
    (n, i) => !readIds.includes(notifId(n, i))
  ).length;

  const recent = (notifications ?? []).slice(0, MAX_DISPLAYED);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" className="relative" />
        }
      >
        <Bell className="w-5 h-5 text-muted-foreground" />
        {unreadCount > 0 && (
          <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-bold text-white">
            {unreadCount > 99 ? "99+" : unreadCount}
          </span>
        )}
      </DropdownMenuTrigger>

      <DropdownMenuContent align="end" sideOffset={8} className="w-80">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="flex items-center justify-between">
            <span>Notifications</span>
            {unreadCount > 0 && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  markAllAsRead();
                }}
                className="text-xs text-muted-foreground hover:text-foreground font-normal"
              >
                Mark all read
              </button>
            )}
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />

        {recent.length === 0 ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">
            No notifications
          </div>
        ) : (
          recent.map((n, i) => {
            const id = notifId(n, i);
            const read = readIds.includes(id);
            return (
              <DropdownMenuItem
                key={id}
                onClick={() => markAsRead(id)}
                className="flex items-start gap-2.5 py-2.5 cursor-pointer"
              >
                <span className="mt-0.5">{severityIcon[n.severity] || severityIcon.info}</span>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium truncate">
                      {n.operation}
                    </span>
                    {!read && (
                      <span className="h-1.5 w-1.5 rounded-full bg-blue-500 shrink-0" />
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground truncate mt-0.5">
                    {n.table && <span className="font-mono">{n.table}</span>}
                    {n.table && n.message ? " \u2014 " : ""}
                    {n.message}
                  </p>
                </div>
                <span className="text-[11px] text-muted-foreground whitespace-nowrap shrink-0">
                  {timeAgo(n.timestamp)}
                </span>
              </DropdownMenuItem>
            );
          })
        )}

        <DropdownMenuSeparator />
        <DropdownMenuItem
          render={<Link href="/activity/" />}
          className="justify-center text-sm text-muted-foreground"
        >
          View all activity
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
