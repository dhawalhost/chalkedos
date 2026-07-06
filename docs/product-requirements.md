# Product Requirements Reference (condensed)

Full detail, personas, and rationale: the PRD document. This is the
acceptance-criteria checklist — what "done" actually means for each
MVP feature, as opposed to `docs/api-reference.md`'s "what shape does
the endpoint return." Use this to verify a feature, not just compile it.

## F-01 — School Setup & Data Management

- Admin creates classes/sections/subjects and assigns per class in the
  setup flow — under 60 minutes total with founder assistance.
- Bulk CSV import for students; a failed row must not silently drop —
  surface which rows failed and why.
- Role-based access from day one: principal sees all, teacher sees only
  assigned sections, admin sees fees + records. This is enforced by RLS
  (see docs/architecture.md), not just hidden UI.

## F-02 — Attendance Marking

- Teacher's class list is **pre-loaded** on the Attendance Home screen
  — zero navigation required to start marking.
- **40 students markable in under 90 seconds** on a 3G connection. If a
  handler or query design makes this infeasible, that's a real problem
  to flag, not a detail to skip.
- Default status is Present; teacher only taps exceptions.
- Editable **same school day only** — see `attendance_records.edited_at`
  / `edited_by`. Reject edits after the day boundary at the handler
  level, not just in the UI.
- Works on mobile web, no app install. Minimum: Android 8, Chrome 90+.
- Offline: queue submission locally, sync on reconnect — don't fail the
  teacher's action just because connectivity dropped mid-tap.

## F-03 — Parent WhatsApp Alert

- Parent receives the WhatsApp message **within 5 minutes** of a
  student being marked absent.
- Hindi by default; English if the guardian's `language_pref` says so.
- **Rate limit: max 1 alert per student per day** — enforced by
  checking `whatsapp_messages` before sending, not just documented as a
  policy.
- Delivery status (sent/delivered/read/failed) visible to admin —
  driven by the WATI webhook updating `whatsapp_messages.status`.
- Uses **pre-approved Meta templates only** — never free-form text sent
  as a business-initiated message.

## F-04 — Fee Tracker

- Partial payments supported — balance is **always computed**
  (`SUM(fee_structures.amount) - SUM(fee_payments.amount)`), never
  stored as a running total. See docs/data-privacy.md and
  docs/architecture.md for why.
- Defaulter list shows amount due + days overdue per student.
- Bulk WhatsApp reminder is **rate-limited to 1 per student per 7
  days** — a student reminded this week is silently skipped, not
  treated as an error, and the skip count is reported back
  (`skipped_rate_limited` in the response).
- Excel export available on demand for admin/principal.

## F-05 — AI Lesson Plan Generator

- Generation completes in **under 20 seconds** on average.
- Output structure is fixed JSON (see AI prompt files) — the frontend
  depends on this shape, don't let a handler pass through unvalidated
  model output.
- **30-day cache** on identical input (board+class+subject+chapter+
  duration+language, normalized) — cache hit must be near-instant, no
  Claude API call.
- **Monthly quota per school** (default 500, Pro tier) — checked
  *before* calling Claude, not after. 429 response includes
  `quota_remaining`.
- Every generation is editable inline before use — never auto-submitted
  or auto-shared with a parent.
- If a chapter name doesn't match a known curriculum entry, the model
  is instructed to flag this in `curriculum_note` rather than
  fabricate — don't silently swallow that field if present.

## F-06 — Timetable Builder

- Conflict detection on **two dimensions**: one subject per section per
  period, AND one place per teacher per period (both are DB-level
  UNIQUE constraints — see migration 000011). A 409 response should
  name which conflict occurred, not just "conflict."
- Teacher sees only their own schedule at `/timetable/me`.
- Substitution assignment works when a teacher is marked absent for a
  scheduled slot.

## F-07 — Parent Broadcast & PTM Scheduler

- Broadcast uses a pre-approved template, free text goes into the
  template's variable slot — same Meta constraint as F-03.
- PTM booking is a **public endpoint, signed-token auth, not JWT** —
  parents never log in. Token is single-use once a slot is confirmed,
  and expires (7 days per the API spec).
- **409 on double-booking** — a real race condition given the booking
  link is shared over WhatsApp; the UNIQUE constraint on
  `ptm_bookings.ptm_slot_id` should be what actually prevents this, not
  just an application-level check-then-insert.

## F-08 — AI Question Paper Generator

- **Marks must sum to `total_marks` exactly.** The model is asked to
  self-check (`total_marks_check`), but the **Go handler must
  independently re-sum every question's marks** and compare — retry
  once automatically on mismatch rather than surfacing a broken paper.
- No verbatim reproduction of textbook problems — original questions
  testing the same concepts (a prompt-level instruction, not enforceable
  in Go, but worth knowing if a generated paper looks suspiciously
  familiar during testing).

## F-09 — AI Report Card Remarks Generator

- **One Claude call per student**, not one call for the whole class —
  deliberate, for cache/retry isolation (see docs/architecture.md).
- Remark never compares one student to another or to a class average —
  enforced by never including that data in the prompt's input in the
  first place.
- Batch generation counts each student as a **separate quota
  generation** — warn before starting a batch that would exceed the
  remaining monthly quota, don't just fail partway through.

## Non-functional requirements that apply across all features

- Every cross-school access attempt → 404, never 403 (see
  docs/api-reference.md).
- Standard response envelope (`{"data":...}` / `{"error":{"code",
  "message"}}`) — don't introduce a different shape for a new endpoint.
- Uptime target 99.5% monthly, AI generation calls may run up to ~25s —
  don't set an aggressive timeout that kills a legitimate slow
  generation (see `main.go`'s 30s write timeout — that number is
  deliberate, not arbitrary).