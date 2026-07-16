"use client";

import { useEffect, useState } from "react";
import {
  MessageSquare,
  Activity,
  BarChart3,
  ArrowRight,
  Database,
  MessageSquareText,
  Bell,
  Zap,
  Link2,
  Clock,
} from "lucide-react";
import Link from "next/link";

type HealthStatus = "ok" | "error" | "not_configured";

interface Stats {
  tables: number;
  chatMessages: number;
  notifications: number;
  toolCalls: number;
  active_connections: number;
  uptime_seconds: number;
}

const quickActions = [
  { label: "New Query", href: "/chat/", icon: MessageSquare, color: "text-blue-500" },
  { label: "Browse Tables", href: "/tables/", icon: Database, color: "text-violet-500" },
  { label: "View Activity", href: "/activity/", icon: Activity, color: "text-amber-500" },
  { label: "Check Metrics", href: "/monitoring/", icon: BarChart3, color: "text-emerald-500" },
];

const statusColor: Record<HealthStatus, string> = {
  ok: "bg-emerald-500",
  error: "bg-red-500",
  not_configured: "bg-zinc-600",
};

const statusLabel: Record<HealthStatus, string> = {
  ok: "Operational",
  error: "Error",
  not_configured: "Not configured",
};

const services = [
  { key: "dynamodb", label: "DynamoDB" },
  { key: "kafka", label: "Kafka" },
  { key: "llm", label: "LLM API" },
] as const;

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

export default function OverviewPage() {
  const [health, setHealth] = useState<Record<string, HealthStatus>>({});
  const [stats, setStats] = useState<Stats | null>(null);

  useEffect(() => {
    fetch("/api/health")
      .then((r) => r.json())
      .then(setHealth)
      .catch(() =>
        setHealth({ dynamodb: "error", kafka: "error", llm: "error" })
      );
    fetch("/api/stats")
      .then((r) => r.json())
      .then(setStats)
      .catch(() => {});
  }, []);

  const statCards = [
    { label: "Tables", value: stats?.tables ?? "—", icon: Database, color: "text-blue-500" },
    { label: "Chat Messages", value: stats?.chatMessages ?? "—", icon: MessageSquareText, color: "text-violet-500" },
    { label: "Tool Calls", value: stats?.toolCalls ?? "—", icon: Zap, color: "text-amber-500" },
    { label: "Notifications", value: stats?.notifications ?? "—", icon: Bell, color: "text-rose-500" },
    { label: "Connections", value: stats?.active_connections ?? "—", icon: Link2, color: "text-emerald-500" },
    { label: "Uptime", value: stats ? formatUptime(stats.uptime_seconds) : "—", icon: Clock, color: "text-cyan-500" },
  ];

  return (
    <div className="flex-1 p-6 max-w-5xl mx-auto w-full space-y-8">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground text-sm mt-1">
          Natural language interface for Amazon DynamoDB — query, modify, and explore your tables using everyday English. Powered by MCP tool-calling with built-in guardrails, risk analysis, and audit logging.
        </p>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
        {statCards.map((stat) => (
          <div
            key={stat.label}
            className="rounded-xl border border-border bg-card/50 p-4"
          >
            <div className="flex items-center justify-between mb-3">
              <span className="text-xs text-muted-foreground uppercase tracking-wider">
                {stat.label}
              </span>
              <stat.icon className={`w-4 h-4 ${stat.color}`} />
            </div>
            <p className="text-2xl font-bold">{stat.value}</p>
          </div>
        ))}
      </div>

      {/* Quick Actions */}
      <div>
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
          Quick Actions
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
          {quickActions.map((action) => (
            <Link
              key={action.label}
              href={action.href}
              className="group flex items-center justify-between rounded-xl border border-border bg-card/50 p-4 hover:border-border hover:bg-accent/50 transition-colors"
            >
              <div className="flex items-center gap-3">
                <action.icon className={`w-5 h-5 ${action.color}`} />
                <span className="text-sm font-medium">{action.label}</span>
              </div>
              <ArrowRight className="w-4 h-4 text-muted-foreground group-hover:text-foreground transition-colors" />
            </Link>
          ))}
        </div>
      </div>

      {/* System Health */}
      <div>
        <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
          System Health
        </h2>
        <div className="rounded-xl border border-border bg-card/50 divide-y divide-border">
          {services.map(({ key, label }) => {
            const status = health[key] ?? "not_configured";
            return (
              <div
                key={key}
                className="flex items-center justify-between px-4 py-3"
              >
                <div className="flex items-center gap-3">
                  <span
                    className={`w-2 h-2 rounded-full ${statusColor[status]}`}
                  />
                  <span className="text-sm">{label}</span>
                </div>
                <span className="text-xs text-muted-foreground">
                  {statusLabel[status]}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
