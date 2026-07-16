"use client";

import { useEffect, useState } from "react";
import { Copy, Check, Play, Loader2 } from "lucide-react";

interface Tool {
  Name: string;
  Description: string;
}

const exampleArgs: Record<string, string> = {
  list_tables: "{}",
  describe_table: '{\n  "tableName": "Users"\n}',
  query_table: '{\n  "tableName": "Users",\n  "keyConditionExpression": "pk = :pk",\n  "expressionAttributeValues": {\n    ":pk": "user#123"\n  }\n}',
  scan_table: '{\n  "tableName": "Users",\n  "limit": 10\n}',
  get_item: '{\n  "tableName": "Users",\n  "key": {\n    "pk": "user#123"\n  }\n}',
  put_item: '{\n  "tableName": "Users",\n  "item": {\n    "pk": "user#123",\n    "name": "John"\n  }\n}',
  update_item: '{\n  "tableName": "Users",\n  "key": {\n    "pk": "user#123"\n  },\n  "updateExpression": "SET #n = :n",\n  "expressionAttributeNames": {\n    "#n": "name"\n  },\n  "expressionAttributeValues": {\n    ":n": "Jane"\n  }\n}',
  delete_item: '{\n  "tableName": "Users",\n  "key": {\n    "pk": "user#123"\n  }\n}',
  batch_get_items: '{\n  "requests": [\n    {"tableName": "Users", "key": {"pk": "user#1"}},\n    {"tableName": "Users", "key": {"pk": "user#2"}}\n  ]\n}',
  batch_put_items: '{\n  "tableName": "Users",\n  "items": [\n    {"pk": "user#1", "name": "Alice"},\n    {"pk": "user#2", "name": "Bob"}\n  ]\n}',
  batch_delete_items: '{\n  "tableName": "Users",\n  "keys": [\n    {"pk": "user#1"},\n    {"pk": "user#2"}\n  ]\n}',
  create_optimized_table: '{\n  "tableName": "NewTable",\n  "keySchema": [\n    {"attributeName": "pk", "keyType": "HASH"}\n  ],\n  "attributeDefinitions": [\n    {"attributeName": "pk", "attributeType": "S"}\n  ],\n  "billingMode": "PAY_PER_REQUEST"\n}',
  delete_table: '{\n  "tableName": "OldTable"\n}',
  update_table: '{\n  "tableName": "Users",\n  "globalSecondaryIndexUpdates": [\n    {\n      "create": {\n        "indexName": "gsi-email",\n        "keySchema": [{"attributeName": "email", "keyType": "HASH"}],\n        "projection": {"projectionType": "ALL"}\n      }\n    }\n  ]\n}',
  update_table_ttl: '{\n  "tableName": "Users",\n  "ttlEnabled": true\n}',
  read_audit_logs: '{\n  "limit": 20\n}',
  get_job_result: '{\n  "jobId": "your-job-id-here"\n}',
};

export default function ToolsPage() {
  const [tools, setTools] = useState<Tool[]>([]);
  const [selected, setSelected] = useState("");
  const [args, setArgs] = useState("{}");
  const [result, setResult] = useState("");
  const [copied, setCopied] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadingTools, setLoadingTools] = useState(true);

  useEffect(() => {
    fetch("/api/tools")
      .then((r) => r.json())
      .then((data: Tool[]) => {
        setTools(data);
        if (data.length > 0) {
          setSelected(data[0].Name);
          setArgs(exampleArgs[data[0].Name] || "{}");
        }
      })
      .catch(() => setTools([]))
      .finally(() => setLoadingTools(false));
  }, []);

  const copyResult = () => {
    navigator.clipboard.writeText(result);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const executeTool = async () => {
    if (!selected) return;
    setLoading(true);
    setResult("");
    try {
      const parsedArgs = JSON.parse(args);
      const response = await fetch("/mcp", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: 1,
          method: "tools/call",
          params: {
            name: selected,
            arguments: parsedArgs,
          },
        }),
      });
      const data = await response.json();
      if (data.result?.content) {
        const textParts = data.result.content
          .filter((c: { type: string }) => c.type === "text")
          .map((c: { type: string; text: string }) => c.text);
        setResult(textParts.join("\n") || "No content in response");
      } else if (data.error) {
        setResult(`Error: ${data.error.message || JSON.stringify(data.error)}`);
      } else {
        setResult(JSON.stringify(data, null, 2));
      }
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e);
      setResult(`Error: ${message}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex-1 p-6 max-w-5xl mx-auto w-full space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold">Tools</h1>
        <p className="text-muted-foreground text-sm mt-1">
          Manual MCP tool playground for debugging.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Tool Selector */}
        <div className="space-y-2">
          <label className="text-xs text-muted-foreground uppercase tracking-wider">
            Select Tool
          </label>
          <div className="space-y-1 max-h-[calc(100vh-280px)] overflow-y-auto pr-1 scrollbar-thin">
            {loadingTools ? (
              <div className="text-muted-foreground text-sm py-4">Loading tools...</div>
            ) : tools.length === 0 ? (
              <div className="text-muted-foreground text-sm py-4">No tools available</div>
            ) : (
              tools.map((tool) => (
                <button
                  key={tool.Name}
                  onClick={() => {
                    setSelected(tool.Name);
                    setArgs(exampleArgs[tool.Name] || "{}");
                    setResult("");
                  }}
                  className={`w-full text-left px-3 py-2.5 rounded-lg text-sm transition-colors ${
                    selected === tool.Name
                      ? "bg-accent text-foreground"
                      : "text-foreground/80 hover:bg-accent/50 hover:text-foreground"
                  }`}
                >
                  <span className="font-mono text-sm">{tool.Name}</span>
                  <span className="block text-xs text-muted-foreground mt-0.5">
                    {tool.Description}
                  </span>
                </button>
              ))
            )}
          </div>
        </div>

        {/* Argument Editor + Result */}
        <div className="lg:col-span-2 space-y-4">
          {/* Arguments */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-xs text-muted-foreground uppercase tracking-wider">
                Arguments (JSON)
              </label>
              <button
                onClick={() => setArgs(exampleArgs[selected] || "{}")}
                className="text-xs text-blue-500 hover:text-blue-400"
              >
                Reset to example
              </button>
            </div>
            <textarea
              value={args}
              onChange={(e) => setArgs(e.target.value)}
              className="w-full h-48 bg-card border border-border rounded-xl px-4 py-3 font-mono text-sm focus:outline-none focus:border-ring resize-none"
              spellCheck={false}
            />
          </div>

          {/* Execute Button */}
          <button
            onClick={executeTool}
            disabled={loading || !selected}
            className="flex items-center gap-2 bg-blue-600 hover:bg-blue-700 disabled:bg-muted disabled:cursor-not-allowed text-white px-5 py-2.5 rounded-xl text-sm font-medium transition-colors"
          >
            {loading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Play className="w-4 h-4" />
            )}
            {loading ? "Executing..." : "Execute"}
          </button>

          {/* Result */}
          {result && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <label className="text-xs text-muted-foreground uppercase tracking-wider">
                  Result
                </label>
                <button
                  onClick={copyResult}
                  className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  {copied ? (
                    <Check className="w-3 h-3" />
                  ) : (
                    <Copy className="w-3 h-3" />
                  )}
                  {copied ? "Copied" : "Copy"}
                </button>
              </div>
              <pre className="bg-card border border-border rounded-xl px-4 py-3 font-mono text-sm whitespace-pre-wrap break-words max-h-96 overflow-y-auto scrollbar-thin">
                {result}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
