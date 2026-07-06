package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RLSClaims is the minimal set of fields Postgres RLS policies check —
// see the Database Schema document, Section 09, for the policies this
// feeds.
type RLSClaims struct {
	Subject  string `json:"sub"`
	SchoolID string `json:"school_id"`
	Role     string `json:"role"`
}

// WithRLSClaims runs fn inside a transaction with the caller's identity
// set as a session-scoped Postgres variable, so that every query fn runs
// is subject to Row-Level Security exactly as if it had come through
// Supabase's own client libraries.
//
// This is the single most important function in the data-access layer:
// forgetting to wrap a query in WithRLSClaims means that query runs
// without the RLS session variable set, and — depending on policy
// design — may see no rows, or may need an explicit service-role bypass.
// It should never silently see another school's data; policies default
// to deny, not allow, when the claim is absent.
func WithRLSClaims(ctx context.Context, pool *pgxpool.Pool, claims RLSClaims, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op if committed

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return fmt.Errorf("marshaling RLS claims: %w", err)
	}

	// `true` (the third arg to set_config) scopes this to the current
	// transaction only — it does not leak to other requests sharing a
	// pooled connection.
	_, err = tx.Exec(ctx, `SELECT set_config('request.jwt.claims', $1, true)`, string(claimsJSON))
	if err != nil {
		return fmt.Errorf("setting RLS session claims: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
