# Chalked OS — Project Context for Claude Code

This file is read automatically by Claude Code at the start of a session
in this repo. It exists so you don't have to re-explain the project
every time you open a new session.

## What this is

Chalked OS — an AI-powered school operating system for Indian private
schools (attendance, fees, AI lesson planning, WhatsApp parent
communication, timetables). Repo: github.com/dhawalhost/chalkedos.

Full product documentation (PRD, personas, user flows, database schema,
API spec, AI prompt library, security plan, etc.) was written before any
code — ask the founder for these if a design question comes up that
isn't answered below; they cover it in detail.

## Stack (decided, not up for casual re-litigation)

- **Backend**: Go 1.22, Chi router, pgx/v5 (not database/sql), sqlc for
  type-safe generated queries. No ORM.
- **Frontend**: Next.js (React), self-hosted (not Vercel — see below).
- **Database**: PostgreSQL via Supabase (managed Postgres + Auth +
  Storage). Row-Level Security on every table — this is the primary
  multi-tenancy defense, not just an app-layer check.
- **AI**: Claude API (claude-sonnet-4-6) for lesson plans, question
  papers, report card remarks.
- **Messaging**: WATI (WhatsApp Business API) — never call Meta's API
  directly.
- **Deployment**: Kubernetes (DigitalOcean, Bangalore region, currently)
  via Helm charts + ArgoCD GitOps. Both frontend and backend run as
  containers on the same cluster, same pipeline — deliberately not
  Vercel, since Vercel's free tier forbids commercial use and a paid
  Vercel Pro seat duplicates a GitOps story we already have for the
  backend.
- **Why Go, why K8s, why not simpler**: founder has 7 years of
  production Go and Kubernetes experience — these choices trade some
  operational simplicity (vs. e.g. Fly.io + Next.js API routes) for
  standing on infrastructure the founder is already fluent in. This was
  a deliberate, discussed trade-off, not an accident — don't "simplify"
  it back to Next.js API routes or a PaaS without asking first.

## The one architectural pattern that matters most

Because Go connects to Postgres directly via pgx (not through Supabase's
JS client), Row-Level Security requires one extra step: **every
authenticated request must run its queries inside `db.WithRLSClaims`**
(see `internal/db/rls.go`), which sets the caller's JWT claims as a
Postgres session variable before any query runs. Skipping this for a
new handler means that handler's queries run with NO RLS claim set —
depending on policy design, this typically means seeing zero rows,
which is safe-but-broken, not a silent leak. But never assume that;
always wrap authenticated queries in `WithRLSClaims`.

## What's already scaffolded

- `cmd/api/main.go`, `internal/config`, `internal/db` (pool + RLS
  helper), `internal/middleware/auth.go` (JWT verify) — all done, all
  building clean.
- `internal/http/attendance.go` — the reference-pattern handler. Copy
  its structure for every new resource (fees, AI, communication,
  timetable, dashboard).
- `db/migrations/000001` through `000012` — **all 21 tables from the
  Database Schema doc, plus RLS policies, are now migrated.** This
  includes fees, AI generations/quota, WhatsApp messages, PTM slots/
  bookings, timetable, substitutions, and audit_log.
- `db/queries/attendance.sql` — sqlc source, not yet generated (sqlc
  isn't installed/run yet — do that early).
- `internal/ai/` — Claude API client (`client.go`) and prompt loading
  via `embed.FS` (`prompts.go`), with the **real, production system
  prompts already written** for all three features under
  `prompts/lesson_plan/v1.0.txt`, `prompts/question_paper/v1.0.txt`,
  `prompts/report_card/v1.0.txt`. Don't rewrite these from scratch —
  they're the actual reviewed prompts from the AI Prompt Library doc.
  What's still missing: the `internal/http/ai.go` handler wiring these
  together with cache-check/quota-check logic (see docs/api-reference.md
  for the exact request/response shape expected).
- `docs/api-reference.md` — condensed, greppable endpoint reference.
  Check this before inventing a new route shape.
- `docs/product-requirements.md` — **what "done" means per feature**,
  not just what shape an endpoint returns. Check this before marking
  a feature complete — e.g. "40 students in under 90 seconds," "same-
  day edit only," "429 with quota_remaining," rate-limit behaviors.
- `docs/architecture.md` — condensed reasoning from the Technical
  Architecture doc: the RLS pattern, AI/WhatsApp flows, deployment
  model, and cost-sensitive decisions not to casually reverse.
- `docs/data-privacy.md` — condensed DPDP-relevant rules: retention
  periods to actually enforce (not just document), children's-data
  constraints, and the audit_log write pattern.
- `docs/design-tokens.css` — the actual color/type system, ready to
  drop into the Next.js app when frontend work starts.
- `deploy/chalked-api/` — Helm chart, values-production.yaml set for
  DigitalOcean.

## Immediate next steps, roughly in order

1. `go mod tidy` with real internet access — the go.mod currently has
   `replace` directives working around a sandboxed environment's
   restricted network (no access to proxy.golang.org). These are very
   likely unnecessary here; try removing them and re-running
   `go mod tidy` first.
2. Install `golang-migrate` and `sqlc`, run all 12 migrations against a
   real Supabase project, generate sqlc code from
   `db/queries/attendance.sql`.
3. Wire the real sqlc-generated query into `attendance.go`, replacing
   the TODO placeholder in `submitAttendance`.
4. Wire `internal/http/auth.go` to Supabase Auth's phone-OTP endpoints.
5. Write sqlc query files for fees, AI, communication, timetable
   (tables already exist via migrations — just need the .sql query
   files, following attendance.sql's pattern).
6. Build `internal/http/ai.go` calling `internal/ai`'s Client — check
   the 30-day cache (query ai_generations by input_hash) and the
   monthly quota (ai_usage_quota) before calling Generate(). Full flow
   is diagrammed in the User Flow Diagrams doc, Flow 03.
7. Build out `internal/whatsapp` (WATI client) and finish the HMAC
   signature verification in `webhooks.go` (currently a TODO stub).
8. Scaffold `fees.go`, `communication.go`, `timetable.go`,
   `dashboard.go` in `internal/http`, following `attendance.go`'s
   pattern exactly (RLS-wrapped queries, same response envelope shape,
   check `docs/api-reference.md` for the exact contract).
9. Add `internal/jobs` for the background WhatsApp-alert dispatch that
   `submitAttendance` currently leaves as a fire-and-forget TODO.

## Response envelope convention (don't deviate)

Every successful response: `{"data": {...}}`.
Every error: `{"error": {"code": "SOME_CODE", "message": "human readable"}}`.
Cross-school access attempts return **404, never 403** — a 403 confirms
a record exists in another tenant, which is itself a data leak. This is
a deliberate security decision, not an oversight if you see it in code
review.

## Things Claude Code should ask the founder before doing, not decide alone

- Changing the cloud provider away from DigitalOcean.
- Switching Helm/ArgoCD for a simpler PaaS.
- Adding a new third-party dependency for something stdlib or an
  existing dependency already covers.
- Any change to the RLS policy pattern in `db/migrations/000007_*` —
  this is the core security boundary; treat changes here as
  security-review-required, not a routine refactor.