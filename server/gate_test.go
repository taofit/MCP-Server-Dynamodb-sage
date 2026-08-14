package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGateGuestToolCalls(t *testing.T) {
	srv := &Server{}

	post := func(role string, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(body)))
		if role != "" {
			r = r.WithContext(context.WithValue(r.Context(), userRoleContextKey, role))
		}
		w := httptest.NewRecorder()
		allowed := srv.gateGuestToolCalls(w, r)
		if allowed {
			w.Header().Set("X-Passthrough", "true")
		}
		return w
	}

	tests := []struct {
		name    string
		role    string
		body    string
		blocked bool
	}{
		{
			name:    "guest write tool is blocked",
			role:    roleGuest,
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_table","arguments":{}}}`,
			blocked: true,
		},
		{
			name:    "guest read tool passes through",
			role:    roleGuest,
			body:    `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_tables","arguments":{}}}`,
			blocked: false,
		},
		{
			name:    "admin write tool passes through",
			role:    roleAdmin,
			body:    `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_table","arguments":{}}}`,
			blocked: false,
		},
		{
			name:    "missing role is treated as guest and blocked",
			role:    "",
			body:    `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"put_item","arguments":{}}}`,
			blocked: true,
		},
		{
			name:    "batch with a blocked write call is blocked",
			role:    roleGuest,
			body:    `[{"jsonrpc":"2.0","id":5,"method":"tools/list"},{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"update_item","arguments":{}}}]`,
			blocked: true,
		},
		{
			name:    "non-tool JSON-RPC method passes through",
			role:    roleGuest,
			body:    `{"jsonrpc":"2.0","id":7,"method":"initialize"}`,
			blocked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := post(tt.role, tt.body)
			if w.Header().Get("X-Passthrough") == "true" {
				if tt.blocked {
					t.Fatalf("expected call to be blocked, but it passed through")
				}
				return
			}
			if !tt.blocked {
				t.Fatalf("expected call to pass through, but it was blocked")
			}
			if w.Code != http.StatusOK {
				t.Fatalf("blocked response status = %d, want 200", w.Code)
			}
			var resp struct {
				Error struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("invalid JSON-RPC error response: %v", err)
			}
			if resp.Error.Code != -32001 {
				t.Fatalf("error code = %d, want -32001", resp.Error.Code)
			}
		})
	}
}
