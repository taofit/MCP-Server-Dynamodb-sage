package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"dynamodb-sage/internal/llm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type chatRequest struct {
	Message string `json:"message"`
}

type chatResponse struct {
	Response string `json:"response"`
}

const historyLimit = 10

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
			tokenChan <- fmt.Sprintf("LLM error: %v", err)
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
		escaped := strings.ReplaceAll(token, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\n", "\\n")
		fmt.Fprintf(w, "data: %s\n\n", escaped)
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
	case "ingest_document":
		return runGuardedTool(srv,ctx, name, args, true, srv.ingestDocument)
	case "search_collection":
		return runGuardedTool(srv,ctx, name, args, false, srv.searchCollection)
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
	req.Params = &mcp.CallToolParamsRaw{
		Name:      name,
		Arguments: args,
	}
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