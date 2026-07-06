// Package middleware provides Chi middleware for authentication,
// request-scoped RLS enforcement, and structured logging.
//
// Auth flow (see Technical Architecture, Section 02):
//  1. Verify the Supabase-issued JWT's signature and expiry.
//  2. Extract school_id and role claims.
//  3. Reject if the JWT's school_id doesn't match the :schoolSlug in the
//     URL — this happens BEFORE any handler or database query runs.
//  4. Stash the claims on the request context so handlers (and the DB
//     layer, via db.WithRLSClaims) can use them.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

// allowedSigningMethods restricts JWT verification to asymmetric algorithms.
// Supabase issues ES256 today, but RS256/PS256 are accepted too in case the
// project's key type changes. HS*/none are explicitly excluded — without
// this, a JWKS-based keyfunc plus a permissive parser is vulnerable to the
// classic alg-confusion attack (an attacker sets alg=HS256 in the token
// header and signs with the public key bytes as if they were an HMAC
// secret). golang-jwt already type-checks the key it gets back from the
// keyfunc against the alg, but this makes the restriction explicit rather
// than relying on that as the only line of defense.
var allowedSigningMethods = []string{"ES256", "RS256", "PS256"}

type contextKey string

const claimsContextKey contextKey = "chalked_claims"

// Claims mirrors the JWT payload Supabase Auth issues, plus the
// school-scoping fields Chalked OS adds via a custom Auth Hook.
type Claims struct {
	Subject  string `json:"sub"`
	SchoolID string `json:"school_id"`
	Role     string `json:"role"` // "principal" | "admin" | "teacher"
	jwt.RegisteredClaims
}

// RequireAuth verifies the bearer token against Supabase's published JWKS
// and enforces that its school_id matches the :schoolSlug path parameter
// before calling the next handler.
func RequireAuth(jwks keyfunc.Keyfunc, schools *SchoolResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			tokenStr, ok := strings.CutPrefix(authHeader, "Bearer ")
			if !ok || tokenStr == "" {
				writeError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Authorization header must be a Bearer token.")
				return
			}

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, jwks.Keyfunc, jwt.WithValidMethods(allowedSigningMethods))
			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "INVALID_TOKEN", "Token is invalid or expired.")
				return
			}

			// Cross-tenant guard: reject before touching any handler or
			// RLS-scoped query. A mismatch or unknown slug both return 404,
			// never 403 — a 403 would confirm the slug belongs to some
			// other real school, which is itself a data leak.
			schoolSlug := chi.URLParam(r, "schoolSlug")
			if schoolSlug != "" {
				schoolID, err := schools.Resolve(r.Context(), schoolSlug)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not verify school.")
					return
				}
				if errors.Is(err, pgx.ErrNoRows) || claims.SchoolID == "" || schoolID != claims.SchoolID {
					writeError(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
					return
				}
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromContext retrieves the authenticated caller's claims, set by
// RequireAuth. Returns false if called on an unauthenticated route.
func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	return claims, ok
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
