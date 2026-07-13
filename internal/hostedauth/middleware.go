package hostedauth

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return v.Require("")(next)
}

func (v *Verifier) Require(permission string) func(http.Handler) http.Handler {
	return v.RequireFor(func(*http.Request) string { return permission })
}

func (v *Verifier) RequireFor(required func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) == "" {
				unauthorized(w)
				return
			}
			identity, err := v.Verify(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
			if err != nil {
				unauthorized(w)
				return
			}
			permission := required(r)
			if permission != "" && !identity.HasPermission(permission) {
				forbidden(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

func (i Identity) HasPermission(permission string) bool {
	for _, granted := range i.Permissions {
		if granted == permission {
			return true
		}
	}
	return false
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized", "code": "workbench_unauthorized"})
}

func forbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden", "code": "workbench_forbidden"})
}
