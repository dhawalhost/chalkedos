package http

import (
	"net/http"
	"slices"

	"github.com/dhawalhost/chalkedos/internal/middleware"
)

// requireRoles returns the caller's claims if their role is one of the
// allowed set, or writes the response and returns nil otherwise. A wrong
// role gets 404, not 403 — role membership is tenant-internal information,
// same leak rule as cross-school access (see the API reference's global
// conventions).
func requireRoles(w http.ResponseWriter, r *http.Request, roles ...string) *middleware.Claims {
	claims, ok := middleware.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "MISSING_CLAIMS", "Authentication required.")
		return nil
	}
	if !slices.Contains(roles, claims.Role) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
		return nil
	}
	return claims
}
