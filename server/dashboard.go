package server

import (
	"context"
	"dynamodb-sage/internal/llm"
	"dynamodb-sage/internal/notification"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type chatRequest struct {
	Message string `json:"message"`
}

type chatResponse struct {
	Response string `json:"response"`
}

const historyLimit = 10

var extContentType = map[string]string{
	".js":   "application/javascript",
	".css":  "text/css",
	".html": "text/html; charset=utf-8",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".json": "application/json",
}

//go:embed static/*
var dashboardFS embed.FS

func staticFileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		staticFS, err := fs.Sub(dashboardFS, "static")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(staticFS, name)
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		isJS := strings.HasSuffix(name, ".js")
		isCSS := strings.HasSuffix(name, ".css")
		isHTML := strings.HasSuffix(name, ".html")

		if isJS {
			w.Header().Set("Content-Type", "application/javascript")
		} else if isCSS {
			w.Header().Set("Content-Type", "text/css")
		} else if isHTML {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}

		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

func (srv *Server) metricsProxy(w http.ResponseWriter, r *http.Request) {
	host := "localhost" + srv.metricsAddr
	resp, err := http.Get("http://" + host + "/metrics")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// send notification to all connected SSE clients
func (srv *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan notification.NotificationPayload, 10)
	srv.sseClients.Store(r.Context(), ch)
	defer func() {
		srv.sseClients.Delete(r.Context())
		close(ch)
	}()
	for {
		select {
		case n, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(n)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (srv *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		json.NewEncoder(w).Encode(chatResponse{Response: "Please enter a message."})
		return
	}
	now := time.Now().Unix()
	srv.store.AddChatHistory("user", "", msg, now)
	tokenChan := make(chan string, 64)
	go func() {
		defer close(tokenChan)
		if err := srv.generateChat(msg, tokenChan); err != nil {
			log.Printf("LLM error: %v", err)
		}
	}()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	var b strings.Builder
	for token := range tokenChan {
		b.WriteString(token)
		for _, line := range strings.Split(token, "\n") {
			fmt.Fprintf(w, "data: %s\n", line)
		}
		fmt.Fprintf(w, "\n")
		flusher.Flush()
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	resp := b.String()
	now = time.Now().Unix()
	srv.store.AddChatHistory("assistant", "", resp, now)
}

func (srv *Server) generateChat(msg string, tokenChan chan string) error {
	// Retrieve last 10 messages from store (includes the user message just added in handleChat)
	llmMessages := srv.buildChatHistory(msg)
	err := srv.generateChatResponse(srv.mcpCtx, llmMessages, tokenChan)
	if err != nil {
		return fmt.Errorf("LLM temporarily unavailable - use tools instead or /help for commands: %w", err)
	}
	return nil
}

func (srv *Server) buildChatHistory(msg string) []llm.Message {
	storedChatHistory, err := srv.store.GetChatHistory(historyLimit)
	if err != nil {
		log.Printf("Warning: Failed to get chat history, fallback to no history: %v", err)
		return []llm.Message{
			{Role: "user", Content: msg},
		}
	}
	var llmMessages []llm.Message
	if len(storedChatHistory) > 0 {
		// Reverse history to keep chronological order
		for i := len(storedChatHistory) - 1; i >= 0; i-- {
			llmMessages = append(llmMessages, llm.Message{
				Role:    storedChatHistory[i].User,
				Content: storedChatHistory[i].Content,
			})
		}
	} else {
		// Fallback to just the current message if no history found
		llmMessages = []llm.Message{
			{Role: "user", Content: msg},
		}
	}
	return llmMessages
}

/*func (srv *Server) generateChatResponse_back(msg string) string {
	lower := strings.ToLower(msg)

	switch {
	case lower == "/help" || lower == "/tools" || lower == "help" || lower == "what can you do":
		return srv.formatToolList()

	case strings.Contains(lower, "table") && strings.Contains(lower, "list"):
		return "Use **list_tables** in the Tools tab to list all your DynamoDB tables.\n\nSwitch to the **Tools** tab, select `list_tables`, and click Send (no arguments needed)."

	case strings.Contains(lower, "table") && (strings.Contains(lower, "describe") || strings.Contains(lower, "info")):
		return "Use **describe_table** in the Tools tab to get details about a specific table.\n\nExample arguments:\n```json\n{\"tableName\": \"your-table-name\"}\n```"

	case strings.Contains(lower, "table") && (strings.Contains(lower, "create") || strings.Contains(lower, "new")):
		return "Use **create_optimized_table** in the Tools tab to create a new DynamoDB table.\n\nIt supports GSIs, LSIs, tags, and provisioned or on-demand billing."

	case strings.Contains(lower, "table") && (strings.Contains(lower, "delete") || strings.Contains(lower, "remove")):
		return "Use **delete_table** in the Tools tab to delete a table.\n\n**Warning:** Deleting a table is irreversible. Make sure you have backups!"

	case strings.Contains(lower, "item") && (strings.Contains(lower, "get") || strings.Contains(lower, "find") || strings.Contains(lower, "read")):
		return "Use **get_item** in the Tools tab to retrieve an item by its primary key, or **query_table** for more complex lookups."

	case strings.Contains(lower, "item") && (strings.Contains(lower, "put") || strings.Contains(lower, "add") || strings.Contains(lower, "insert") || strings.Contains(lower, "create")):
		return "Use **put_item** in the Tools tab to insert or overwrite an item.\n\nFor multiple items, use **batch_put_items**."

	case strings.Contains(lower, "item") && (strings.Contains(lower, "update") || strings.Contains(lower, "change")):
		return "Use **update_item** in the Tools tab to modify an existing item.\n\nYou'll need the table name, key, and an update expression."

	case strings.Contains(lower, "item") && (strings.Contains(lower, "delete") || strings.Contains(lower, "remove")):
		return "Use **delete_item** in the Tools tab to remove an item by its primary key."

	case strings.Contains(lower, "scan"):
		return "Use **scan_table** in the Tools tab.\n\n⚠️ **Scanning is expensive** – it reads the entire table. Prefer **query_table** when you know the partition key."

	case strings.Contains(lower, "query"):
		return "Use **query_table** in the Tools tab.\n\nYou need:\n- `tableName` (required)\n- `keyConditionExpression` (required, e.g. `#pk = :pkVal`)\n- `expressionAttributeValues` (required)\n- `expressionAttributeNames` (optional but recommended)"

	case strings.Contains(lower, "audit") || strings.Contains(lower, "log"):
		return "Use **read_audit_logs** in the Tools tab to see recent DynamoDB operations with timestamps, tables, and capacity consumed."

	case strings.Contains(lower, "job") || strings.Contains(lower, "async"):
		return "Use **get_job_result** in the Tools tab to check the status of async operations (table creation, batch operations)."

	case strings.Contains(lower, "ttl") || strings.Contains(lower, "time to live"):
		return "Use **update_table_ttl** in the Tools tab to enable or disable TTL on a table."
	default:
		if srv.rateLimiter != nil && !srv.rateLimiter.Allow() {
			log.Printf("LLM rate limit reached")
			return "LLM temporarily unavailable due to rate limiting - please try again later or use tools instead:\n\n" + srv.formatToolList()
		}

		// Retrieve last 10 messages from store (includes the user message just added in handleChat)
		storedChatHistory, err := srv.store.GetChatHistory(historyLimit)
		var llmMessages []llm.Message
		if err == nil && len(storedChatHistory) > 0 {
			// Reverse history to keep chronological order
			for i := len(storedChatHistory) - 1; i >= 0; i-- {
				llmMessages = append(llmMessages, llm.Message{
					Role:    storedChatHistory[i].User,
					Content: storedChatHistory[i].Content,
				})
			}
		} else {
			// Fallback to just the current message if retrieving history fails
			llmMessages = []llm.Message{
				{Role: "user", Content: msg},
			}
		}
		tokenChan := make(chan string)

		err = srv.generateChatResponse(srv.mcpCtx, llmMessages, tokenChan)
		if err != nil {
			log.Printf("LLM error: %v", err)
			return "LLM temporarily unavailable - use tools instead or /help for commands"
		}
		return "resp"
	}
}
*/

func (srv *Server) formatToolList() string {
	var b strings.Builder
	b.WriteString("Here are the available DynamoDB operations:\n\n")
	for _, t := range srv.toolList {
		b.WriteString("• `" + t.Name + "` — " + t.Description + "\n")
	}
	b.WriteString("\nSwitch to the **Tools** tab, select an operation, fill in the arguments as JSON (an example is pre-filled for you), and click Send.")
	return b.String()
}

func (srv *Server) generateChatResponse(ctx context.Context, messages []llm.Message, tokenChan chan string) error {
	if srv.llm == nil {
		return fmt.Errorf("LLM not initialized")
	}
	const maxIterations = 10
	for i := 0; i < maxIterations; i++ {
		toolCalls, err := srv.llm.Generate(ctx, srv.llm.LoadSystemPrompt(), messages, srv.toolDefs, tokenChan)
		if err != nil {
			log.Printf("LLM error: %v", err)
			return err
		}
		if len(toolCalls) == 0 {
			return nil
		}

		messages = append(messages, llm.Message{
			Role:    "assistant",
			ToolCalls: toolCalls,
		})
		toolResults := fetchToolResults(ctx, toolCalls, srv)
		messages = append(messages, llm.Message{
			Role:    "user",
			ToolResults: toolResults,
		})
	}
	return nil
}

func fetchToolResults(ctx context.Context, toolCalls []llm.ToolCall, srv *Server) []llm.ToolResult {
	toolResults := make([]llm.ToolResult, 0, len(toolCalls))
	for _, tc := range toolCalls {
		tOutput, err := srv.executeToolByName(ctx, tc.Name, tc.Arguments)
		if err != nil {
			tOutput = fmt.Sprintf("Error: %v", err)
		}
		toolResults = append(toolResults, llm.ToolResult{
			ToolCallID:  tc.ID,
			DisplayName: tc.Name,
			Result:      tOutput,
			IsError:     err != nil,
		})
	}
	return toolResults
}

func (srv *Server) executeToolByName(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if args == nil {
		args = json.RawMessage("{}")
	}

	// Route chat/agentic tool calls through the same guardrail + risk-analysis
	// pipeline as the MCP path (instrumentMCP + withRiskAnalysis). Mutating and
	// heavy ops are risk-analyzed; read-only ops are instrumented only. This
	// makes the Claude-driven path go through the guardrail just like MCP.
	switch name {
	case "list_tables":
		return runGuardedTool(srv,ctx, name, args, false, srv.listTables)
	case "describe_table":
		return runGuardedTool(srv,ctx, name, args, false, srv.describeTable)
	case "scan_table":
		return runGuardedTool(srv,ctx, name, args, true, srv.scanTable)
	case "query_table":
		return runGuardedTool(srv,ctx, name, args, false, srv.queryTable)
	case "put_item":
		return runGuardedTool(srv,ctx, name, args, true, srv.putItem)
	case "get_item":
		return runGuardedTool(srv,ctx, name, args, false, srv.getItem)
	case "update_item":
		return runGuardedTool(srv,ctx, name, args, true, srv.updateItem)
	case "delete_item":
		return runGuardedTool(srv,ctx, name, args, true, srv.deleteItem)
	case "batch_get_items":
		return runGuardedTool(srv,ctx, name, args, true, srv.batchGetItems)
	case "batch_put_items":
		return runGuardedTool(srv,ctx, name, args, true, srv.batchPutItems)
	case "batch_delete_items":
		return runGuardedTool(srv,ctx, name, args, true, srv.batchDeleteItems)
	case "create_optimized_table":
		return runGuardedTool(srv,ctx, name, args, true, srv.createOptimizedTable)
	case "delete_table":
		return runGuardedTool(srv,ctx, name, args, true, srv.deleteTable)
	case "update_table":
		return runGuardedTool(srv,ctx, name, args, true, srv.updateTable)
	case "update_table_ttl":
		return runGuardedTool(srv,ctx, name, args, false, srv.updateTableTTL)
	case "read_audit_logs":
		return runGuardedTool(srv,ctx, name, args, false, srv.readAuditLogs)
	case "get_job_result":
		return runGuardedTool(srv,ctx, name, args, false, srv.getJobResult)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func runGuardedTool[In any](srv *Server, ctx context.Context, name string, args json.RawMessage, risk bool, call func(ctx context.Context, req *mcp.CallToolRequest, input *In) (*mcp.CallToolResult, any, error)) (string, error) {
	var input In
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid arguments for %s: %v", name, err)
	}
	req := &mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	wrapped := mcp.ToolHandlerFor[In, any](func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		return call(ctx, req, &in)
	})
	var h mcp.ToolHandlerFor[In, any] = wrapped
	if risk {
		h = withRiskAnalysis(srv, wrapped)
	}
	result, _, err := instrumentMCP(srv, name, h)(ctx, req, input)
	return srv.formatToolResult(result, err), nil
}

func (srv *Server) formatToolResult(result *mcp.CallToolResult, err error) string {
	if err != nil {
		return fmt.Sprintf("Error executing tool: %v", err)
	}
	if result == nil {
		return "No result"
	}
	var textParts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			textParts = append(textParts, tc.Text)
		}
	}
	if len(textParts) == 0 {
		return "No content available."
	}
	return strings.Join(textParts, "\n")
}

func (srv *Server) prometheusServer() {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(srv.metricsAddr, metricsMux)
}
