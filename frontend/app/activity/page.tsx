"use client";

import { useEffect, useState, useMemo } from "react";
import { Search, ChevronDown, ChevronRight, CheckCircle, XCircle, AlertTriangle, Info } from "lucide-react";
import { cn } from "@/lib/utils";
import { useNotificationsStore, type Notification } from "@/store/notifications";

const timeRanges = ["Today", "This Week", "All"] as const;

const statusIcon: Record<string, React.ReactNode> = {
  success: <CheckCircle className="w-4 h-4 text-emerald-500" />,
  error: <XCircle className="w-4 h-4 text-red-500" />,
  warning: <AlertTriangle className="w-4 h-4 text-amber-500" />,
  info: <Info className="w-4 h-4 text-blue-500" />,
};

function formatTime(ts: number): string {
  const d = new Date(ts * 1000);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function isToday(ts: number): boolean {
  const d = new Date(ts * 1000);
  const now = new Date();
  return d.toDateString() === now.toDateString();
}

function isThisWeek(ts: number): boolean {
  const d = new Date(ts * 1000);
  const now = new Date();
  const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
  return d >= weekAgo;
}

export default function ActivityPage() {
  const [range, setRange] = useState<(typeof timeRanges)[number]>("Today");
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const notifications = useNotificationsStore((s) => s.notifications);
  const fetchNotifications = useNotificationsStore((s) => s.fetchNotifications);

  useEffect(() => {
    fetchNotifications();
    const interval = setInterval(fetchNotifications, 5000);
    return () => clearInterval(interval);
  }, [fetchNotifications]);

  useEffect(() => {
    const groups = (notifications ?? []).reduce<Record<string, number>>((acc, n) => {
      acc[n.table] = (acc[n.table] || 0) + 1;
      return acc;
    }, {});
    const initialExpanded: Record<string, boolean> = {};
    Object.keys(groups).slice(0, 3).forEach((k) => {
      initialExpanded[k] = true;
    });
    setExpanded(initialExpanded);
  }, [notifications]);

  const filtered = useMemo(() => {
    return (notifications ?? []).filter((n) => {
      if (range === "Today") return isToday(n.timestamp);
      if (range === "This Week") return isThisWeek(n.timestamp);
      return true;
    });
  }, [notifications, range]);

  const groups = useMemo(() => {
    const grouped = filtered.reduce<Record<string, Notification[]>>((acc, n) => {
      const key = n.table || "unknown";
      if (!acc[key]) acc[key] = [];
      acc[key].push(n);
      return acc;
    }, {});

    return Object.entries(grouped)
      .filter(([key]) => key.toLowerCase().includes(search.toLowerCase()))
      .map(([key, items]) => ({
        key,
        count: items.length,
        items: items.map((n) => ({
          operation: n.operation,
          status: n.severity as "success" | "error" | "warning" | "info",
          time: formatTime(n.timestamp),
          message: n.message,
        })),
      }));
  }, [filtered, search]);

  const toggle = (key: string) =>
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }));

  const successful = filtered.filter((n) => n.severity === "success").length;
  const failed = filtered.filter((n) => n.severity === "error").length;
  const warnings = filtered.filter((n) => n.severity === "warning").length;
  const successRate = filtered.length > 0 ? Math.round((successful / filtered.length) * 100) : 0;

  return (
    <div className="flex-1 p-6 max-w-5xl mx-auto w-full space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold">Activity</h1>
        <p className="text-muted-foreground text-sm mt-1">
          DynamoDB operations grouped by table name.
        </p>
      </div>

      {(notifications ?? []).length === 0 ? (
        <div className="text-center text-muted-foreground py-8">No activity yet. Perform a DynamoDB operation to see it here.</div>
      ) : (
        <>
          {/* Filters */}
          <div className="flex items-center gap-4">
            <div className="flex gap-1 bg-card rounded-lg p-1 border border-border">
              {timeRanges.map((r) => (
                <button
                  key={r}
                  onClick={() => setRange(r)}
                  className={cn(
                    "px-3 py-1.5 rounded-md text-xs font-medium transition-colors",
                    r === range
                      ? "bg-accent text-foreground"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  {r}
                </button>
              ))}
            </div>
            <div className="relative flex-1 max-w-xs">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <input
                type="text"
                placeholder="Search tables..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full bg-card border border-border rounded-lg pl-9 pr-4 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:border-ring"
              />
            </div>
          </div>

          {/* Summary Cards */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="rounded-xl border border-border bg-card/50 p-4">
              <p className="text-xs text-muted-foreground uppercase tracking-wider">Success Rate</p>
              <p className={`text-2xl font-bold mt-1 ${successRate >= 90 ? "text-emerald-500" : successRate >= 70 ? "text-amber-500" : "text-red-500"}`}>{successRate}%</p>
            </div>
            <div className="rounded-xl border border-border bg-card/50 p-4">
              <p className="text-xs text-muted-foreground uppercase tracking-wider">Successful</p>
              <p className="text-2xl font-bold mt-1 text-emerald-500">{successful}</p>
            </div>
            <div className="rounded-xl border border-border bg-card/50 p-4">
              <p className="text-xs text-muted-foreground uppercase tracking-wider">Failed</p>
              <p className="text-2xl font-bold mt-1 text-red-500">{failed}</p>
            </div>
            <div className="rounded-xl border border-border bg-card/50 p-4">
              <p className="text-xs text-muted-foreground uppercase tracking-wider">Warnings</p>
              <p className="text-2xl font-bold mt-1 text-amber-500">{warnings}</p>
            </div>
          </div>

          {/* Groups */}
          {groups.length === 0 ? (
            <div className="text-center text-muted-foreground py-8">No activity found for this time range.</div>
          ) : (
            <div className="space-y-2">
              {groups.map((group) => (
                <div
                  key={group.key}
                  className="rounded-xl border border-border bg-card/50 overflow-hidden"
                >
                  <button
                    onClick={() => toggle(group.key)}
                    className="w-full flex items-center justify-between px-4 py-3 hover:bg-accent/50 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      {expanded[group.key] ? (
                        <ChevronDown className="w-4 h-4 text-muted-foreground" />
                      ) : (
                        <ChevronRight className="w-4 h-4 text-muted-foreground" />
                      )}
                      <span className="text-xs text-muted-foreground uppercase tracking-wider">Table</span>
                      <span className="font-medium text-sm">{group.key}</span>
                      <span className="text-xs text-muted-foreground bg-accent px-2 py-0.5 rounded-full">
                        {group.count} ops
                      </span>
                    </div>
                  </button>

                  {expanded[group.key] && (
                    <div className="border-t border-border">
                      {group.items.map((item, i) => (
                        <div
                          key={i}
                          className="flex items-center justify-between px-4 py-2.5 text-sm border-b border-border/50 last:border-0"
                        >
                          <div className="flex items-center gap-3">
                            {statusIcon[item.status] || statusIcon.info}
                            <span className="text-foreground/80">{item.operation}</span>
                          </div>
                          <div className="flex items-center gap-4 text-xs text-muted-foreground">
                            <span>{item.time}</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
