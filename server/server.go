package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	db *dynamodb.Client
	s  *mcp.Server
}

type ListTablesArgs struct {
}

type DescribeTableArgs struct {
	TableName string `json:"tableName"`
}

type ScanTableArgs struct {
	TableName string `json:"tableName"`
}

func New(db *dynamodb.Client) *Server {

	s := mcp.NewServer(&mcp.Implementation{
		Name:    "dynamo-sage",
		Version: "1.0.0",
	}, nil)

	srv := &Server{
		db: db,
		s:  s,
	}
	addTools(s, srv)

	return srv
}

func (srv *Server) SSEHandler() http.Handler {
	sseHandler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return srv.s
	}, nil)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set up a single route with CORS support for the inspector
		// Allow the MCP Inspector (or any origin) to connect
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		sseHandler.ServeHTTP(w, r)
	})
}

func addTools(s *mcp.Server, srv *Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_tables",
		Description: "List all DynamoDB tables",
		InputSchema: map[string]any{
			"type": "object",
		},
	}, srv.listTables)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "describe_table",
		Description: "Get details about a DynamoDB table schema, indexes, and status",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to describe",
				},
			},
			"required": []string{"tableName"},
		},
	}, srv.describeTable)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "scan_table",
		Description: "Read items from a DynamoDB table (returns up to 20 items)",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tableName": map[string]any{
					"type":        "string",
					"description": "The name of the table to scan",
				},
			},
			"required": []string{"tableName"},
		},
	}, srv.scanTable)
}

func (srv *Server) listTables(ctx context.Context, req *mcp.CallToolRequest, args *ListTablesArgs) (*mcp.CallToolResult, any, error) {
	out, err := srv.db.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when listing tables: %v", err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	tables := strings.Join(out.TableNames, ", ")
	if tables == "" {
		tables = "(no tables found)"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("DynamoDB Tables: %s", tables),
			},
		},
	}, nil, nil
}
func (srv *Server) describeTable(ctx context.Context, req *mcp.CallToolRequest, args *DescribeTableArgs) (*mcp.CallToolResult, any, error) {
	out, err := srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &args.TableName,
	})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when describing table %s: %v", args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	// Format the output in a readable way
	details := fmt.Sprintf("Table: %s\nStatus: %s\nItem Count: %d\nSize (Bytes): %d\n",
		*out.Table.TableName, out.Table.TableStatus, out.Table.ItemCount, out.Table.TableSizeBytes)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: details,
			},
		},
	}, nil, nil
}

func (srv *Server) scanTable(ctx context.Context, req *mcp.CallToolRequest, args *ScanTableArgs) (*mcp.CallToolResult, any, error) {
	// Limit scan to 20 items to avoid token overflow in the AI client
	limit := int32(20)
	out, err := srv.db.Scan(ctx, &dynamodb.ScanInput{
		TableName: &args.TableName,
		Limit:     &limit,
	})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error when scanning table %s: %v", args.TableName, err),
				},
			},
			IsError: true,
		}, nil, nil
	}

	// For a simple text representation of the items
	itemsText := fmt.Sprintf("Scanned %d items from table %s:\n", len(out.Items), args.TableName)
	for i, item := range out.Items {
		itemsText += fmt.Sprintf("[%d] %v\n", i+1, item)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: itemsText,
			},
		},
	}, nil, nil
}
