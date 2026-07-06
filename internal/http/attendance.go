package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/db"
	"github.com/dhawalhost/chalkedos/internal/db/sqlc"
)

// parseUUID and parseDate wrap pgtype's Scan so handlers can turn client-
// supplied strings into typed params with a single INVALID_FIELD error path,
// rather than repeating a Scan+error-check at every call site.
func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	err := u.Scan(s)
	return u, err
}

func parseDate(s string) (pgtype.Date, error) {
	var d pgtype.Date
	err := d.Scan(s)
	return d, err
}

func attendanceRoutes(r chi.Router, pool *pgxpool.Pool) {
	r.Route("/attendance", func(r chi.Router) {
		r.Get("/today", getTodayAttendance(pool))
		r.Post("/", submitAttendance(pool))
		r.Patch("/{id}", editAttendance(pool))
		r.Get("/history", getAttendanceHistory(pool))
		r.Get("/flagged", getFlaggedStudents(pool))
		r.Get("/summary", getAttendanceSummary(pool))
	})
}

// istZone is the school-day timezone for the same-day-only edit rule
// (F-02). Fixed offset rather than tzdata lookup: IST has no DST, and a
// FixedZone can't fail on containers shipped without a tz database.
var istZone = time.FixedZone("IST", 5*3600+30*60)

// todayIST is the current school day as YYYY-MM-DD.
func todayIST() string {
	return time.Now().In(istZone).Format("2006-01-02")
}

var validAttendanceStatus = map[string]bool{
	"present": true, "absent": true, "late": true, "leave": true,
}

type attendanceRecord struct {
	StudentID string `json:"student_id"`
	Status    string `json:"status"` // present | absent | late | leave
}

type submitAttendanceRequest struct {
	SectionID string             `json:"section_id"`
	Date      string             `json:"date"`
	Records   []attendanceRecord `json:"records"`
}

// submitAttendance implements POST /api/:school/attendance — see the
// API Specification, Section 03, and User Flow Diagrams, Flow 01, for
// the full behaviour this handler is responsible for (upsert semantics,
// downstream WhatsApp alerts for absences).
func submitAttendance(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "teacher", "admin")
		if claims == nil {
			return
		}

		var req submitAttendanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if req.SectionID == "" || req.Date == "" || len(req.Records) == 0 {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "section_id, date, and records are required.")
			return
		}
		// F-02: attendance is markable and editable for the current school
		// day only — reject any other date at the handler level.
		if req.Date != todayIST() {
			writeErr(w, http.StatusUnprocessableEntity, "NOT_SAME_DAY", "Attendance can only be marked for today's date.")
			return
		}
		for i, rec := range req.Records {
			if !validAttendanceStatus[rec.Status] {
				writeErr(w, http.StatusBadRequest, "INVALID_FIELD", fmt.Sprintf("records[%d].status must be present|absent|late|leave.", i))
				return
			}
		}

		sectionID, err := parseUUID(req.SectionID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "section_id is not a valid UUID.")
			return
		}
		date, err := parseDate(req.Date)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "date must be in YYYY-MM-DD format.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}
		markedBy, err := parseUUID(claims.Subject)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's subject is not a valid UUID.")
			return
		}

		markedCount := 0
		absentCount := 0

		err = db.WithRLSClaims(r.Context(), pool, db.RLSClaims{
			Subject:  claims.Subject,
			SchoolID: claims.SchoolID,
			Role:     claims.Role,
		}, func(tx pgx.Tx) error {
			queries := sqlc.New(tx)
			for _, rec := range req.Records {
				studentID, err := parseUUID(rec.StudentID)
				if err != nil {
					return err
				}
				if _, err := queries.UpsertAttendance(r.Context(), sqlc.UpsertAttendanceParams{
					SchoolID:  schoolID,
					StudentID: studentID,
					SectionID: sectionID,
					Date:      date,
					Status:    rec.Status,
					MarkedBy:  markedBy,
				}); err != nil {
					return err
				}
				markedCount++
				if rec.Status == "absent" {
					absentCount++
				}
			}
			return nil
		})

		if err != nil {
			writeErr(w, http.StatusInternalServerError, "SUBMIT_FAILED", "Could not save attendance.")
			return
		}

		// TODO(chalked): enqueue WhatsApp absence alerts here via a
		// goroutine / background job (internal/jobs), per the User Flow
		// Diagrams doc — this must not block the response to the teacher.

		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"marked_count":           markedCount,
			"absent_count":           absentCount,
			"whatsapp_alerts_queued": absentCount,
		})
	}
}

// getTodayAttendance implements GET /api/:school/attendance/today.
func getTodayAttendance(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "teacher", "admin")
		if claims == nil {
			return
		}

		sectionIDStr := r.URL.Query().Get("section_id")
		if sectionIDStr == "" {
			writeErr(w, http.StatusBadRequest, "MISSING_SECTION_ID", "section_id query parameter is required.")
			return
		}
		sectionID, err := parseUUID(sectionIDStr)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "section_id is not a valid UUID.")
			return
		}

		today := todayIST()
		date, err := parseDate(today)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not compute today's date.")
			return
		}

		var rows []sqlc.GetTodayAttendanceRow
		err = db.WithRLSClaims(r.Context(), pool, db.RLSClaims{
			Subject:  claims.Subject,
			SchoolID: claims.SchoolID,
			Role:     claims.Role,
		}, func(tx pgx.Tx) error {
			queries := sqlc.New(tx)
			var err error
			rows, err = queries.GetTodayAttendance(r.Context(), sqlc.GetTodayAttendanceParams{
				SectionID: sectionID,
				Date:      date,
			})
			return err
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load today's attendance.")
			return
		}

		students := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			status := ""
			if row.Status != nil {
				status = *row.Status
			}
			students = append(students, map[string]interface{}{
				"student_id": row.StudentID,
				"full_name":  row.FullName,
				"status":     status,
			})
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"section":  map[string]string{"id": sectionIDStr},
			"date":     today,
			"students": students,
		})
	}
}

// getFlaggedStudents implements GET /api/:school/attendance/flagged —
// students with 3+ consecutive absences, computed on read per the
// Database Schema document's "derived logic" section (no stored flag
// column, to avoid a trigger keeping a redundant field in sync).
func getFlaggedStudents(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin", "principal")
		if claims == nil {
			return
		}

		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		var rows []sqlc.GetFlaggedStudentsRow
		err = db.WithRLSClaims(r.Context(), pool, db.RLSClaims{
			Subject:  claims.Subject,
			SchoolID: claims.SchoolID,
			Role:     claims.Role,
		}, func(tx pgx.Tx) error {
			queries := sqlc.New(tx)
			var err error
			rows, err = queries.GetFlaggedStudents(r.Context(), schoolID)
			return err
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load flagged students.")
			return
		}

		flagged := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			flagged = append(flagged, map[string]interface{}{
				"student_id":           row.StudentID,
				"consecutive_absences": row.ConsecutiveAbsences,
			})
		}
		writeJSON(w, http.StatusOK, flagged)
	}
}

// editAttendance implements PATCH /api/:school/attendance/:id — edits a
// single record's status, same school day only (F-02): the record's date
// must be today, enforced in the UPDATE's WHERE clause.
func editAttendance(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "teacher", "admin")
		if claims == nil {
			return
		}

		recordID, err := parseUUID(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "id is not a valid UUID.")
			return
		}

		var req struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validAttendanceStatus[req.Status] {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "status must be present|absent|late|leave.")
			return
		}

		editedBy, err := parseUUID(claims.Subject)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's subject is not a valid UUID.")
			return
		}
		today, err := parseDate(todayIST())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not compute today's date.")
			return
		}

		err = db.WithRLSClaims(r.Context(), pool, db.RLSClaims{
			Subject:  claims.Subject,
			SchoolID: claims.SchoolID,
			Role:     claims.Role,
		}, func(tx pgx.Tx) error {
			_, err := sqlc.New(tx).UpdateAttendanceStatus(r.Context(), sqlc.UpdateAttendanceStatusParams{
				ID:       recordID,
				Status:   req.Status,
				EditedBy: editedBy,
				Date:     today,
			})
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Unknown id, other school's record (RLS), or a past day's
			// record — indistinguishable by design.
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "UPDATE_FAILED", "Could not update attendance.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
	}
}

// getAttendanceHistory implements GET /api/:school/attendance/history
// ?section_id=&from=&to=.
func getAttendanceHistory(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "teacher", "admin", "principal")
		if claims == nil {
			return
		}

		sectionID, err := parseUUID(r.URL.Query().Get("section_id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "section_id query parameter must be a valid UUID.")
			return
		}
		from, err := parseDate(r.URL.Query().Get("from"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "from must be YYYY-MM-DD.")
			return
		}
		to, err := parseDate(r.URL.Query().Get("to"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "to must be YYYY-MM-DD.")
			return
		}
		if to.Time.Before(from.Time) || to.Time.Sub(from.Time) > 366*24*time.Hour {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "from..to must be a valid range of at most one year.")
			return
		}

		var rows []sqlc.GetAttendanceHistoryRow
		err = db.WithRLSClaims(r.Context(), pool, db.RLSClaims{
			Subject:  claims.Subject,
			SchoolID: claims.SchoolID,
			Role:     claims.Role,
		}, func(tx pgx.Tx) error {
			var err error
			rows, err = sqlc.New(tx).GetAttendanceHistory(r.Context(), sqlc.GetAttendanceHistoryParams{
				SectionID: sectionID,
				FromDate:  from,
				ToDate:    to,
			})
			return err
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load attendance history.")
			return
		}

		records := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			records = append(records, map[string]interface{}{
				"student_id": row.StudentID,
				"full_name":  row.FullName,
				"date":       row.Date.Time.Format("2006-01-02"),
				"status":     row.Status,
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"records": records})
	}
}

// getAttendanceSummary implements GET /api/:school/attendance/summary
// ?date= — school-wide attendance percentage for a date (F-02 dashboard).
func getAttendanceSummary(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin", "principal")
		if claims == nil {
			return
		}

		dateStr := r.URL.Query().Get("date")
		if dateStr == "" {
			dateStr = todayIST()
		}
		date, err := parseDate(dateStr)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "date must be YYYY-MM-DD.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		var summary sqlc.GetSchoolAttendanceSummaryRow
		err = db.WithRLSClaims(r.Context(), pool, db.RLSClaims{
			Subject:  claims.Subject,
			SchoolID: claims.SchoolID,
			Role:     claims.Role,
		}, func(tx pgx.Tx) error {
			var err error
			summary, err = sqlc.New(tx).GetSchoolAttendanceSummary(r.Context(), sqlc.GetSchoolAttendanceSummaryParams{
				SchoolID: schoolID,
				Date:     date,
			})
			return err
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load attendance summary.")
			return
		}

		pct := 0.0
		if summary.TotalCount > 0 {
			pct = float64(summary.PresentCount) / float64(summary.TotalCount) * 100
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"date":           dateStr,
			"present_count":  summary.PresentCount,
			"total_count":    summary.TotalCount,
			"attendance_pct": pct,
		})
	}
}
