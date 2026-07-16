"use client";

import { useEffect, useState, useMemo } from "react";
import {
  Search,
  Database,
  ChevronRight,
  Key,
  Hash,
  Table2,
  RefreshCw,
  AlertTriangle,
  CheckCircle,
  Clock,
  Columns3,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

// ─── Types ──────────────────────────────────────────────────────────────────

interface TableInfo {
  tableName: string;
  itemCount?: number;
  sizeBytes?: number;
  status?: string;
}

interface KeySchemaEntry {
  attributeName: string;
  keyType: "HASH" | "RANGE";
}

interface AttributeDefinition {
  attributeName: string;
  attributeType: "S" | "N" | "B";
}

interface GSI {
  indexName: string;
  keySchema: KeySchemaEntry[];
  projection?: { projectionType: string };
  itemCount?: number;
  sizeBytes?: number;
}

interface TableDescription {
  tableName: string;
  status: string;
  keySchema: KeySchemaEntry[];
  attributeDefinitions: AttributeDefinition[];
  itemCount: number;
  sizeBytes: number;
  throughput?: { readCapacityUnits: number; writeCapacityUnits: number };
  billingMode?: string;
  gsis: GSI[];
  lsis: GSI[];
  ttlAttribute?: string;
  ttlEnabled?: boolean;
}

type Tab = "schema" | "data" | "stats";

// ─── Helpers ────────────────────────────────────────────────────────────────

const typeLabel: Record<string, string> = {
  S: "String",
  N: "Number",
  B: "Binary",
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function formatNumber(n: number): string {
  return n.toLocaleString();
}

const statusStyles: Record<string, string> = {
  ACTIVE: "text-emerald-500",
  CREATING: "text-amber-500",
  UPDATING: "text-amber-500",
  DELETING: "text-red-500",
  INACCESSIBLE: "text-red-500",
};

// ─── Table List Panel ───────────────────────────────────────────────────────

function TableListPanel({
  tables,
  loading,
  search,
  onSearch,
  selected,
  onSelect,
}: {
  tables: TableInfo[];
  loading: boolean;
  search: string;
  onSearch: (v: string) => void;
  selected: string | null;
  onSelect: (name: string) => void;
}) {
  const filtered = useMemo(
    () =>
      tables.filter((t) =>
        t.tableName.toLowerCase().includes(search.toLowerCase())
      ),
    [tables, search]
  );

  return (
    <div className="flex flex-col h-full">
      {/* Search */}
      <div className="p-3 border-b border-border">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search tables..."
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            className="w-full bg-background border border-border rounded-lg pl-8 pr-3 py-1.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:border-ring"
          />
        </div>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto scrollbar-thin">
        {loading ? (
          <div className="p-3 space-y-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full rounded-lg" />
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <div className="p-6 text-center text-sm text-muted-foreground">
            {tables.length === 0 ? "No tables found" : "No matches"}
          </div>
        ) : (
          <div className="p-2 space-y-0.5">
            {filtered.map((t) => (
              <button
                key={t.tableName}
                onClick={() => onSelect(t.tableName)}
                className={cn(
                  "w-full flex items-center justify-between px-3 py-2 rounded-lg text-sm transition-colors text-left",
                  selected === t.tableName
                    ? "bg-accent text-accent-foreground"
                    : "text-foreground hover:bg-accent/50"
                )}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <Table2 className="w-4 h-4 text-muted-foreground shrink-0" />
                  <span className="truncate font-mono text-xs">
                    {t.tableName}
                  </span>
                </div>
                <ChevronRight className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Footer count */}
      <div className="px-3 py-2 border-t border-border text-xs text-muted-foreground">
        {tables.length} table{tables.length !== 1 ? "s" : ""}
      </div>
    </div>
  );
}

// ─── Schema Tab ─────────────────────────────────────────────────────────────

function SchemaTab({ desc }: { desc: TableDescription }) {
  return (
    <div className="space-y-6">
      {/* Key Schema */}
      <section>
        <h3 className="text-xs text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
          <Key className="w-3.5 h-3.5" />
          Key Schema
        </h3>
        <div className="rounded-xl border border-border bg-card/50 overflow-hidden">
          {desc.keySchema.map((k) => (
            <div
              key={k.attributeName}
              className="flex items-center justify-between px-4 py-2.5 border-b border-border/50 last:border-0"
            >
              <div className="flex items-center gap-2">
                <span className="font-mono text-sm">{k.attributeName}</span>
              </div>
              <span
                className={cn(
                  "text-xs px-2 py-0.5 rounded-full",
                  k.keyType === "HASH"
                    ? "bg-blue-500/10 text-blue-500"
                    : "bg-violet-500/10 text-violet-500"
                )}
              >
                {k.keyType === "HASH" ? "Partition Key" : "Sort Key"}
              </span>
            </div>
          ))}
        </div>
      </section>

      {/* Attribute Definitions */}
      <section>
        <h3 className="text-xs text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
          <Hash className="w-3.5 h-3.5" />
          Attribute Definitions
        </h3>
        <div className="rounded-xl border border-border bg-card/50 overflow-hidden">
          {desc.attributeDefinitions.map((a) => (
            <div
              key={a.attributeName}
              className="flex items-center justify-between px-4 py-2.5 border-b border-border/50 last:border-0"
            >
              <span className="font-mono text-sm">{a.attributeName}</span>
              <span className="text-xs text-muted-foreground">
                {typeLabel[a.attributeType] || a.attributeType}
              </span>
            </div>
          ))}
        </div>
      </section>

      {/* GSIs */}
      {desc.gsis.length > 0 && (
        <section>
          <h3 className="text-xs text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
            <Columns3 className="w-3.5 h-3.5" />
            Global Secondary Indexes ({desc.gsis.length})
          </h3>
          <div className="space-y-3">
            {desc.gsis.map((gsi) => (
              <div
                key={gsi.indexName}
                className="rounded-xl border border-border bg-card/50 p-4"
              >
                <div className="flex items-center justify-between mb-2">
                  <span className="font-mono text-sm font-medium">
                    {gsi.indexName}
                  </span>
                  {gsi.projection && (
                    <span className="text-xs text-muted-foreground">
                      {gsi.projection.projectionType}
                    </span>
                  )}
                </div>
                <div className="space-y-1">
                  {gsi.keySchema.map((k) => (
                    <div
                      key={k.attributeName}
                      className="flex items-center gap-2 text-xs text-muted-foreground"
                    >
                      <span className="font-mono">{k.attributeName}</span>
                      <span className="text-muted-foreground/60">
                        ({k.keyType})
                      </span>
                    </div>
                  ))}
                </div>
                {gsi.itemCount !== undefined && (
                  <p className="text-xs text-muted-foreground mt-2">
                    {formatNumber(gsi.itemCount)} items
                    {gsi.sizeBytes !== undefined &&
                      ` \u00b7 ${formatBytes(gsi.sizeBytes)}`}
                  </p>
                )}
              </div>
            ))}
          </div>
        </section>
      )}

      {/* LSIs */}
      {desc.lsis.length > 0 && (
        <section>
          <h3 className="text-xs text-muted-foreground uppercase tracking-wider mb-3">
            Local Secondary Indexes ({desc.lsis.length})
          </h3>
          <div className="space-y-3">
            {desc.lsis.map((lsi) => (
              <div
                key={lsi.indexName}
                className="rounded-xl border border-border bg-card/50 p-4"
              >
                <span className="font-mono text-sm font-medium">
                  {lsi.indexName}
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

// ─── Data Tab ───────────────────────────────────────────────────────────────

function DataTab({ items, loading }: { items: Record<string, unknown>[]; loading: boolean }) {
  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="text-center text-sm text-muted-foreground py-8">
        No items found in this table
      </div>
    );
  }

  const allKeys = Array.from(
    new Set(items.flatMap((item) => Object.keys(item)))
  );

  return (
    <div className="overflow-x-auto">
      <table className="json-table">
        <thead>
          <tr>
            {allKeys.map((key) => (
              <th key={key}>{key}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {items.map((item, i) => (
            <tr key={i}>
              {allKeys.map((key) => (
                <td key={key}>
                  <span className="font-mono text-xs">
                    {renderCellValue(item[key])}
                  </span>
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function renderCellValue(value: unknown): string {
  if (value === null || value === undefined) return "\u2014";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

// ─── Stats Tab ──────────────────────────────────────────────────────────────

function StatsTab({ desc }: { desc: TableDescription }) {
  const statusStyle = statusStyles[desc.status] || "text-muted-foreground";

  const items = [
    { label: "Status", value: desc.status, className: statusStyle },
    { label: "Item Count", value: formatNumber(desc.itemCount) },
    { label: "Table Size", value: formatBytes(desc.sizeBytes) },
    {
      label: "Billing Mode",
      value: desc.billingMode || "Unknown",
    },
  ];

  if (desc.throughput) {
    items.push(
      {
        label: "Read Capacity",
        value: `${desc.throughput.readCapacityUnits} RCU`,
      },
      {
        label: "Write Capacity",
        value: `${desc.throughput.writeCapacityUnits} WCU`,
      }
    );
  }

  if (desc.ttlEnabled !== undefined) {
    items.push({
      label: "TTL",
      value: desc.ttlEnabled
        ? `Enabled (${desc.ttlAttribute || "unknown attribute"})`
        : "Disabled",
    });
  }

  return (
    <div className="space-y-6">
      <section>
        <h3 className="text-xs text-muted-foreground uppercase tracking-wider mb-3 flex items-center gap-2">
          <Database className="w-3.5 h-3.5" />
          Table Statistics
        </h3>
        <div className="rounded-xl border border-border bg-card/50 overflow-hidden">
          {items.map((item) => (
            <div
              key={item.label}
              className="flex items-center justify-between px-4 py-2.5 border-b border-border/50 last:border-0"
            >
              <span className="text-sm text-muted-foreground">{item.label}</span>
              <span
                className={cn("text-sm font-medium", item.className)}
              >
                {item.value}
              </span>
            </div>
          ))}
        </div>
      </section>

      {/* GSI stats */}
      {desc.gsis.length > 0 && (
        <section>
          <h3 className="text-xs text-muted-foreground uppercase tracking-wider mb-3">
            GSI Statistics
          </h3>
          <div className="rounded-xl border border-border bg-card/50 overflow-hidden">
            <div className="grid grid-cols-4 gap-0 text-xs text-muted-foreground px-4 py-2 border-b border-border">
              <span>Index</span>
              <span>Partition Key</span>
              <span>Items</span>
              <span>Size</span>
            </div>
            {desc.gsis.map((gsi) => (
              <div
                key={gsi.indexName}
                className="grid grid-cols-4 gap-0 px-4 py-2.5 border-b border-border/50 last:border-0 text-sm"
              >
                <span className="font-mono text-xs truncate">
                  {gsi.indexName}
                </span>
                <span className="font-mono text-xs text-muted-foreground">
                  {gsi.keySchema.find((k) => k.keyType === "HASH")
                    ?.attributeName || "\u2014"}
                </span>
                <span className="text-xs">
                  {gsi.itemCount !== undefined
                    ? formatNumber(gsi.itemCount)
                    : "\u2014"}
                </span>
                <span className="text-xs text-muted-foreground">
                  {gsi.sizeBytes !== undefined ? formatBytes(gsi.sizeBytes) : "\u2014"}
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

// ─── Main Page ──────────────────────────────────────────────────────────────

export default function TablesPage() {
  const [tables, setTables] = useState<TableInfo[]>([]);
  const [tablesLoading, setTablesLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [desc, setDesc] = useState<TableDescription | null>(null);
  const [descLoading, setDescLoading] = useState(false);
  const [tab, setTab] = useState<Tab>("schema");
  const [items, setItems] = useState<Record<string, unknown>[]>([]);
  const [itemsLoading, setItemsLoading] = useState(false);

  // Fetch table list
  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const res = await fetch("/api/tables");
        const data: TableInfo[] = await res.json();
        if (!cancelled) setTables(data);
      } catch {
        if (!cancelled) setTables([]);
      } finally {
        if (!cancelled) setTablesLoading(false);
      }
    }
    load();
    return () => { cancelled = true; };
  }, []);

  // Fetch table description when selected
  useEffect(() => {
    if (!selected) return;
    const tableName = selected;
    let cancelled = false;
    async function load() {
      setDescLoading(true);
      try {
        const res = await fetch(`/api/tables/${encodeURIComponent(tableName)}`);
        const data: TableDescription = await res.json();
        if (!cancelled) setDesc(data);
      } catch {
        if (!cancelled) setDesc(null);
      } finally {
        if (!cancelled) setDescLoading(false);
      }
    }
    load();
    return () => { cancelled = true; };
  }, [selected]);

  // Fetch sample items when data tab is active
  useEffect(() => {
    if (!selected || tab !== "data") return;
    const tableName = selected;
    let cancelled = false;
    async function load() {
      setItemsLoading(true);
      try {
        const res = await fetch(`/api/tables/${encodeURIComponent(tableName)}/items?limit=20`);
        const data: Record<string, unknown>[] = await res.json();
        if (!cancelled) setItems(data);
      } catch {
        if (!cancelled) setItems([]);
      } finally {
        if (!cancelled) setItemsLoading(false);
      }
    }
    load();
    return () => { cancelled = true; };
  }, [selected, tab]);

  const handleRefresh = () => {
    fetch("/api/tables")
      .then((r) => r.json())
      .then((data: TableInfo[]) => setTables(data))
      .catch(() => setTables([]));
    if (selected) {
      fetch(`/api/tables/${encodeURIComponent(selected)}`)
        .then((r) => r.json())
        .then((data: TableDescription) => setDesc(data))
        .catch(() => setDesc(null));
    }
  };

  const tabs: { key: Tab; label: string }[] = [
    { key: "schema", label: "Schema" },
    { key: "data", label: "Data" },
    { key: "stats", label: "Stats" },
  ];

  return (
    <div className="flex-1 flex h-[calc(100vh-3.5rem)] overflow-hidden">
      {/* Left Panel - Table List */}
      <div className="w-64 shrink-0 border-r border-border bg-card/30 hidden md:flex flex-col">
        <TableListPanel
          tables={tables}
          loading={tablesLoading}
          search={search}
          onSearch={setSearch}
          selected={selected}
          onSelect={setSelected}
        />
      </div>

      {/* Right Panel - Table Details */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {!selected ? (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center space-y-3">
              <Database className="w-12 h-12 text-muted-foreground/30 mx-auto" />
              <p className="text-sm text-muted-foreground">
                Select a table to view its details
              </p>
            </div>
          </div>
        ) : descLoading ? (
          <div className="p-6 space-y-4">
            <Skeleton className="h-8 w-48" />
            <Skeleton className="h-4 w-72" />
            <div className="space-y-3 mt-6">
          {Array.from({ length: 3 }).map((_, idx) => (
            <Skeleton key={idx} className="h-12 w-full rounded-xl" />
          ))}
            </div>
          </div>
        ) : !desc ? (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center space-y-2">
              <AlertTriangle className="w-8 h-8 text-amber-500 mx-auto" />
              <p className="text-sm text-muted-foreground">
                Failed to load table details
              </p>
            </div>
          </div>
        ) : (
          <>
            {/* Table Header */}
            <div className="px-6 py-4 border-b border-border bg-card/20">
              <div className="flex items-center justify-between">
                <div>
                  <h1 className="text-xl font-bold font-mono">
                    {desc.tableName}
                  </h1>
                  <div className="flex items-center gap-3 mt-1">
                    <span
                      className={cn(
                        "flex items-center gap-1 text-xs",
                        statusStyles[desc.status] || "text-muted-foreground"
                      )}
                    >
                      {desc.status === "ACTIVE" ? (
                        <CheckCircle className="w-3 h-3" />
                      ) : (
                        <Clock className="w-3 h-3" />
                      )}
                      {desc.status}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {formatNumber(desc.itemCount)} items
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {formatBytes(desc.sizeBytes)}
                    </span>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={handleRefresh}
                  className="shrink-0"
                >
                  <RefreshCw className="w-4 h-4" />
                </Button>
              </div>

              {/* Tabs */}
              <div className="flex gap-1 mt-4 bg-muted rounded-lg p-1 w-fit">
                {tabs.map((t) => (
                  <button
                    key={t.key}
                    onClick={() => setTab(t.key)}
                    className={cn(
                      "px-3 py-1.5 rounded-md text-xs font-medium transition-colors",
                      tab === t.key
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground"
                    )}
                  >
                    {t.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Tab Content */}
            <div className="flex-1 overflow-y-auto p-6 scrollbar-thin">
              {tab === "schema" && <SchemaTab desc={desc} />}
              {tab === "data" && <DataTab items={items} loading={itemsLoading} />}
              {tab === "stats" && <StatsTab desc={desc} />}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
