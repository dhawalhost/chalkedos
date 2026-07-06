package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/db"
	"github.com/dhawalhost/chalkedos/internal/db/sqlc"
	"github.com/dhawalhost/chalkedos/internal/middleware"
)

func feeRoutes(r chi.Router, pool *pgxpool.Pool) {
	r.Route("/fees", func(r chi.Router) {
		r.Post("/structure", createFeeStructure(pool))
		r.Get("/structure", getFeeStructures(pool))
		r.Post("/payments", recordFeePayment(pool))
		r.Get("/students/{id}/balance", getStudentBalance(pool))
		r.Get("/defaulters", getDefaulters(pool))
		r.Post("/defaulters/remind", remindDefaulters(pool))
		// GET /fees/export (Excel via signed URL) deferred: needs a
		// spreadsheet dependency — founder decision per CLAUDE.md.
	})
}

var validPaymentModes = map[string]bool{
	"cash": true, "upi": true, "cheque": true, "bank_transfer": true,
}

// numericToString renders a pgtype.Numeric for JSON without losing
// precision to float64 — money stays a string.
func numericToString(n pgtype.Numeric) string {
	v, err := n.Value()
	if err != nil || v == nil {
		return "0"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// currentAcademicYear resolves the school's current academic year inside
// an existing RLS transaction. Fee amounts and payments are always scoped
// to it.
func currentAcademicYear(r *http.Request, q *sqlc.Queries, schoolID pgtype.UUID) (pgtype.UUID, error) {
	return q.GetCurrentAcademicYear(r.Context(), schoolID)
}

type feeStructureRequest struct {
	ClassID    string         `json:"class_id"`
	Components []feeComponent `json:"components"`
}

type feeComponent struct {
	Component string `json:"component"`
	Amount    string `json:"amount"`
}

// createFeeStructure implements POST /api/:school/fees/structure —
// bulk-creates fee components for a class in the current academic year.
func createFeeStructure(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin")
		if claims == nil {
			return
		}

		var req feeStructureRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if req.ClassID == "" || len(req.Components) == 0 {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "class_id and components are required.")
			return
		}
		classID, err := parseUUID(req.ClassID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "class_id is not a valid UUID.")
			return
		}
		amounts := make([]pgtype.Numeric, len(req.Components))
		for i, c := range req.Components {
			if c.Component == "" {
				writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", fmt.Sprintf("components[%d].component is required.", i))
				return
			}
			if err := amounts[i].Scan(c.Amount); err != nil || !amounts[i].Valid {
				writeErr(w, http.StatusBadRequest, "INVALID_FIELD", fmt.Sprintf("components[%d].amount must be a decimal string.", i))
				return
			}
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		created := make([]map[string]any, 0, len(req.Components))
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			yearID, err := currentAcademicYear(r, q, schoolID)
			if err != nil {
				return fmt.Errorf("no current academic year: %w", err)
			}
			for i, c := range req.Components {
				row, err := q.CreateFeeStructure(r.Context(), sqlc.CreateFeeStructureParams{
					SchoolID: schoolID, AcademicYearID: yearID,
					ClassID: classID, Component: c.Component, Amount: amounts[i],
				})
				if err != nil {
					return err
				}
				created = append(created, map[string]any{
					"id": row.ID, "component": row.Component, "amount": numericToString(row.Amount),
				})
			}
			return nil
		})
		if err != nil {
			slog.Error("fee structure create failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "CREATE_FAILED", "Could not create fee structure. Components may already exist for this class and year.")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"components": created})
	}
}

// getFeeStructures implements GET /api/:school/fees/structure?class_id=.
func getFeeStructures(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin", "principal")
		if claims == nil {
			return
		}
		classID, err := parseUUID(r.URL.Query().Get("class_id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "class_id query parameter must be a valid UUID.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		var rows []sqlc.FeeStructure
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			yearID, err := currentAcademicYear(r, q, schoolID)
			if err != nil {
				return fmt.Errorf("no current academic year: %w", err)
			}
			rows, err = q.GetFeeStructures(r.Context(), sqlc.GetFeeStructuresParams{
				ClassID: classID, AcademicYearID: yearID,
			})
			return err
		})
		if err != nil {
			slog.Error("fee structure fetch failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load fee structure.")
			return
		}

		components := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			components = append(components, map[string]any{
				"id": row.ID, "component": row.Component, "amount": numericToString(row.Amount),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"components": components})
	}
}

type feePaymentRequest struct {
	StudentID   string `json:"student_id"`
	Amount      string `json:"amount"`
	PaymentMode string `json:"payment_mode"`
	PaymentDate string `json:"payment_date"`
	Note        string `json:"note"`
}

// recordFeePayment implements POST /api/:school/fees/payments — partial
// payments are supported by design (F-04); no check against the balance.
func recordFeePayment(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin")
		if claims == nil {
			return
		}

		var req feePaymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		studentID, err := parseUUID(req.StudentID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "student_id is not a valid UUID.")
			return
		}
		var amount pgtype.Numeric
		if err := amount.Scan(req.Amount); err != nil || !amount.Valid {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "amount must be a positive decimal string.")
			return
		}
		if !validPaymentModes[req.PaymentMode] {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "payment_mode must be cash|upi|cheque|bank_transfer.")
			return
		}
		paymentDate, err := parseDate(req.PaymentDate)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "payment_date must be YYYY-MM-DD.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}
		recordedBy, err := parseUUID(claims.Subject)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's subject is not a valid UUID.")
			return
		}

		var note *string
		if req.Note != "" {
			note = &req.Note
		}

		var payment sqlc.FeePayment
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			yearID, err := currentAcademicYear(r, q, schoolID)
			if err != nil {
				return fmt.Errorf("no current academic year: %w", err)
			}
			payment, err = q.RecordFeePayment(r.Context(), sqlc.RecordFeePaymentParams{
				SchoolID: schoolID, StudentID: studentID, AcademicYearID: yearID,
				Amount: amount, PaymentMode: req.PaymentMode, PaymentDate: paymentDate,
				RecordedBy: recordedBy, Note: note,
			})
			return err
		})
		if err != nil {
			slog.Error("fee payment record failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "CREATE_FAILED", "Could not record payment.")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":           payment.ID,
			"amount":       numericToString(payment.Amount),
			"payment_mode": payment.PaymentMode,
			"payment_date": payment.PaymentDate.Time.Format("2006-01-02"),
		})
	}
}

// getStudentBalance implements GET /api/:school/fees/students/:id/balance.
func getStudentBalance(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin", "principal")
		if claims == nil {
			return
		}
		studentID, err := parseUUID(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "id is not a valid UUID.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		var balance sqlc.GetStudentFeeBalanceRow
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			yearID, err := currentAcademicYear(r, q, schoolID)
			if err != nil {
				return fmt.Errorf("no current academic year: %w", err)
			}
			balance, err = q.GetStudentFeeBalance(r.Context(), sqlc.GetStudentFeeBalanceParams{
				ID: studentID, AcademicYearID: yearID,
			})
			return err
		})
		if err != nil {
			slog.Error("balance fetch failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load balance.")
			return
		}

		due, _ := balance.TotalDue.Float64Value()
		paid, _ := balance.TotalPaid.Float64Value()
		writeJSON(w, http.StatusOK, map[string]any{
			"student_id":  chi.URLParam(r, "id"),
			"total_due":   numericToString(balance.TotalDue),
			"total_paid":  numericToString(balance.TotalPaid),
			"balance_due": fmt.Sprintf("%.2f", due.Float64-paid.Float64),
		})
	}
}

// getDefaulters implements GET /api/:school/fees/defaulters.
func getDefaulters(pool *pgxpool.Pool) http.HandlerFunc {
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

		rows, err := fetchDefaulters(r, pool, claims, schoolID)
		if err != nil {
			slog.Error("defaulters fetch failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load defaulters.")
			return
		}

		defaulters := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			entry := map[string]any{
				"student_id":  row.StudentID,
				"full_name":   row.FullName,
				"total_due":   numericToString(row.TotalDue),
				"total_paid":  numericToString(row.TotalPaid),
				"balance_due": numericToString(row.BalanceDue),
			}
			if row.LastPaymentDate.Valid {
				entry["last_payment_date"] = row.LastPaymentDate.Time.Format("2006-01-02")
			} else {
				entry["last_payment_date"] = nil
			}
			defaulters = append(defaulters, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"defaulters": defaulters})
	}
}

func fetchDefaulters(r *http.Request, pool *pgxpool.Pool, claims *middleware.Claims, schoolID pgtype.UUID) ([]sqlc.GetDefaultersRow, error) {
	var rows []sqlc.GetDefaultersRow
	err := db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		yearID, err := currentAcademicYear(r, q, schoolID)
		if err != nil {
			return fmt.Errorf("no current academic year: %w", err)
		}
		rows, err = q.GetDefaulters(r.Context(), sqlc.GetDefaultersParams{
			SchoolID: schoolID, AcademicYearID: yearID,
		})
		return err
	})
	return rows, err
}

// remindDefaulters implements POST /api/:school/fees/defaulters/remind.
// Queues one WhatsApp fee reminder per defaulter, rate-limited to 1 per
// student per 7 days (F-04) — rate-limited students are silently skipped
// and counted, never errored. Messages are queued in whatsapp_messages;
// internal/jobs owns actual delivery.
func remindDefaulters(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin")
		if claims == nil {
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		rows, err := fetchDefaulters(r, pool, claims, schoolID)
		if err != nil {
			slog.Error("defaulters fetch failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load defaulters.")
			return
		}

		queued, skippedRateLimited, skippedNoGuardian := 0, 0, 0
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			for _, d := range rows {
				recent, err := q.HasRecentFeeReminder(r.Context(), d.StudentID)
				if err != nil {
					return err
				}
				if recent {
					skippedRateLimited++
					continue
				}
				guardian, err := q.GetPrimaryGuardian(r.Context(), d.StudentID)
				if errors.Is(err, pgx.ErrNoRows) {
					skippedNoGuardian++
					continue
				}
				if err != nil {
					return err
				}
				preview := fmt.Sprintf("Fee reminder: balance ₹%s due for %s", numericToString(d.BalanceDue), d.FullName)
				msgID, err := q.InsertWhatsappMessage(r.Context(), sqlc.InsertWhatsappMessageParams{
					SchoolID: schoolID, StudentID: d.StudentID, GuardianID: guardian.ID,
					Template: "fee_reminder", Language: guardian.LanguagePref, BodyPreview: &preview,
				})
				if err != nil {
					return err
				}
				if err := q.InsertFeeReminderLog(r.Context(), sqlc.InsertFeeReminderLogParams{
					SchoolID: schoolID, StudentID: d.StudentID, WhatsappMessageID: msgID,
				}); err != nil {
					return err
				}
				queued++
			}
			return nil
		})
		if err != nil {
			slog.Error("reminder queue failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "REMIND_FAILED", "Could not queue reminders.")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"queued":               queued,
			"skipped_rate_limited": skippedRateLimited,
			"skipped_no_guardian":  skippedNoGuardian,
		})
	}
}
