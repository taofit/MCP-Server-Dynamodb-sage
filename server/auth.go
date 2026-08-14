package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

type contextKey string

const (
	roleGuest = "guest"
	roleAdmin = "admin"
)
const userRoleContextKey = contextKey("userRole")

func (srv *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	role, ok := srv.tokenRole(r)
	if !ok {
		http.Error(w, `{"error":"Unauthorized: Invalid token"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"role": role})
}

func (srv *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		role, ok := srv.tokenRole(r)
		if !ok {
			http.Error(w, `{"error":"Unauthorized: Invalid token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userRoleContextKey, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (srv *Server) tokenRole(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		if r.URL.Path == "/api/events" {
			token = r.URL.Query().Get("token")
		}
	}
	if strings.TrimSpace(token) == "" {
		return "", false
	}
	guestKey := os.Getenv("GUEST_KEY")
	adminKey := os.Getenv("ADMIN_KEY")
	if guestKey != "" && guestKey == token {
		return roleGuest, true
	}
	if adminKey != "" && adminKey == token {
		return roleAdmin, true
	}
	return "", false
}

func (srv *Server) demoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	guestKey := os.Getenv("GUEST_KEY")
	if guestKey == "" {
		http.Error(w, "Demo mode not enabled", http.StatusNotImplemented)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"role": roleGuest, "token": guestKey})
}

func isPublicRequest(r *http.Request) bool {
	if skipTokenCheck() {
		return true
	}
	if r.Method == http.MethodOptions ||
		r.URL.Path == "/health" ||
		r.URL.Path == "/api/login" ||
		r.URL.Path == "/api/demo" {
		return true
	}
	// Static assets and SPA pages are public: browsers cannot send an
	// Authorization header on page loads or asset requests.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		p := r.URL.Path
		if strings.HasPrefix(p, "/_next/") ||
			strings.HasPrefix(p, "/static/") ||
			p == "/" ||
			p == "/login" ||
			p == "/favicon.ico" ||
			p == "/icon.svg" {
			return true
		}
		// Any other non-API GET/HEAD is a client-side route served by the SPA fallback.
		if !strings.HasPrefix(p, "/api/") && p != "/sse" && p != "/mcp" {
			return true
		}
	}
	return false
}

func skipTokenCheck() bool {
	return os.Getenv("SKIP_TOKEN_CHECK") == "true"
}

func getUserRoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(userRoleContextKey).(string)
	return role, ok
}
