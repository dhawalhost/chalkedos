# Data Privacy Reference (condensed)

Full legal context: the Security & Data Privacy Plan document (grounded
in India's DPDP Act 2023 + Rules 2025 — **not legal advice**, and
neither is this file). This is the code-relevant summary: what the data
model and handlers need to actually do.

## Roles (matters for who can request what)

- **School** = Data Fiduciary for student data. **Chalked OS** = Data
  Processor — only acts on the school's instructions.
- **Chalked OS** = independent Data Fiduciary only for its own
  operational data (staff `user_profiles`, usage logs).
- Practical effect: a student/guardian data request should route through
  the school (admin role), not directly to a Chalked OS-only support
  channel. A staff account data request can go direct.

## Retention periods (enforce these, don't just document them)

| Data | Retention | Enforcement |
|---|---|---|
| Active student records | Enrollment + current academic year | `students.is_active`, `withdrawn_at` — never hard-delete on withdrawal |
| Attendance & fee history | 7 years | No automatic purge job needed yet at this scale — revisit before year 7 of any school's data |
| AI generation cache | 30 days | `ai_generations.cache_expires_at` — already enforced by the cache-check query |
| WhatsApp message logs | 1 year | Needs a purge job — **not yet built**, add to `internal/jobs` |
| All school data, post-cancellation | 30-day export window, then delete | Not yet built — needs a scheduled job triggered by `schools.subscription_status = 'cancelled'` |

## Children's data — the one thing to never get casual about

Every student record is, by definition, a minor's data under DPDP's
18-year threshold (the strictest "child" definition of any major
privacy law). Practical rules for code:

- Never add a feature that profiles or behaviourally tracks a student.
  Chalked OS has no student-facing login at all — keep it that way
  unless a documented product decision changes this.
- AI prompts (`internal/ai/prompts/*`) already scope student data to
  the minimum needed per generation — don't add fields to a prompt's
  input without checking whether they're actually necessary for that
  specific generation.
- No feature should aggregate or export student data for any purpose
  other than the specific educational function it was collected for
  (attendance, fees, lesson content). If a "nice to have" analytics
  feature comes up, it needs a privacy-plan review before building, not
  after.

## Audit trail

`audit_log` is written **only by the Go service role**, never a
client-facing insert — see migration `000011`'s
`audit_no_client_write` policy (`FOR ALL USING (false)`). Any handler
that creates/updates/deletes a record a dispute could plausibly arise
from (fee payments, attendance edits) should write an audit_log row
using the service-role connection, not the request-scoped RLS
connection.

## Breach handling (if this ever becomes real, not hypothetical)

DPDP requires notifying **every** breach, not just severe ones, within
72 hours — there's no "was it serious enough" judgment call available.
If something looks like it might be a breach (unexpected query results
across schools, a leaked credential, unexpected RLS bypass), stop and
flag it immediately rather than quietly patching and moving on — see
security@chalked.in in CLAUDE.md.

## Sub-processors (already contractually scoped — don't add a new one silently)

Supabase, Anthropic (Claude API), WATI, DigitalOcean. Adding a new
third-party service that touches student data (a new analytics tool, a
new AI provider, a new email service) is a privacy-plan change, not
just a code change — flag it before integrating.
