package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schoolCacheTTL bounds how stale a slug->id mapping can be. Schools are
// created rarely, so this trades a little staleness for avoiding a DB
// round-trip on every authenticated request.
const schoolCacheTTL = 60 * time.Second

type schoolCacheEntry struct {
	id        string
	expiresAt time.Time
}

// SchoolResolver resolves a school's URL slug to its id via the
// resolve_school_id SQL function (see
// db/migrations/000013_add_school_slug_resolver.up.sql), which is
// SECURITY DEFINER so it can run before any RLS claim is set for the
// request — it returns only an id, never a full schools row.
type SchoolResolver struct {
	pool *pgxpool.Pool

	mu    sync.Mutex
	cache map[string]schoolCacheEntry
}

func NewSchoolResolver(pool *pgxpool.Pool) *SchoolResolver {
	return &SchoolResolver{pool: pool, cache: make(map[string]schoolCacheEntry)}
}

// Resolve returns the school id for the given slug, or pgx.ErrNoRows if no
// school has that slug.
func (r *SchoolResolver) Resolve(ctx context.Context, slug string) (string, error) {
	r.mu.Lock()
	if entry, ok := r.cache[slug]; ok && time.Now().Before(entry.expiresAt) {
		r.mu.Unlock()
		return entry.id, nil
	}
	r.mu.Unlock()

	// resolve_school_id returns NULL for an unknown slug — scan into a
	// pointer so that comes back as a nil *string instead of an error.
	// Cast to text: pgx has no default scan plan from the uuid wire format
	// into a bare Go string, only into pgtype.UUID.
	var id *string
	err := r.pool.QueryRow(ctx, `SELECT resolve_school_id($1)::text`, slug).Scan(&id)
	if err != nil {
		return "", err
	}
	if id == nil {
		return "", pgx.ErrNoRows
	}

	r.mu.Lock()
	r.cache[slug] = schoolCacheEntry{id: *id, expiresAt: time.Now().Add(schoolCacheTTL)}
	r.mu.Unlock()

	return *id, nil
}
