-- name: UpsertAttendance :one
-- Upserts a single student's attendance for a date, matching the
-- unique(student_id, date) constraint. Called once per student in a
-- loop from the submitAttendance handler — see API Specification,
-- Section 03.
INSERT INTO attendance_records (school_id, student_id, section_id, date, status, marked_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (student_id, date)
DO UPDATE SET
    status     = EXCLUDED.status,
    edited_at  = now(),
    edited_by  = EXCLUDED.marked_by
RETURNING *;

-- name: GetTodayAttendance :many
-- Backs GET /api/:school/attendance/today — returns every student in a
-- section with their attendance status for a given date, if marked.
SELECT
    s.id AS student_id,
    s.full_name,
    ar.status
FROM students s
LEFT JOIN attendance_records ar
    ON ar.student_id = s.id AND ar.date = $2
WHERE s.section_id = $1 AND s.is_active = true
ORDER BY s.full_name;

-- name: GetFlaggedStudents :many
-- Students with 3+ consecutive absences ending today, computed on read
-- per the Database Schema document's "derived logic, not stored" note.
WITH recent AS (
    SELECT
        student_id,
        date,
        status,
        ROW_NUMBER() OVER (PARTITION BY student_id ORDER BY date DESC) AS rn
    FROM attendance_records
    WHERE school_id = $1 AND date <= CURRENT_DATE
)
SELECT student_id, COUNT(*) AS consecutive_absences
FROM recent
WHERE rn <= 3 AND status = 'absent'
GROUP BY student_id
HAVING COUNT(*) = 3;

-- name: GetSchoolAttendanceSummary :one
-- School-wide attendance percentage for a given date — a single
-- indexed aggregate, cheap enough to compute live rather than cache,
-- per the Database Schema document.
SELECT
    COUNT(*) FILTER (WHERE status = 'present') AS present_count,
    COUNT(*) AS total_count
FROM attendance_records
WHERE school_id = $1 AND date = $2;

-- name: GetAttendanceHistory :many
-- Backs GET /api/:school/attendance/history?section_id=&from=&to=.
SELECT ar.student_id, s.full_name, ar.date, ar.status
FROM attendance_records ar
JOIN students s ON s.id = ar.student_id
WHERE ar.section_id = $1 AND ar.date BETWEEN sqlc.arg(from_date) AND sqlc.arg(to_date)
ORDER BY ar.date, s.full_name;

-- name: UpdateAttendanceStatus :one
-- Backs PATCH /api/:school/attendance/:id. The date predicate enforces
-- the same-school-day-only edit rule (F-02) at the query level as well
-- as in the handler — an id from a past day matches zero rows.
UPDATE attendance_records
SET status = $2, edited_at = now(), edited_by = $3
WHERE id = $1 AND date = $4
RETURNING id;
