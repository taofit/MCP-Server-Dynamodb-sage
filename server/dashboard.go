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
			Role:      "assistant",
			ToolCalls: toolCalls,
		})
		toolResults := srv.processToolCalls(toolCalls)
		messages = append(messages, llm.Message{
			Role:        "user",
			ToolResults: toolResults,
		})
	}
	return nil
}

func (srv *Server) processToolCalls(toolCalls []llm.ToolCall) []llm.ToolResult {
	toolResults := make([]llm.ToolResult, 0, len(toolCalls))
	for _, tc := range toolCalls {
		result, err := srv.executeToolByName(srv.mcpCtx, tc.Name, tc.Arguments)
		toolResults = append(toolResults, llm.ToolResult{
			ToolCallID:  tc.ID,
			DisplayName: tc.Name,
			Result:      result,
			IsError:     err != nil,
		})
	}
	return toolResults
}

func (srv *Server) executeToolByName(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if args == nil {
		args = json.RawMessage("{}")
	}
	req := &mcp.CallToolRequest{}
	switch name {
	case "list_tables":
		var a ListTablesArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for list_tables: %v", err)
		}
		result, _, err := srv.listTables(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "describe_table":
		var a DescribeTableArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for describe_table: %v", err)
		}
		result, _, err := srv.describeTable(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "scan_table":
		var a ScanTableArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for scan_table: %v", err)
		}
		result, _, err := srv.scanTable(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "query_table":
		var a QueryTableArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for query_table: %v", err)
		}
		result, _, err := srv.queryTable(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "put_item":
		var a PutItemArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for put_item: %v", err)
		}
		result, _, err := srv.putItem(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "get_item":
		var a GetItemArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for get_item: %v", err)
		}
		result, _, err := srv.getItem(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "update_item":
		var a UpdateItemArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for update_item: %v", err)
		}
		result, _, err := srv.updateItem(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "delete_item":
		var a DeleteItemArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for delete_item: %v", err)
		}
		result, _, err := srv.deleteItem(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "batch_get_items":
		var a BatchGetItemArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for batch_get_items: %v", err)
		}
		result, _, err := srv.batchGetItems(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "batch_put_items":
		var a BatchPutItemsArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for batch_put_items: %v", err)
		}
		result, _, err := srv.batchPutItems(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "batch_delete_items":
		var a BatchDeleteItemsArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for batch_delete_items: %v", err)
		}
		result, _, err := srv.batchDeleteItems(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "create_optimized_table":
		var a CreateOptimizedTableArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for create_optimized_table: %v", err)
		}
		result, _, err := srv.createOptimizedTable(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "delete_table":
		var a DeleteTableArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for delete_table: %v", err)
		}
		result, _, err := srv.deleteTable(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "update_table":
		var a UpdateTableArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for update_table: %v", err)
		}
		result, _, err := srv.updateTable(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "update_table_ttl":
		var a UpdateTableTTLArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for update_table_ttl: %v", err)
		}
		result, _, err := srv.updateTableTTL(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "read_audit_logs":
		var a ReadAuditLogsArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for read_audit_logs: %v", err)
		}
		result, _, err := srv.readAuditLogs(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	case "get_job_result":
		var a GetJobResultArgs
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("Invalid arguments for get_job_result: %v", err)
		}
		result, _, err := srv.getJobResult(ctx, req, &a)
		return srv.formatToolResult(result, err), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
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
