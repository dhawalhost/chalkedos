# Architecture Reference (condensed)

Full reasoning and diagrams: the Technical Architecture document. This
is the quick-reference version — decisions and constraints Claude Code
should check before proposing an alternative.

## Stack (settled — see CLAUDE.md for "don't re-litigate without asking")

Go 1.22 + Chi + pgx/sqlc · Next.js (self-hosted, not Vercel) · Postgres
via Supabase (Auth + Storage too) · Claude API (claude-sonnet-4-6) ·
WATI (WhatsApp) · Kubernetes (DigitalOcean, Bangalore) + Helm + ArgoCD.

## The RLS pattern (the single most important thing to get right)

Go connects to Postgres directly via pgx, not through Supabase's JS
client. To make Row-Level Security apply anyway, every authenticated
request runs inside `db.WithRLSClaims(ctx, pool, claims, func(tx pgx.Tx) error {...})`,
which does `SELECT set_config('request.jwt.claims', <json>, true)`
inside the transaction before any query runs. RLS policies then read
`current_setting('request.jwt.claims', true)::json ->> 'school_id'`.

**Every new handler that touches the database must go through this.**
A handler that queries the pool directly, bypassing `WithRLSClaims`,
either sees zero rows (safe but broken) or — if run under a
service-role connection — sees everything (a real vulnerability). When
in doubt, follow `internal/http/attendance.go`'s pattern exactly.

## Multi-tenancy — three layers, not one

1. **Routing**: `/api/{schoolSlug}/...` — middleware rejects a request
   whose JWT school_id doesn't match the slug, before any handler runs.
2. **Application**: handlers re-check school_id/role before querying,
   even though RLS would also block it — defense in depth, not
   redundant.
3. **Database (primary defense)**: RLS policies, as above. This is the
   layer that makes a leak "structurally impossible," not just unlikely.

Cross-school access → **404, never 403** (403 confirms the record
exists elsewhere — itself a leak).

## AI generation flow (Claude API)

1. Compute `input_hash` from normalized input (board+class+subject+
   chapter+duration+language, lowercased).
2. Check `ai_generations` for a matching hash within 30 days
   (`cache_expires_at`) — cache hit returns instantly, no API call.
3. Check `ai_usage_quota` for the school/month — quota exhausted
   returns 429, never silently calls Claude anyway.
4. Call `internal/ai.Client.Generate()` with the versioned system
   prompt (`internal/ai/prompts/<feature>/v1.0.txt`).
5. Parse the response as JSON against the schema in the AI Prompt
   Library doc — **validate server-side** (e.g. question paper marks
   must sum to `total_marks` — re-sum in Go, don't trust the model's
   own `total_marks_check` field).
6. Log the generation (school_id, teacher_id, feature, prompt_version,
   input_hash, cost_inr) and increment the quota counter.

## WhatsApp flow (WATI)

Outbound: rate-check (1 absence alert/student/day via
`whatsapp_messages`, 1 fee reminder/student/week via
`fee_reminders_log`) → fill approved template → POST to WATI → log the
message row with status `queued`.

Inbound: `/api/webhooks/wati` verifies an HMAC signature (not JWT, WATI
isn't a logged-in user) → updates `whatsapp_messages.status` by
matching `wati_message_id` → **always returns 200**, even for an
unrecognized ID, to stop WATI retrying indefinitely.

## Deployment

Two Helm charts (`chalked-api`, `chalked-web` — web not yet scaffolded)
on one Kubernetes cluster, one ArgoCD instance. CI builds + pushes an
image, bumps `image.tag` in the relevant `values-production.yaml`,
commits — ArgoCD detects the Git change and reconciles the cluster.
Rollback is `git revert`, not a separate tool.

Cloud provider (DigitalOcean) is deliberately swappable — provider-
specific detail lives only in `values-production.yaml` (load balancer
annotations, storage class), never in the Helm templates themselves.
Don't hardcode a DigitalOcean-specific assumption into application code.

## Cost-sensitive design decisions (don't casually reverse these)

- 30-day AI response caching exists specifically to control Claude API
  cost — don't shorten it without checking the Business Model doc's
  cost assumptions.
- Report card batch generation is one Claude call per student, not one
  call for the whole class — deliberate, for cache/retry isolation, not
  an oversight to "optimize" into a single call.
