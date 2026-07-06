// Package http contains Chi HTTP handlers, one file per resource,
// matching the grouping in the API Specification document.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/config"
	"github.com/dhawalhost/chalkedos/internal/middleware"
	"github.com/dhawalhost/chalkedos/internal/supabase"
)

// NewRouter builds the full route tree for the API service. It fetches
// Supabase's JWKS up front (via ctx) so a misconfigured/unreachable JWKS URL
// fails startup loudly instead of rejecting every request at runtime.
func NewRouter(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) (http.Handler, error) {
	jwks, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.SupabaseJWKSURL})
	if err != nil {
		return nil, fmt.Errorf("fetching Supabase JWKS: %w", err)
	}

	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second)) // AI generation endpoints run long

	r.Get("/healthz", healthCheck(pool))

	authClient := supabase.NewAuthClient(cfg.SupabaseURL, cfg.SupabasePublishableKey)
	limiter := newOTPLimiter()
	r.Route("/api/auth", func(r chi.Router) {
		// Public — no auth required. See internal/http/auth.go.
		r.Post("/otp/request", requestOTP(authClient, limiter))
		r.Post("/otp/verify", verifyOTP(authClient, pool))
	})

	r.Route("/api/{schoolSlug}", func(r chi.Router) {
		r.Use(middleware.RequireAuth(jwks, middleware.NewSchoolResolver(pool)))

		attendanceRoutes(r, pool)
		// TODO(chalked): feeRoutes(r, pool), aiRoutes(r, pool, cfg),
		// communicationRoutes(r, pool), timetableRoutes(r, pool),
		// dashboardRoutes(r, pool) — scaffold these next, following the
		// same pattern as attendance.go.
	})

	r.Post("/api/webhooks/wati", watiWebhook(cfg, pool))

	return r, nil
}

func healthCheck(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// writeJSON is the shared response envelope helper — see the API
// Specification's "Standard response envelope" section.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{"code": code, "message": message},
	})
}
