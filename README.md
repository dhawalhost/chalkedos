# Chalked OS — Go API

Backend service for Chalked OS (github.com/dhawalhost/chalkedos), per
the Technical Architecture document (Go 1.22, Chi router, pgx/sqlc,
deployed via Helm + ArgoCD).

## What's scaffolded here

- `cmd/api/main.go` — entrypoint, graceful shutdown
- `internal/config` — env var config loading (caarlos0/env)
- `internal/db` — pgx connection pool + the `WithRLSClaims` helper that
  makes Postgres RLS apply to Go-issued queries (see the Technical
  Architecture doc, Section 02, for why this matters)
- `internal/middleware` — JWT auth verification
- `internal/http` — Chi handlers; `attendance.go` is wired end-to-end as
  a reference implementation, other resources (fees, AI, communication,
  timetable, dashboard) are stubbed as TODOs following the same pattern
- `db/migrations` — the first 7 migrations from the Database Schema
  document: schools, academic_years, user_profiles, classes/sections/
  subjects, students/guardians/teacher_assignments, attendance_records,
  and RLS policies
- `db/queries/attendance.sql` — sqlc query source for the attendance
  feature
- `deploy/chalked-api/` — the Helm chart, with `values-production.yaml`
  set up for DigitalOcean per the current provider decision
- `Dockerfile` — multi-stage build, distroless final image

## Running locally

```bash
cp .env.example .env   # fill in real values
go run ./cmd/api
```

Requires a Postgres instance reachable at `DATABASE_URL` with the
migrations in `db/migrations` applied (via `golang-migrate` — not yet
added as a dependency in this skeleton; add it next).

## A note on this environment's network restrictions

Module path is set to `github.com/dhawalhost/chalkedos` — matches the
real repo. This skeleton was built and verified (`go build`, `go vet`,
`gofmt`) in a sandboxed environment whose network egress only allows a
fixed domain allowlist — notably **not** `proxy.golang.org` or
`golang.org` itself. To make `go mod tidy` work at all here, `go.mod`
uses `GOPROXY=direct` (fetches straight from GitHub, which is allowed)
plus a handful of `replace` directives pointing `golang.org/x/*` and two
`gopkg.in/*` test-only transitive dependencies at their official GitHub
mirrors:

```
replace golang.org/x/crypto => github.com/golang/crypto v0.17.0
replace golang.org/x/text   => github.com/golang/text v0.14.0
replace golang.org/x/sync   => github.com/golang/sync v0.1.0
replace gopkg.in/yaml.v3    => github.com/go-yaml/yaml/v3 v3.0.1
replace gopkg.in/check.v1   => github.com/go-check/check v0.0.0-...
```

**In Claude Code, your own machine, or CI with normal internet access,
these replace directives are almost certainly unnecessary** — plain
`go mod tidy` with the default proxy should resolve everything directly.
They don't hurt anything if left in (they point at the same code via a
mirror), but feel free to remove them and re-run `go mod tidy` once
you're in an environment with full internet access, just to confirm
the direct path also works cleanly.

## Next steps (in the order the Technical Architecture doc suggests)

1. Add `golang-migrate` and run the existing migrations against a real
   Supabase project.
2. Add `sqlc` as a dev-tool and generate `internal/db/sqlc` from
   `db/queries/attendance.sql` — then wire the real query into
   `attendance.go`, replacing the TODO placeholder.
3. Wire `internal/http/auth.go` to Supabase Auth's phone-OTP endpoints.
4. Add the `internal/ai` package (Claude API client) per the AI Prompt
   Library document, and the `/ai/lesson-plan` route.
5. Add the `internal/whatsapp` package (WATI client) and complete
   `webhooks.go`'s HMAC verification.
6. Scaffold `fees.go`, `communication.go`, `timetable.go`,
   `dashboard.go` following `attendance.go`'s pattern.
7. Add `internal/jobs` for the background WhatsApp-alert dispatch that
   `submitAttendance` currently leaves as a TODO.

## Reference documents

Every design decision here traces back to a document already written:
Technical Architecture (stack + RLS pattern), Database Schema (tables +
policies), API Specification (endpoint contracts), AI Prompt Library
(what `internal/ai` should send to Claude).
