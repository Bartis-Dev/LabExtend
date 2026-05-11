package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Bartis-Dev/LabExtend/internal/auth"
)

type ctxKey int

const userCtxKey ctxKey = 1

// requireAuth verifies the labextend_session JWT cookie and injects the
// claims into the request context. Routes wrapped with this middleware
// return 401 if the cookie is missing or the token fails verification.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("labextend_session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		claims, err := auth.Verify(s.JWTSecret, c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// userFromCtx returns the authenticated user's claims, or nil if the
// request did not pass through requireAuth.
func userFromCtx(r *http.Request) *auth.Claims {
	v, _ := r.Context().Value(userCtxKey).(*auth.Claims)
	return v
}

// originCheck blocks non-safe methods whose Origin (or Referer when
// Origin is absent) does not match the request Host. This is the CSRF
// backstop for SameSite=Strict cookies.
func originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Build allowed origins from request host (both http and https).
		host := r.Host
		if origin != "http://"+host && origin != "https://"+host {
			writeError(w, http.StatusForbidden, "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
