package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/config"
	"github.com/dhawalhost/chalkedos/internal/db"
	"github.com/dhawalhost/chalkedos/internal/db/sqlc"
)

// communicationRoutes registers the JWT-authenticated communication
// endpoints. The public (signed-token) PTM endpoints are registered
// separately in the router, outside the RequireAuth subtree.
func communicationRoutes(r chi.Router, pool *pgxpool.Pool, cfg *config.Config) {
	r.Route("/communication", func(r chi.Router) {
		r.Post("/broadcast", sendBroadcast(pool))
		r.Post("/ptm/slots", createPTMSlots(pool))
		r.Post("/ptm/invite", createPTMInvite(pool, cfg))
		r.Get("/messages", listMessages(pool))
	})
}

type broadcastRequest struct {
	Message   string `json:"message"`
	SectionID string `json:"section_id"` // optional — empty = whole school
}

// sendBroadcast implements POST /api/:school/communication/broadcast.
// Free text goes into the pre-approved 'broadcast' template's variable
// slot (F-07) — the template name is fixed here, never client-supplied,
// which is what keeps this within Meta's approved-template constraint.
func sendBroadcast(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin", "principal")
		if claims == nil {
			return
		}

		var req broadcastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if req.Message == "" || len(req.Message) > 1000 {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "message is required (max 1000 characters).")
			return
		}
		var sectionID *string
		if req.SectionID != "" {
			if _, err := parseUUID(req.SectionID); err != nil {
				writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "section_id is not a valid UUID.")
				return
			}
			sectionID = &req.SectionID
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		queued := 0
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			var sectionParam sqlc.GetBroadcastRecipientsParams
			sectionParam.SchoolID = schoolID
			if sectionID != nil {
				sid, _ := parseUUID(*sectionID)
				sectionParam.SectionID = sid
			}
			recipients, err := q.GetBroadcastRecipients(r.Context(), sectionParam)
			if err != nil {
				return err
			}
			preview := req.Message
			if len(preview) > 120 {
				preview = preview[:120]
			}
			for _, rec := range recipients {
				if _, err := q.InsertWhatsappMessage(r.Context(), sqlc.InsertWhatsappMessageParams{
					SchoolID: schoolID, StudentID: rec.StudentID, GuardianID: rec.GuardianID,
					Template: "broadcast", Language: rec.LanguagePref, BodyPreview: &preview,
				}); err != nil {
					return err
				}
				queued++
			}
			return nil
		})
		if err != nil {
			slog.Error("broadcast queue failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "BROADCAST_FAILED", "Could not queue broadcast.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
	}
}

type ptmSlotsRequest struct {
	TeacherID string   `json:"teacher_id"` // optional for teachers (defaults to caller); required for admins
	SlotTimes []string `json:"slot_times"` // RFC3339
}

// createPTMSlots implements POST /api/:school/communication/ptm/slots.
func createPTMSlots(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "teacher", "admin")
		if claims == nil {
			return
		}

		var req ptmSlotsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if len(req.SlotTimes) == 0 || len(req.SlotTimes) > 50 {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "slot_times (1-50, RFC3339) is required.")
			return
		}

		// Teachers create their own slots; admins must name the teacher.
		teacherIDStr := req.TeacherID
		if claims.Role == "teacher" {
			teacherIDStr = claims.Subject
		} else if teacherIDStr == "" {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "teacher_id is required when creating slots as admin.")
			return
		}
		teacherID, err := parseUUID(teacherIDStr)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "teacher_id is not a valid UUID.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		times := make([]time.Time, len(req.SlotTimes))
		for i, ts := range req.SlotTimes {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil || t.Before(time.Now()) {
				writeErr(w, http.StatusBadRequest, "INVALID_FIELD", fmt.Sprintf("slot_times[%d] must be a future RFC3339 timestamp.", i))
				return
			}
			times[i] = t
		}

		created := make([]map[string]any, 0, len(times))
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			for _, t := range times {
				slot, err := q.CreatePTMSlot(r.Context(), sqlc.CreatePTMSlotParams{
					SchoolID: schoolID, TeacherID: teacherID,
					SlotTime: pgTimestamptz(t),
				})
				if err != nil {
					return err
				}
				created = append(created, map[string]any{
					"id": slot.ID, "slot_time": slot.SlotTime.Time.Format(time.RFC3339),
				})
			}
			return nil
		})
		if err != nil {
			slog.Error("ptm slot create failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "CREATE_FAILED", "Could not create PTM slots.")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"slots": created})
	}
}

type ptmInviteRequest struct {
	StudentID string `json:"student_id"`
}

// createPTMInvite implements POST /api/:school/communication/ptm/invite —
// mints the signed booking token a parent uses on the public PTM
// endpoints. In the full flow this token rides inside a WhatsApp template
// (internal/jobs); this endpoint is the token source either way.
func createPTMInvite(pool *pgxpool.Pool, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireRoles(w, r, "admin", "teacher")
		if claims == nil {
			return
		}
		if cfg.PTMTokenSecret == "" {
			writeErr(w, http.StatusServiceUnavailable, "PTM_DISABLED", "PTM booking links are not configured on this server.")
			return
		}

		var req ptmInviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		studentID, err := parseUUID(req.StudentID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "student_id is not a valid UUID.")
			return
		}
		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}

		var pair sqlc.GetStudentWithPrimaryGuardianRow
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			var err error
			pair, err = sqlc.New(tx).GetStudentWithPrimaryGuardian(r.Context(), sqlc.GetStudentWithPrimaryGuardianParams{
				ID: studentID, SchoolID: schoolID,
			})
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
			return
		}
		if err != nil {
			slog.Error("ptm invite lookup failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "INVITE_FAILED", "Could not create booking link.")
			return
		}

		token, err := signPTMToken(cfg.PTMTokenSecret, ptmTokenPayload{
			SchoolID:   claims.SchoolID,
			StudentID:  uuidString(pair.StudentID),
			GuardianID: uuidString(pair.GuardianID),
			ExpiresAt:  time.Now().Add(ptmTokenTTL).Unix(),
		})
		if err != nil {
			slog.Error("ptm token sign failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "INVITE_FAILED", "Could not create booking link.")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      token,
			"expires_in": int(ptmTokenTTL.Seconds()),
		})
	}
}

// listMessages implements GET /api/:school/communication/messages.
func listMessages(pool *pgxpool.Pool) http.HandlerFunc {
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

		params := sqlc.ListWhatsappMessagesParams{SchoolID: schoolID}
		if sid := r.URL.Query().Get("student_id"); sid != "" {
			studentID, err := parseUUID(sid)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "student_id must be a valid UUID.")
				return
			}
			params.StudentID = studentID
		}

		var rows []sqlc.ListWhatsappMessagesRow
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			var err error
			rows, err = sqlc.New(tx).ListWhatsappMessages(r.Context(), params)
			return err
		})
		if err != nil {
			slog.Error("messages list failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load messages.")
			return
		}

		messages := make([]map[string]any, 0, len(rows))
		for _, m := range rows {
			entry := map[string]any{
				"id": m.ID, "student_id": m.StudentID, "template": m.Template,
				"language": m.Language, "status": m.Status, "body_preview": m.BodyPreview,
				"created_at": m.CreatedAt.Time.Format(time.RFC3339),
			}
			if m.SentAt.Valid {
				entry["sent_at"] = m.SentAt.Time.Format(time.RFC3339)
			}
			if m.DeliveredAt.Valid {
				entry["delivered_at"] = m.DeliveredAt.Time.Format(time.RFC3339)
			}
			messages = append(messages, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// --- Public PTM endpoints (signed token, no JWT) ---

// publicPTMRoutes registers the parent-facing booking endpoints. These
// live OUTSIDE RequireAuth: authentication is the signed token minted by
// createPTMInvite, verified per-request.
func publicPTMRoutes(r chi.Router, pool *pgxpool.Pool, cfg *config.Config) {
	r.Get("/api/public/ptm/slots", listPublicPTMSlots(pool, cfg))
	r.Post("/api/public/ptm/bookings", bookPublicPTMSlot(pool, cfg))
}

func ptmTokenFromRequest(w http.ResponseWriter, r *http.Request, cfg *config.Config) *ptmTokenPayload {
	if cfg.PTMTokenSecret == "" {
		writeErr(w, http.StatusServiceUnavailable, "PTM_DISABLED", "PTM booking is not configured on this server.")
		return nil
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-Booking-Token")
	}
	payload, err := verifyPTMToken(cfg.PTMTokenSecret, token)
	if err != nil {
		// One generic 401 for every failure mode — missing, malformed,
		// bad signature, expired — nothing to enumerate against.
		writeErr(w, http.StatusUnauthorized, "INVALID_TOKEN", "Booking link is invalid or has expired.")
		return nil
	}
	return payload
}

// listPublicPTMSlots implements GET /api/public/ptm/slots?token=.
func listPublicPTMSlots(pool *pgxpool.Pool, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload := ptmTokenFromRequest(w, r, cfg)
		if payload == nil {
			return
		}

		rows, err := pool.Query(r.Context(),
			`SELECT id, teacher_name, slot_time FROM list_open_ptm_slots($1)`,
			payload.SchoolID)
		if err != nil {
			slog.Error("public slot list failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load available slots.")
			return
		}
		defer rows.Close()

		slots := make([]map[string]any, 0)
		for rows.Next() {
			var id, teacherName string
			var slotTime time.Time
			if err := rows.Scan(&id, &teacherName, &slotTime); err != nil {
				slog.Error("public slot scan failed", "error", err)
				writeErr(w, http.StatusInternalServerError, "FETCH_FAILED", "Could not load available slots.")
				return
			}
			slots = append(slots, map[string]any{
				"id": id, "teacher_name": teacherName, "slot_time": slotTime.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"slots": slots})
	}
}

type publicBookingRequest struct {
	SlotID string `json:"slot_id"`
}

// bookPublicPTMSlot implements POST /api/public/ptm/bookings?token=.
// The double-booking race is settled by the UNIQUE constraint on
// ptm_bookings.ptm_slot_id inside book_ptm_slot — the loser gets 409.
func bookPublicPTMSlot(pool *pgxpool.Pool, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload := ptmTokenFromRequest(w, r, cfg)
		if payload == nil {
			return
		}

		var req publicBookingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if _, err := parseUUID(req.SlotID); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "slot_id is not a valid UUID.")
			return
		}

		var bookingID *string
		err := pool.QueryRow(r.Context(),
			`SELECT book_ptm_slot($1, $2, $3, $4)::text`,
			req.SlotID, payload.SchoolID, payload.StudentID, payload.GuardianID,
		).Scan(&bookingID)
		if err != nil {
			slog.Error("public booking failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "BOOKING_FAILED", "Could not book the slot. Try again.")
			return
		}
		if bookingID == nil {
			writeErr(w, http.StatusConflict, "SLOT_TAKEN", "This slot was just booked by someone else. Pick another.")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"booking_id": *bookingID})
	}
}
