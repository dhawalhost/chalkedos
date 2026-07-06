-- name: CreateFeeStructure :one
INSERT INTO fee_structures (school_id, academic_year_id, class_id, component, amount)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetFeeStructures :many
SELECT * FROM fee_structures
WHERE class_id = $1 AND academic_year_id = $2
ORDER BY component;

-- name: RecordFeePayment :one
INSERT INTO fee_payments (school_id, student_id, academic_year_id, amount, payment_mode, payment_date, recorded_by, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetStudentFeeBalance :one
-- Balance is always computed, never stored (F-04): total due from the
-- student's class fee structure minus total paid this academic year.
SELECT
    COALESCE((
        SELECT SUM(fs.amount)
        FROM fee_structures fs
        JOIN sections sec ON sec.class_id = fs.class_id
        JOIN students st ON st.section_id = sec.id
        WHERE st.id = $1 AND fs.academic_year_id = $2
    ), 0)::numeric AS total_due,
    COALESCE((
        SELECT SUM(fp.amount)
        FROM fee_payments fp
        WHERE fp.student_id = $1 AND fp.academic_year_id = $2
    ), 0)::numeric AS total_paid;

-- name: GetDefaulters :many
-- Students with a positive computed balance. last_payment_date stands in
-- for "days overdue" — the schema has no per-component due dates yet
-- (flagged as an open product question), so true overdue-ness is not
-- computable.
WITH due AS (
    SELECT s.id AS student_id, s.full_name, SUM(fs.amount) AS total_due
    FROM students s
    JOIN sections sec ON sec.id = s.section_id
    JOIN fee_structures fs ON fs.class_id = sec.class_id AND fs.academic_year_id = $2
    WHERE s.school_id = $1 AND s.is_active
    GROUP BY s.id, s.full_name
), paid AS (
    SELECT student_id, SUM(amount) AS total_paid
    FROM fee_payments
    WHERE academic_year_id = $2
    GROUP BY student_id
)
SELECT
    d.student_id,
    d.full_name,
    d.total_due::numeric AS total_due,
    COALESCE(p.total_paid, 0)::numeric AS total_paid,
    (d.total_due - COALESCE(p.total_paid, 0))::numeric AS balance_due,
    (SELECT MAX(fp.payment_date) FROM fee_payments fp
     WHERE fp.student_id = d.student_id AND fp.academic_year_id = $2)::date AS last_payment_date
FROM due d
LEFT JOIN paid p ON p.student_id = d.student_id
WHERE d.total_due - COALESCE(p.total_paid, 0) > 0
ORDER BY balance_due DESC;

-- name: HasRecentFeeReminder :one
-- F-04 rate limit: 1 reminder per student per 7 days.
SELECT EXISTS (
    SELECT 1 FROM fee_reminders_log
    WHERE student_id = $1 AND sent_at > now() - interval '7 days'
);

-- name: GetPrimaryGuardian :one
SELECT id, language_pref FROM guardians
WHERE student_id = $1 AND is_primary_contact
LIMIT 1;

-- name: InsertWhatsappMessage :one
-- Rows start 'queued'; internal/jobs (the WATI dispatcher) owns the
-- transition to sent/delivered/failed.
INSERT INTO whatsapp_messages (school_id, student_id, guardian_id, template, language, body_preview, status)
VALUES ($1, $2, $3, $4, $5, $6, 'queued')
RETURNING id;

-- name: InsertFeeReminderLog :exec
INSERT INTO fee_reminders_log (school_id, student_id, whatsapp_message_id)
VALUES ($1, $2, $3);
