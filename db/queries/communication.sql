-- name: UpdateWhatsappMessageStatusTx :one
-- RLS-scoped variant of the webhook's SECURITY DEFINER updater, for any
-- future in-app status corrections. (The WATI webhook itself uses
-- update_whatsapp_message_status — see migration 000014.)
UPDATE whatsapp_messages
SET status = $2
WHERE id = $1
RETURNING id;

-- name: GetBroadcastRecipients :many
-- Primary guardians of active students, optionally narrowed to a section.
SELECT g.id AS guardian_id, g.student_id, g.language_pref
FROM guardians g
JOIN students s ON s.id = g.student_id
WHERE s.school_id = $1 AND s.is_active AND g.is_primary_contact
  AND (sqlc.narg(section_id)::uuid IS NULL OR s.section_id = sqlc.narg(section_id))
ORDER BY g.student_id;

-- name: ListWhatsappMessages :many
SELECT id, student_id, guardian_id, template, language, body_preview,
       status, sent_at, delivered_at, failure_reason, created_at
FROM whatsapp_messages
WHERE school_id = $1
  AND (sqlc.narg(student_id)::uuid IS NULL OR student_id = sqlc.narg(student_id))
ORDER BY created_at DESC
LIMIT 100;

-- name: CreatePTMSlot :one
INSERT INTO ptm_slots (school_id, teacher_id, slot_time)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetStudentWithPrimaryGuardian :one
-- Used when minting a PTM booking token: confirms the student is active
-- in this school and finds the guardian the token is issued to.
SELECT s.id AS student_id, g.id AS guardian_id
FROM students s
JOIN guardians g ON g.student_id = s.id AND g.is_primary_contact
WHERE s.id = $1 AND s.school_id = $2 AND s.is_active;
