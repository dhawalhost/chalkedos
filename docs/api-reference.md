# API Reference (condensed)

Full detail with request/response examples: see the API Specification
document. This file is the quick-grep version for Claude Code to check
a contract against while building — endpoint list, roles, and status
codes only.

## Conventions

- Base path: `/api/{schoolSlug}/...` — every route scoped to one tenant.
- Auth: `Authorization: Bearer <supabase-jwt>` on everything except
  `/api/auth/*` and `/api/webhooks/wati`.
- Success: `{"data": {...}}`. Error: `{"error": {"code", "message"}}`.
- Cross-school access → **404, never 403** (403 would confirm the
  record exists in another tenant — a data leak).
- Rate limits: 1 WhatsApp absence alert/student/day, 1 fee reminder/
  student/week, 500 AI generations/school/month (Pro tier), 5 OTP
  requests/15min per phone+IP.

## Auth & Setup

| Method | Path | Roles | Notes |
|---|---|---|---|
| POST | `/api/auth/otp/request` | Public | Rate-limited |
| POST | `/api/auth/otp/verify` | Public | Returns JWT + school + role |
| POST | `/api/:school/setup/classes` | Admin | Bulk create |
| POST | `/api/:school/setup/students/import` | Admin | CSV bulk import |
| POST | `/api/:school/students` | Admin | Single create |
| PATCH | `/api/:school/students/:id` | Admin | |
| GET | `/api/:school/students` | Teacher, Admin, Principal | Teachers see only assigned sections |

## Attendance (F-02, F-03)

| Method | Path | Roles | Notes |
|---|---|---|---|
| GET | `/api/:school/attendance/today` | Teacher, Admin | `?section_id=` |
| POST | `/api/:school/attendance` | Teacher, Admin | Upsert; triggers async WhatsApp alerts for absences |
| PATCH | `/api/:school/attendance/:id` | Teacher, Admin | Same school-day only |
| GET | `/api/:school/attendance/history` | Teacher, Admin, Principal | `?section_id=&from=&to=` |
| GET | `/api/:school/attendance/flagged` | Admin, Principal | 3+ consecutive absences, computed on read |
| GET | `/api/:school/attendance/summary` | Admin, Principal | School-wide % for a date |

## Fees (F-04)

| Method | Path | Roles | Notes |
|---|---|---|---|
| POST | `/api/:school/fees/structure` | Admin | Per class per academic year |
| GET | `/api/:school/fees/structure` | Admin, Principal | `?class_id=` |
| POST | `/api/:school/fees/payments` | Admin | Supports partial payment |
| GET | `/api/:school/fees/students/:id/balance` | Admin, Principal | |
| GET | `/api/:school/fees/defaulters` | Admin, Principal | |
| POST | `/api/:school/fees/defaulters/remind` | Admin | Rate-limited, skips silently |
| GET | `/api/:school/fees/export` | Admin, Principal | Signed URL to Excel |

## AI Generation (F-05, F-08, F-09)

| Method | Path | Roles | Notes |
|---|---|---|---|
| POST | `/api/:school/ai/lesson-plan` | Teacher | Cache 30d, quota-checked |
| PATCH | `/api/:school/ai/lesson-plan/:id` | Teacher | Save edits |
| GET | `/api/:school/ai/lesson-plan/:id/pdf` | Teacher | |
| POST | `/api/:school/ai/question-paper` | Teacher | Marks must sum exactly — validate server-side, don't trust the model |
| GET | `/api/:school/ai/question-paper/:id/pdf` | Teacher | |
| POST | `/api/:school/ai/report-cards/batch` | Teacher | One generation per student, not one call for the class |
| PATCH | `/api/:school/ai/report-cards/:id` | Teacher | |
| GET | `/api/:school/ai/report-cards/class/:sectionId/pdf` | Teacher, Admin | |
| GET | `/api/:school/ai/usage` | Teacher, Admin, Principal | Quota status |

## Communication (F-03, F-07)

| Method | Path | Roles | Notes |
|---|---|---|---|
| POST | `/api/:school/communication/broadcast` | Admin, Principal | Pre-approved template only |
| POST | `/api/:school/communication/ptm/slots` | Admin, Teacher | |
| GET | `/api/:school/communication/ptm/slots` | Public (signed token) | Parent booking link, no JWT |
| POST | `/api/:school/communication/ptm/bookings` | Public (signed token) | 409 on double-booking race |
| GET | `/api/:school/communication/messages` | Admin, Principal | `?student_id=` |

## Timetable (F-06) & Dashboard

| Method | Path | Roles | Notes |
|---|---|---|---|
| POST | `/api/:school/timetable/slots` | Admin | 409 on conflict, names which kind |
| GET | `/api/:school/timetable` | Teacher, Admin, Principal | `?section_id=` |
| GET | `/api/:school/timetable/me` | Teacher | |
| POST | `/api/:school/timetable/substitutions` | Admin | |
| GET | `/api/:school/dashboard/principal` | Principal | One denormalized call, not 5 |

## Webhooks

| Method | Path | Auth | Notes |
|---|---|---|---|
| POST | `/api/webhooks/wati` | HMAC signature, not JWT | Always returns 200, even for unrecognised message IDs |
