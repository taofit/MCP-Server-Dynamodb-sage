"use client";

import { useState, useRef, useEffect } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Send, Loader2, Trash2 } from "lucide-react";
import { useChatStore } from "@/store/chat";

const suggestedPrompts = [
  "List all my DynamoDB tables",
  "Show me the schema of the users table",
  "Query orders where userId = 123",
  "How many items are in each table?",
];

function extractPipeCells(line: string): string[] {
  const cells = line.split("|").map(c => c.trim());
  if (cells[0] === "") cells.shift();
  if (cells.length > 0 && cells[cells.length - 1] === "") cells.pop();
  return cells;
}

function extractTabCells(line: string, preserveEmpty = false): string[] {
  const cells = line.split("\t").map(c => c.trim());
  if (preserveEmpty) return cells;
  return cells.filter(c => c !== "");
}

function isSeparatorOnly(line: string): boolean {
  const t = line.trim();
  if (t.includes("|")) {
    const cells = t.split("|").map(c => c.trim()).filter(c => c);
    return cells.length >= 2 && cells.every(c => /^-+$/.test(c));
  }
  if (t.includes("\t")) {
    const cells = t.split("\t").map(c => c.trim()).filter(c => c);
    return cells.length >= 2 && cells.every(c => /^-+$/.test(c));
  }
  return /^-{3,}$/.test(t);
}

function buildTable(colCount: number, header: string[], dataCells: string[]): string[] {
  const rows: string[] = [];
  rows.push("| " + header.join(" | ") + " |");
  rows.push("| " + header.map(() => "---").join(" | ") + " |");
  for (let i = 0; i < dataCells.length; i += colCount) {
    const row = dataCells.slice(i, i + colCount);
    while (row.length < colCount) row.push("");
    if (row.length === colCount && row.some(c => c !== "")) {
      rows.push("| " + row.join(" | ") + " |");
    }
  }
  return rows;
}

function flattenTabLines(lines: string[]): string[][] {
  const allCells: string[] = [];
  for (const line of lines) {
    const t = line.trim();
    if (!t || !t.includes("\t")) continue;
    const cells = t.split("\t");
    for (const c of cells) allCells.push(c.trim());
  }
  if (allCells.length < 4) return [];
  let colCount = 0;
  for (let n = 2; n <= Math.min(20, Math.floor(allCells.length / 2)); n++) {
    if (allCells.length % n === 0) { colCount = n; break; }
  }
  if (colCount === 0) colCount = Math.floor(Math.sqrt(allCells.length));
  const rows: string[][] = [];
  for (let i = 0; i < allCells.length; i += colCount) {
    const row = allCells.slice(i, i + colCount);
    if (row.length === colCount) rows.push(row);
  }
  return rows;
}

function findJsonArray(text: string): string | null {
  const start = text.indexOf("[");
  if (start === -1) return null;
  let depth = 0, inString = false, escape = false;
  for (let i = start; i < text.length; i++) {
    const ch = text[i];
    if (escape) { escape = false; continue; }
    if (ch === "\\") { escape = true; continue; }
    if (ch === '"') { inString = !inString; continue; }
    if (inString) continue;
    if (ch === "[") depth++;
    if (ch === "]") { depth--; if (depth === 0) return text.substring(start, i + 1); }
  }
  return null;
}

function tryParseJsonArray(text: string): string[][] | null {
  let raw = text;
  raw = raw.replace(/\\n/g, " ").replace(/\\r/g, " ");
  raw = raw.replace(/[\r\n]+/g, " ");
  raw = raw.replace(/\s+/g, " ").trim();

  const jsonStr = findJsonArray(raw);
  if (!jsonStr) return null;

  try {
    const arr = JSON.parse(jsonStr);
    if (Array.isArray(arr) && arr.length > 0 && typeof arr[0] === "object") {
      const keysSet = new Set<string>();
      for (const item of arr) {
        if (item && typeof item === "object") {
          for (const k of Object.keys(item)) keysSet.add(k);
        }
      }
      const keys = Array.from(keysSet);
      if (keys.length >= 2) {
        const rows: string[][] = [keys];
        for (const item of arr) {
          rows.push(keys.map(k => String(item[k] ?? "")));
        }
        return rows;
      }
    }
  } catch {}
  return null;
}

function JsonTable({ text, className }: { text: string; className?: string }) {
  const rows = tryParseJsonArray(text);
  if (rows) {
    const header = rows[0];
    return (
      <table className="json-table">
        <thead>
          <tr>{header.map((h, i) => <th key={i}>{h}</th>)}</tr>
        </thead>
        <tbody>
          {rows.slice(1).map((row, ri) => (
            <tr key={ri}>{row.map((cell, ci) => <td key={ci}>{cell}</td>)}</tr>
          ))}
        </tbody>
      </table>
    );
  }
  return <code className={className}>{text}</code>;
}

function fixMarkdownTables(text: string): string {
  let result = text;

  const lines = result.split("\n");
  const out: string[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    // --- Single-line flattened pipe table (header|data|data|... on one line) ---
    if (trimmed.includes("|") && !trimmed.includes("\t")) {
      const cells = extractPipeCells(trimmed);
      if (cells.length >= 4) {
        // Case 1: has --- separator cells
        let sepIdx = -1;
        let colCount = 0;
        for (let s = 0; s < cells.length; s++) {
          if (/^-+$/.test(cells[s])) {
            let count = 0;
            for (let j = s; j < cells.length; j++) {
              if (/^-+$/.test(cells[j])) count++;
              else break;
            }
            if (count >= 2) { sepIdx = s; colCount = count; break; }
          }
        }
        if (sepIdx !== -1 && colCount >= 2) {
          const header = cells.slice(0, sepIdx).filter(c => c !== "").slice(-colCount);
          const data = cells.slice(sepIdx + colCount).filter(c => c !== "");
          out.push(...buildTable(colCount, header, data));
          i++;
          continue;
        }

        // Case 2: empty cells as row separators (| | | pattern)
        const firstEmpty = cells.findIndex(c => c === "");
        if (firstEmpty > 0) {
          const header = cells.slice(0, firstEmpty);
          const colCount2 = header.length;
          if (colCount2 >= 2) {
            const data = cells.slice(firstEmpty);
            out.push(...buildTable(colCount2, header, data));
            i++;
            continue;
          }
        }
      }
    }

    // --- Multi-line table: collect consecutive pipe-delimited lines ---
    if (trimmed.includes("|") && !trimmed.includes("\t")) {
      const pipeGroup: string[] = [];
      let j = i;
      while (j < lines.length) {
        const t = lines[j].trim();
        if (t.includes("|") && !t.includes("\t")) {
          pipeGroup.push(t);
          j++;
        } else break;
      }
      if (pipeGroup.length >= 2) {
        const allCells: string[][] = [];
        for (const pl of pipeGroup) {
          const c = extractPipeCells(pl);
          if (c.length >= 2 && !isSeparatorOnly(pl)) allCells.push(c);
        }
        if (allCells.length >= 2) {
          const header = allCells[0];
          const colCount = header.length;
          if (colCount >= 2) {
            const dataCells: string[] = [];
            for (let r = 1; r < allCells.length; r++) dataCells.push(...allCells[r]);
            out.push(...buildTable(colCount, header, dataCells));
            i = j;
            continue;
          }
        }
      }
    }

    // --- Tab-separated data ---
    if (trimmed.includes("\t") && !trimmed.includes("|")) {
      const tabGroup: string[] = [];
      let j = i;
      while (j < lines.length) {
        const t = lines[j].trim();
        if (t.includes("\t") && t.length > 0) {
          tabGroup.push(lines[j]);
          j++;
        } else break;
      }
      if (tabGroup.length >= 2) {
        const rows = flattenTabLines(tabGroup);
        if (rows.length >= 2 && rows[0].length >= 2) {
          const header = rows[0];
          const colCount = header.length;
          const dataCells: string[] = [];
          for (let r = 1; r < rows.length; r++) {
            for (let c = 0; c < colCount; c++) dataCells.push(c < rows[r].length ? rows[r][c] : "");
          }
          out.push(...buildTable(colCount, header, dataCells));
          i = j;
          continue;
        }
      }
    }

    out.push(line);
    i++;
  }

  return out.join("\n");
}

export default function ChatPage() {
  const messages = useChatStore((s) => s.messages);
  const addMessage = useChatStore((s) => s.addMessage);
  const updateMessage = useChatStore((s) => s.updateMessage);
  const clearMessages = useChatStore((s) => s.clearMessages);
  const [input, setInput] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const scrollToBottom = () => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages]);

  const sendMessage = async (content?: string) => {
    const text = content ?? input.trim();
    if (!text || isLoading) return;

    addMessage({ role: "user", content: text });
    setInput("");
    setIsLoading(true);

    const assistantId = addMessage({ role: "assistant", content: "" });

    try {
      const res = await fetch("/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: text }),
      });

      if (!res.ok) throw new Error(`HTTP ${res.status}`);

      const reader = res.body!.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let accumulated = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";

        for (const line of lines) {
          if (line.startsWith("data: ")) {
            const token = line.slice(6).trim();
            if (token === "[DONE]") continue;

            accumulated += token.replace(/\\n/g, "\n");
            updateMessage(assistantId, accumulated);
          }
        }
      }
    } catch (err) {
      updateMessage(
        assistantId,
        `Error: ${err instanceof Error ? err.message : "Unknown error"}`
      );
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="flex flex-col h-full">
      {/* Messages Area */}
      <div className="flex-1 overflow-y-auto p-6 space-y-4 scrollbar-thin">
        {messages.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full space-y-8">
            <div className="text-center space-y-2">
              <h1 className="text-2xl font-bold">DynamoDB Sage</h1>
              <p className="text-muted-foreground text-sm">
                Ask anything about your DynamoDB tables in natural language.
                I can list tables, query data, scan records, create or delete tables, and more — just describe what you need.
              </p>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 max-w-lg w-full">
              {suggestedPrompts.map((prompt) => (
                <button
                  key={prompt}
                  onClick={() => sendMessage(prompt)}
                  className="text-left text-sm px-4 py-3 rounded-xl border border-border bg-card/50 hover:border-border hover:bg-accent/50 transition-colors"
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        )}

        {messages.map((msg) => (
          <div
            key={msg.id}
            className={`flex ${msg.role === "user" ? "justify-end" : "justify-start"}`}
          >
            <div
              className={`max-w-[80%] px-4 py-3 rounded-2xl ${
                msg.role === "user"
                  ? "bg-blue-600 text-white"
                  : "bg-card border border-border"
              }`}
            >
              <div className={`prose prose-sm max-w-none ${msg.role === "assistant" ? "prose-headings:text-foreground prose-p:text-foreground prose-li:text-foreground prose-th:text-foreground prose-td:text-foreground prose-code:text-foreground prose-code:bg-accent/50 prose-strong:text-foreground prose-a:text-blue-500" : "prose-invert"}`}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={{
                  pre({ children }) {
                    const child = children as React.ReactElement;
                    if (child && typeof child === "object" && "props" in child) {
                      const p = child.props as Record<string, unknown>;
                      const cls = typeof p.className === "string" ? p.className : "";
                      if (cls.includes("language-json")) {
                        const txt = typeof p.children === "string" ? p.children : String(p.children ?? "");
                        return <JsonTable text={txt} className={cls} />;
                      }
                    }
                    return <pre>{children}</pre>;
                  }
                }}>
                  {fixMarkdownTables(msg.content)}
                </ReactMarkdown>
              </div>
              <span className={`text-[10px] mt-1 block ${msg.role === "user" ? "text-blue-200" : "text-muted-foreground"}`}>
                {new Date(msg.timestamp).toLocaleTimeString()}
              </span>
            </div>
          </div>
        ))}

        {isLoading && (
          <div className="flex justify-start">
            <div className="bg-card border border-border px-4 py-3 rounded-2xl flex items-center gap-2">
              <Loader2 className="w-4 h-4 animate-spin" />
              <span className="text-sm text-muted-foreground">Thinking...</span>
            </div>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Input Area */}
      <div className="border-t border-border p-4 bg-background/80">
        <div className="flex gap-2 max-w-4xl mx-auto">
          {messages.length > 0 && (
            <button
              onClick={clearMessages}
              className="px-3 rounded-xl border border-border bg-card text-muted-foreground hover:text-foreground hover:border-border transition-colors flex items-center"
              title="Clear chat"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          )}
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                sendMessage();
              }
            }}
            placeholder="Ask anything about your DynamoDB tables..."
            rows={1}
            className="flex-1 bg-card border border-border rounded-xl px-5 py-3 focus:outline-none focus:border-blue-500 text-sm placeholder:text-muted-foreground resize-none overflow-y-auto max-h-32"
            style={{ fieldSizing: "content" }}
            disabled={isLoading}
          />
          <button
            onClick={() => sendMessage()}
            disabled={isLoading || !input.trim()}
            className="bg-blue-600 hover:bg-blue-700 disabled:bg-muted text-white px-6 rounded-xl flex items-center justify-center transition-colors"
          >
            {isLoading ? (
              <Loader2 className="w-5 h-5 animate-spin" />
            ) : (
              <Send className="w-5 h-5" />
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
