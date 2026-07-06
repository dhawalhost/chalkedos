-- name: GetCachedGeneration :one
-- 30-day cache lookup by normalized input hash — a hit must be
-- near-instant and must not call Claude or consume quota (F-05).
SELECT * FROM ai_generations
WHERE school_id = $1 AND input_hash = $2 AND feature = $3
  AND cache_expires_at > now()
ORDER BY generated_at DESC
LIMIT 1;

-- name: InsertGeneration :one
INSERT INTO ai_generations (
    school_id, teacher_id, feature, prompt_version, input_hash,
    input_json, output_json, language, model, cost_inr, cache_expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now() + interval '30 days')
RETURNING *;

-- name: UpdateGenerationOutput :one
-- Saves a teacher's inline edits. Scoped to the owning teacher — RLS
-- already scopes to the school, this narrows to the author.
UPDATE ai_generations
SET output_json = $3
WHERE id = $1 AND teacher_id = $2
RETURNING id;

-- name: GetCurrentAcademicYear :one
SELECT id FROM academic_years
WHERE school_id = $1 AND is_current
LIMIT 1;

-- name: EnsureQuotaRow :exec
-- Creates the school's quota row for a month if absent; no-op otherwise.
INSERT INTO ai_usage_quota (school_id, academic_year_id, month, generations_used, generations_limit)
VALUES ($1, $2, $3, 0, 500)
ON CONFLICT (school_id, month) DO NOTHING;

-- name: ReserveQuota :one
-- Atomically consumes $3 generations if (and only if) the whole amount
-- fits under the limit — partial batch reservations are all-or-nothing
-- per F-09 ("warn before starting a batch that would exceed the
-- remaining quota, don't fail partway through").
UPDATE ai_usage_quota
SET generations_used = generations_used + $3
WHERE school_id = $1 AND month = $2
  AND generations_used + $3 <= generations_limit
RETURNING generations_limit - generations_used AS remaining;

-- name: ReleaseQuota :exec
-- Returns reserved quota after a failed Claude call — reservation
-- happens before the call (F-05: "checked before calling Claude"), so a
-- failure must give the generation back.
UPDATE ai_usage_quota
SET generations_used = GREATEST(generations_used - $3, 0)
WHERE school_id = $1 AND month = $2;

-- name: GetQuota :one
SELECT generations_used, generations_limit FROM ai_usage_quota
WHERE school_id = $1 AND month = $2;
