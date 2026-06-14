package unils

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func GetArg[T any](args []json.RawMessage, idx int) (T, error) {
	var arg T
	if idx >= len(args) || idx < 0 {
		return arg, fmt.Errorf("error: arguments are missing at index %d", idx)
	}

	if err := json.Unmarshal(args[idx], &arg); err != nil {
		return arg, fmt.Errorf("error: failed to unmarshal arguments at index %d: %v", idx, err)
	}
	return arg, nil
}

func ErrorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: msg,
			},
		},
	}
}

func SuccessResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: msg,
			},
		},
	}
}