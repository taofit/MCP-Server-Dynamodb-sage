"use client";

import { useEffect, useState, useRef, useCallback } from "react";
import {
  LineChart,
  Line,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
} from "recharts";
import {
  Clock,
  ChevronDown,
  ChevronRight,
  Server,
  AlertTriangle,
  TrendingUp,
  Gauge,
  MessageSquare,
} from "lucide-react";

type HealthStatus = "ok" | "error" | "not_configured";

interface Stats {
  tables: number;
  chatMessages: number;
  notifications: number;
  toolCalls: number;
  active_connections: number;
  uptime_seconds: number;
}

interface PrometheusMetric {
  name: string;
  value: number;
  labels?: Record<string, string>;
}

interface DashboardData {
  timestamp: string;
  toolCalls: number;
  toolErrors: number;
  dynamoOps: number;
  kafkaLag: number;
  goroutines: number;
  heapMB: number;
  latencyP95: number;
  errorRate: number;
}

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

const POLL_INTERVAL = 10000;
const MAX_TREND_POINTS = 60;

function parsePrometheusMetrics(text: string): PrometheusMetric[] {
  const metrics: PrometheusMetric[] = [];
  const lines = text.split("\n");
  for (const line of lines) {
    if (line.startsWith("#") || line.trim() === "") continue;
    const match = line.match(
      /^([a-zA-Z_:][a-zA-Z0-9_:]*)\{([^}]*)?\}\s+(.+)$/
    );
    if (match) {
      const [, name, labelsStr, valueStr] = match;
      const value = parseFloat(valueStr);
      if (!isNaN(value)) {
        const labels: Record<string, string> = {};
        if (labelsStr) {
          for (const pair of labelsStr.split(",")) {
            const [k, v] = pair.split("=");
            labels[k.trim()] = v?.replace(/"/g, "").trim() || "";
          }
        }
        metrics.push({ name, value, labels });
      }
    } else {
      const simpleMatch = line.match(
        /^([a-zA-Z_:][a-zA-Z0-9_:]*)\s+(.+)$/
      );
      if (simpleMatch) {
        const [, name, valueStr] = simpleMatch;
        const value = parseFloat(valueStr);
        if (!isNaN(value)) {
          metrics.push({ name, value });
        }
      }
    }
  }
  return metrics;
}

function getMetric(metrics: PrometheusMetric[], name: string): number {
  return metrics.find((m) => m.name === name && !m.labels)?.value ?? 0;
}

function sumByLabel(
  metrics: PrometheusMetric[],
  name: string,
  label: string
): Record<string, number> {
  const result: Record<string, number> = {};
  for (const m of metrics) {
    if (m.name === name && m.labels?.[label]) {
      const key = m.labels[label];
      result[key] = (result[key] || 0) + m.value;
    }
  }
  return result;
}

function sumMetrics(metrics: PrometheusMetric[], name: string): number {
  return metrics
    .filter((m) => m.name === name && !m.labels)
    .reduce((acc, m) => acc + m.value, 0);
}

function calcPercentile(
  metrics: PrometheusMetric[],
  prefix: string,
  p: number
): number {
  const buckets: { le: number; count: number }[] = [];
  let totalCount = 0;

  for (const m of metrics) {
    if (m.name === prefix && m.labels?.le) {
      const le = parseFloat(m.labels.le);
      buckets.push({ le, count: m.value });
      totalCount = Math.max(totalCount, m.value);
    }
  }

  if (totalCount === 0) return 0;
  const target = totalCount * p;

  buckets.sort((a, b) => a.le - b.le);
  for (const b of buckets) {
    if (b.count >= target) return b.le;
  }
  return buckets[buckets.length - 1]?.le ?? 0;
}

function sumGaugeByLabel(
  metrics: PrometheusMetric[],
  name: string,
  label: string
): number {
  let total = 0;
  for (const m of metrics) {
    if (m.name === name && m.labels?.[label]) {
      total += m.value;
    }
  }
  return total;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes.toFixed(0)} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function formatDuration(seconds: number): string {
  if (seconds < 0.001) return `${(seconds * 1000000).toFixed(0)} µs`;
  if (seconds < 1) return `${(seconds * 1000).toFixed(1)} ms`;
  return `${seconds.toFixed(2)} s`;
}

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function now() {
  return new Date().toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function MetricCard({
  label,
  value,
  sub,
  icon: Icon,
  color,
  alert,
}: {
  label: string;
  value: string | number;
  sub?: string;
  icon: React.ElementType;
  color: string;
  alert?: boolean;
}) {
  return (
    <div
      className={`rounded-xl border bg-card/50 p-4 transition-colors ${
        alert ? "border-amber-500/50 bg-amber-500/5" : "border-border"
      }`}
    >
      <div className="flex items-center justify-between mb-3">
        <span className="text-xs text-muted-foreground uppercase tracking-wider">
          {label}
        </span>
        <div className="flex items-center gap-2">
          {alert && (
            <AlertTriangle className="w-3.5 h-3.5 text-amber-500 animate-pulse" />
          )}
          <Icon className={`w-4 h-4 ${color}`} />
        </div>
      </div>
      <p className="text-2xl font-bold">{value}</p>
      {sub && <p className="text-xs text-muted-foreground mt-1">{sub}</p>}
    </div>
  );
}

function Section({
  title,
  children,
  defaultOpen = true,
}: {
  title: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="rounded-xl border border-border bg-card/50 overflow-hidden">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between px-4 py-3 hover:bg-accent/50 transition-colors"
      >
        <h3 className="text-sm font-semibold text-foreground/80">{title}</h3>
        {open ? (
          <ChevronDown className="w-4 h-4 text-muted-foreground" />
        ) : (
          <ChevronRight className="w-4 h-4 text-muted-foreground" />
        )}
      </button>
      {open && <div className="border-t border-border p-4">{children}</div>}
    </div>
  );
}

function BarRow({
  label,
  value,
  max,
}: {
  label: string;
  value: number;
  max: number;
}) {
  const pct = max > 0 ? (value / max) * 100 : 0;
  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="w-32 text-muted-foreground text-right font-mono text-xs truncate">
        {label}
      </span>
      <div className="flex-1 h-2 bg-accent rounded-full overflow-hidden">
        <div
          className="h-full bg-blue-500 rounded-full transition-all duration-500"
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      <span className="w-16 text-right text-foreground/80 font-mono text-xs">
        {value.toLocaleString()}
      </span>
    </div>
  );
}

const chartTooltipStyle = {
  contentStyle: {
    background: "#18181b",
    border: "1px solid #27272a",
    borderRadius: "8px",
    fontSize: "12px",
  },
};

export default function MonitoringPage() {
  const [health, setHealth] = useState<Record<string, HealthStatus>>({});
  const [stats, setStats] = useState<Stats | null>(null);
  const [metrics, setMetrics] = useState<PrometheusMetric[]>([]);
  const [loading, setLoading] = useState(true);
  const [trend, setTrend] = useState<DashboardData[]>([]);
  const prevTotals = useRef({ toolCalls: 0, dynamoOps: 0, toolErrors: 0 });
  const prevTime = useRef(0);

  const fetchData = useCallback(async () => {
    try {
      const [healthData, statsData, metricsText] = await Promise.all([
        fetch("/api/health").then((r) => r.json()),
        fetch("/api/stats").then((r) => r.json()),
        fetch("/api/metrics").then((r) => r.text()),
      ]);
      setHealth(healthData);
      setStats(statsData);
      const parsed = parsePrometheusMetrics(metricsText);
      setMetrics(parsed);

      const nowMs = Date.now();
      if (prevTime.current === 0) {
        prevTime.current = nowMs;
      }
      const elapsed = (nowMs - prevTime.current) / 1000;

      const toolCalls = sumMetrics(parsed, "sage_mcp_tool_invocations_total");
      const toolErrors = sumMetrics(parsed, "sage_mcp_tool_errors_total");
      const dynamoOps = sumMetrics(parsed, "sage_dynamodb_operation_total");
      const kafkaLag = sumGaugeByLabel(parsed, "sage_kafka_consumer_lag", "partition");
      const goroutines = getMetric(parsed, "go_goroutines");
      const heapAlloc = getMetric(parsed, "go_memstats_heap_alloc_bytes");
      const latencyP95 = calcPercentile(
        parsed,
        "sage_mcp_tool_duration_seconds",
        0.95
      );

      const opsPerSec =
        elapsed > 0
          ? (toolCalls - prevTotals.current.toolCalls +
              dynamoOps - prevTotals.current.dynamoOps) /
            elapsed
          : 0;
      const errorRate =
        toolCalls > 0
          ? ((toolErrors - prevTotals.current.toolErrors) / toolCalls) * 100
          : 0;

      prevTotals.current = { toolCalls, dynamoOps, toolErrors };
      prevTime.current = nowMs;

      const point: DashboardData = {
        timestamp: now(),
        toolCalls: Math.round(opsPerSec * 10) / 10,
        toolErrors: Math.round(errorRate * 10) / 10,
        dynamoOps: Math.round(opsPerSec * 10) / 10,
        kafkaLag,
        goroutines,
        heapMB: Math.round((heapAlloc / (1024 * 1024)) * 10) / 10,
        latencyP95: Math.round(latencyP95 * 1000) / 1000,
        errorRate: Math.round(errorRate * 10) / 10,
      };

      setTrend((prev) => [...prev.slice(-(MAX_TREND_POINTS - 1)), point]);
    } catch {
      // silent
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    fetchData();
    const interval = setInterval(fetchData, POLL_INTERVAL);
    return () => clearInterval(interval);
  }, [fetchData]);

  const latest = trend[trend.length - 1];

  const toolCallsByOp = sumByLabel(metrics, "sage_mcp_tool_invocations_total", "tool");
  const toolErrorsByOp = sumByLabel(metrics, "sage_mcp_tool_errors_total", "tool");
  const dynamoOpsByOp = sumByLabel(metrics, "sage_dynamodb_operation_total", "operation");
  const kafkaSends = sumMetrics(metrics, "sage_kafka_send_total");
  const kafkaBytes = sumMetrics(metrics, "sage_kafka_send_bytes_total");
  const kafkaLag = sumGaugeByLabel(metrics, "sage_kafka_consumer_lag", "partition");
  const riskBlocked = getMetric(metrics, "sage_risk_analysis_blocked_total");
  const riskConfirmed = getMetric(metrics, "sage_risk_analysis_confirmed_total");
  const piiDetected = getMetric(metrics, "sage_risk_pii_detected_total");
  const goroutines = getMetric(metrics, "go_goroutines");
  const heapAlloc = getMetric(metrics, "go_memstats_heap_alloc_bytes");
  const heapInuse = getMetric(metrics, "go_memstats_heap_inuse_bytes");
  const heapObjects = getMetric(metrics, "go_memstats_heap_objects");
  const gcRuns = getMetric(metrics, "go_gc_duration_seconds_count");
  const gcSum = getMetric(metrics, "go_gc_duration_seconds_sum");
  const gcAvg = gcRuns > 0 ? gcSum / gcRuns : 0;
  const uptime = stats?.uptime_seconds ?? 0;

  const latencyP95 = calcPercentile(
    metrics,
    "sage_mcp_tool_duration_seconds",
    0.95
  );
  const totalToolCalls = Object.values(toolCallsByOp).reduce((a, b) => a + b, 0);
  const totalToolErrors = Object.values(toolErrorsByOp).reduce((a, b) => a + b, 0);
  const errorRate = totalToolCalls > 0 ? (totalToolErrors / totalToolCalls) * 100 : 0;

  return (
    <div className="flex-1 p-6 max-w-7xl mx-auto w-full space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Monitoring</h1>
          <p className="text-muted-foreground text-sm mt-1">
            Real-time metrics — refreshing every {POLL_INTERVAL / 1000}s
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="relative flex h-2.5 w-2.5">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
            <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-emerald-500" />
          </span>
          <span className="text-xs text-muted-foreground">Live</span>
        </div>
      </div>

      {/* Key Metrics Row */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
        <MetricCard
          label="Latency p95"
          value={formatDuration(latencyP95)}
          icon={Gauge}
          color="text-blue-500"
          sub={latest ? `current` : "—"}
        />
        <MetricCard
          label="Error Rate"
          value={`${latest?.errorRate?.toFixed(1) ?? errorRate.toFixed(1)}%`}
          icon={AlertTriangle}
          color={errorRate > 5 ? "text-red-500" : "text-emerald-500"}
          alert={errorRate > 5}
          sub={`${totalToolErrors} errors / ${totalToolCalls} calls`}
        />
        <MetricCard
          label="Ops / sec"
          value={latest?.toolCalls?.toFixed(1) ?? "0"}
          icon={TrendingUp}
          color="text-amber-500"
          sub="tool + dynamo"
        />
        <MetricCard
          label="Goroutines"
          value={goroutines}
          icon={Server}
          color="text-violet-500"
          sub={`${heapAlloc > 0 ? formatBytes(heapAlloc) : "—"} heap`}
        />
        <MetricCard
          label="Kafka Lag"
          value={kafkaLag}
          icon={MessageSquare}
          color={kafkaLag > 100 ? "text-red-500" : "text-emerald-500"}
          alert={kafkaLag > 100}
          sub={kafkaSends > 0 ? `${kafkaSends} sends` : "not configured"}
        />
      </div>

      {/* Trend Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Latency Trend */}
        <Section title="Latency p95 Trend">
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={trend}>
                <defs>
                  <linearGradient id="latencyGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis
                  dataKey="timestamp"
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                  tickFormatter={(v: number) => `${(v * 1000).toFixed(0)}ms`}
                />
                <Tooltip
                  {...chartTooltipStyle}
                  formatter={(v) => [`${((v as number) * 1000).toFixed(1)} ms`, "p95"]}
                />
                <Area
                  type="monotone"
                  dataKey="latencyP95"
                  stroke="#3b82f6"
                  strokeWidth={2}
                  fill="url(#latencyGrad)"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Section>

        {/* Ops/sec Trend */}
        <Section title="Operations / sec">
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={trend}>
                <defs>
                  <linearGradient id="opsGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="#f59e0b" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis
                  dataKey="timestamp"
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                />
                <Tooltip {...chartTooltipStyle} />
                <Area
                  type="monotone"
                  dataKey="toolCalls"
                  stroke="#f59e0b"
                  strokeWidth={2}
                  fill="url(#opsGrad)"
                  name="ops/s"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </Section>

        {/* Error Rate Trend */}
        <Section title="Error Rate Trend">
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={trend}>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis
                  dataKey="timestamp"
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                  tickFormatter={(v: number) => `${v}%`}
                />
                <Tooltip
                  {...chartTooltipStyle}
                  formatter={(v) => [`${(v as number).toFixed(1)}%`, "error rate"]}
                />
                <Line
                  type="monotone"
                  dataKey="errorRate"
                  stroke="#ef4444"
                  strokeWidth={2}
                  dot={false}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Section>

        {/* Goroutines & Memory */}
        <Section title="Goroutines & Heap Memory">
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={trend}>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis
                  dataKey="timestamp"
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  yAxisId="left"
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis
                  yAxisId="right"
                  orientation="right"
                  tick={{ fill: "#71717a", fontSize: 10 }}
                  axisLine={false}
                  tickLine={false}
                  tickFormatter={(v: number) => `${v}MB`}
                />
                <Tooltip
                  {...chartTooltipStyle}
                  formatter={(v, name) =>
                    name === "goroutines" ? [v, "goroutines"] : [`${v} MB`, "heap"]
                  }
                />
                <Line
                  yAxisId="left"
                  type="monotone"
                  dataKey="goroutines"
                  stroke="#8b5cf6"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  yAxisId="right"
                  type="monotone"
                  dataKey="heapMB"
                  stroke="#06b6d4"
                  strokeWidth={2}
                  dot={false}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Section>
      </div>

      {/* System Health */}
      <div className="rounded-xl border border-border bg-card/50 p-4">
        <h3 className="text-sm font-semibold text-muted-foreground mb-3">
          System Health
        </h3>
        {loading ? (
          <div className="text-muted-foreground text-sm">Loading...</div>
        ) : (
          <div className="flex gap-8">
            {services.map(({ key, label }) => {
              const status = health[key] ?? "not_configured";
              return (
                <div key={key} className="flex items-center gap-2">
                  <span
                    className={`w-2.5 h-2.5 rounded-full ${statusColor[status]}`}
                  />
                  <span className="text-sm text-foreground/80">{label}</span>
                  <span className="text-xs text-muted-foreground">
                    {statusLabel[status]}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Tool Performance */}
      {Object.keys(toolCallsByOp).length > 0 && (
        <Section title="Tool Performance">
          <div className="space-y-3">
            {Object.entries(toolCallsByOp)
              .sort(([, a], [, b]) => b - a)
              .map(([op, count]) => (
                <div key={op} className="flex items-center gap-4">
                  <span className="w-40 text-sm font-mono text-foreground/80 truncate">
                    {op}
                  </span>
                  <div className="flex-1">
                    <BarRow
                      label=""
                      value={count}
                      max={Math.max(...Object.values(toolCallsByOp))}
                    />
                  </div>
                  {toolErrorsByOp[op] ? (
                    <span className="text-xs text-red-400">
                      {toolErrorsByOp[op]} err
                    </span>
                  ) : null}
                </div>
              ))}
          </div>
        </Section>
      )}

      {/* DynamoDB Operations */}
      {Object.keys(dynamoOpsByOp).length > 0 && (
        <Section title="DynamoDB Operations" defaultOpen={false}>
          <div className="space-y-3">
            {Object.entries(dynamoOpsByOp)
              .sort(([, a], [, b]) => b - a)
              .map(([op, count]) => (
                <div key={op} className="flex items-center gap-4">
                  <span className="w-32 text-sm font-mono text-foreground/80">
                    {op}
                  </span>
                  <div className="flex-1">
                    <BarRow
                      label=""
                      value={count}
                      max={Math.max(...Object.values(dynamoOpsByOp))}
                    />
                  </div>
                </div>
              ))}
          </div>
        </Section>
      )}

      {/* Risk Analysis */}
      {(riskBlocked > 0 || riskConfirmed > 0 || piiDetected > 0) && (
        <Section title="Risk Analysis" defaultOpen={false}>
          <div className="grid grid-cols-3 gap-4">
            <div className="text-center p-3 rounded-lg bg-accent/50">
              <p className="text-2xl font-bold text-amber-500">{riskBlocked}</p>
              <p className="text-xs text-muted-foreground mt-1">Blocked</p>
            </div>
            <div className="text-center p-3 rounded-lg bg-accent/50">
              <p className="text-2xl font-bold text-emerald-500">
                {riskConfirmed}
              </p>
              <p className="text-xs text-muted-foreground mt-1">Confirmed</p>
            </div>
            <div className="text-center p-3 rounded-lg bg-accent/50">
              <p className="text-2xl font-bold text-red-500">{piiDetected}</p>
              <p className="text-xs text-muted-foreground mt-1">PII Detected</p>
            </div>
          </div>
        </Section>
      )}

      {/* Kafka */}
      {kafkaSends > 0 && (
        <Section title="Kafka" defaultOpen={false}>
          <div className="grid grid-cols-3 gap-4">
            <div className="text-center p-3 rounded-lg bg-accent/50">
              <p className="text-2xl font-bold text-violet-500">
                {kafkaSends.toLocaleString()}
              </p>
              <p className="text-xs text-muted-foreground mt-1">Messages Sent</p>
            </div>
            <div className="text-center p-3 rounded-lg bg-accent/50">
              <p className="text-2xl font-bold text-violet-500">
                {formatBytes(kafkaBytes)}
              </p>
              <p className="text-xs text-muted-foreground mt-1">Data Sent</p>
            </div>
            <div className="text-center p-3 rounded-lg bg-accent/50">
              <p className="text-2xl font-bold text-violet-500">{kafkaLag}</p>
              <p className="text-xs text-muted-foreground mt-1">Consumer Lag</p>
            </div>
          </div>
        </Section>
      )}

      {/* Go Runtime */}
      <Section title="Go Runtime" defaultOpen={false}>
        <div className="grid grid-cols-2 gap-x-8 gap-y-2 text-sm">
          <div className="flex justify-between py-1.5 border-b border-border/50">
            <span className="text-muted-foreground">Goroutines</span>
            <span className="font-mono text-foreground">{goroutines}</span>
          </div>
          <div className="flex justify-between py-1.5 border-b border-border/50">
            <span className="text-muted-foreground">Heap Alloc</span>
            <span className="font-mono text-foreground">
              {formatBytes(heapAlloc)}
            </span>
          </div>
          <div className="flex justify-between py-1.5 border-b border-border/50">
            <span className="text-muted-foreground">Heap In Use</span>
            <span className="font-mono text-foreground">
              {formatBytes(heapInuse)}
            </span>
          </div>
          <div className="flex justify-between py-1.5 border-b border-border/50">
            <span className="text-muted-foreground">Heap Objects</span>
            <span className="font-mono text-foreground">
              {heapObjects.toLocaleString()}
            </span>
          </div>
          <div className="flex justify-between py-1.5 border-b border-border/50">
            <span className="text-muted-foreground">GC Runs</span>
            <span className="font-mono text-foreground">
              {gcRuns.toLocaleString()}
            </span>
          </div>
          <div className="flex justify-between py-1.5 border-b border-border/50">
            <span className="text-muted-foreground">GC Avg Duration</span>
            <span className="font-mono text-foreground">
              {formatDuration(gcAvg)}
            </span>
          </div>
        </div>
      </Section>

      {/* Uptime Footer */}
      <div className="text-center text-xs text-muted-foreground pb-2">
        <Clock className="w-3 h-3 inline mr-1" />
        Uptime: {formatUptime(uptime)}
      </div>
    </div>
  );
}
