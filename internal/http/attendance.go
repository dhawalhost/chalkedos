package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/db"
	"github.com/dhawalhost/chalkedos/internal/db/sqlc"
	"github.com/dhawalhost/chalkedos/internal/middleware"
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
		r.Get("/flagged", getFlaggedStudents(pool))
	})
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
		claims, ok := middleware.FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "MISSING_CLAIMS", "Authentication required.")
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
		claims, ok := middleware.FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "MISSING_CLAIMS", "Authentication required.")
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

		today := time.Now().Format("2006-01-02")
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
		claims, ok := middleware.FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "MISSING_CLAIMS", "Authentication required.")
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
