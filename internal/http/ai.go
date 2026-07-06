package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/ai"
	"github.com/dhawalhost/chalkedos/internal/db"
	"github.com/dhawalhost/chalkedos/internal/db/sqlc"
	"github.com/dhawalhost/chalkedos/internal/middleware"
)

// Cost estimate constants for ai_generations.cost_inr. Claude Sonnet
// pricing at time of writing: $3/M input tokens, $15/M output tokens.
// USD->INR fixed at a rough rate — this column feeds internal cost
// dashboards, not billing, so an estimate is the intent (see the
// Technical Architecture doc's cost model).
const (
	usdPerMInputTokens  = 3.0
	usdPerMOutputTokens = 15.0
	inrPerUSD           = 88.0
)

func aiRoutes(r chi.Router, pool *pgxpool.Pool, client *ai.Client, aiEnabled bool) {
	r.Route("/ai", func(r chi.Router) {
		r.Post("/lesson-plan", generateLessonPlan(pool, client, aiEnabled))
		r.Patch("/lesson-plan/{id}", saveGenerationEdit(pool))
		r.Post("/question-paper", generateQuestionPaper(pool, client, aiEnabled))
		r.Post("/report-cards/batch", generateReportCardsBatch(pool, client, aiEnabled))
		r.Patch("/report-cards/{id}", saveGenerationEdit(pool))
		r.Get("/usage", getAIUsage(pool))
		// PDF endpoints (lesson-plan/:id/pdf etc.) are deliberately not
		// implemented yet: PDF rendering needs a new dependency, which is
		// a founder decision per CLAUDE.md, not a handler detail.
	})
}

// inputHash produces the normalized cache key per F-05: identical inputs
// (feature + prompt version + language + canonical input JSON) within 30
// days must be served from ai_generations without a Claude call.
func inputHash(feature ai.Feature, version, language string, inputJSON []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|", feature, version, language)
	h.Write(inputJSON)
	return hex.EncodeToString(h.Sum(nil))
}

func costINR(res *ai.Result) pgtype.Numeric {
	usd := float64(res.InputTokens)/1e6*usdPerMInputTokens +
		float64(res.OutputTokens)/1e6*usdPerMOutputTokens
	var n pgtype.Numeric
	// Scan on a fixed 4-decimal string cannot fail; matches numeric(10,4).
	_ = n.Scan(fmt.Sprintf("%.4f", usd*inrPerUSD))
	return n
}

func currentQuotaMonth() pgtype.Date {
	now := time.Now()
	return pgtype.Date{Time: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// writeQuotaExceeded is the one deliberate extension of the error
// envelope: F-05 requires the 429 to carry quota_remaining.
func writeQuotaExceeded(w http.ResponseWriter, remaining int32) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":            "QUOTA_EXCEEDED",
			"message":         "Monthly AI generation quota exceeded.",
			"quota_remaining": remaining,
		},
	})
}

// requireTeacher gates the generation endpoints: the API contract lists
// them as Teacher-only.
func requireTeacher(w http.ResponseWriter, r *http.Request) *middleware.Claims {
	return requireRoles(w, r, "teacher")
}

func rlsClaims(c *middleware.Claims) db.RLSClaims {
	return db.RLSClaims{Subject: c.Subject, SchoolID: c.SchoolID, Role: c.Role}
}

// reserveQuota consumes n generations inside one RLS transaction,
// creating the month's quota row first if needed. Returns (remaining,
// true) on success; on false the response has already been written.
func reserveQuota(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, claims *middleware.Claims, schoolID pgtype.UUID, n int32) bool {
	err := db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		yearID, err := q.GetCurrentAcademicYear(r.Context(), schoolID)
		if err != nil {
			return fmt.Errorf("no current academic year: %w", err)
		}
		month := currentQuotaMonth()
		if err := q.EnsureQuotaRow(r.Context(), sqlc.EnsureQuotaRowParams{
			SchoolID: schoolID, AcademicYearID: yearID, Month: month,
		}); err != nil {
			return err
		}
		_, err = q.ReserveQuota(r.Context(), sqlc.ReserveQuotaParams{
			SchoolID: schoolID, Month: month, GenerationsUsed: n,
		})
		return err
	})
	if err == nil {
		return true
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// Reservation didn't fit — report what's actually left.
		var remaining int32
		_ = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			quota, qErr := sqlc.New(tx).GetQuota(r.Context(), sqlc.GetQuotaParams{
				SchoolID: schoolID, Month: currentQuotaMonth(),
			})
			if qErr == nil {
				remaining = quota.GenerationsLimit - quota.GenerationsUsed
			}
			return qErr
		})
		writeQuotaExceeded(w, remaining)
		return false
	}
	slog.Error("quota reservation failed", "error", err)
	writeErr(w, http.StatusInternalServerError, "QUOTA_CHECK_FAILED", "Could not check AI quota.")
	return false
}

// releaseQuota gives back n reserved generations after a failed Claude
// call. Best-effort: a failure here means the school under-counts its
// remaining quota, which we log loudly but don't surface to the teacher.
func releaseQuota(ctx context.Context, pool *pgxpool.Pool, claims *middleware.Claims, schoolID pgtype.UUID, n int32) {
	err := db.WithRLSClaims(ctx, pool, rlsClaims(claims), func(tx pgx.Tx) error {
		return sqlc.New(tx).ReleaseQuota(ctx, sqlc.ReleaseQuotaParams{
			SchoolID: schoolID, Month: currentQuotaMonth(), GenerationsUsed: n,
		})
	})
	if err != nil {
		slog.Error("quota release failed — school quota now over-counted", "error", err, "school_id", claims.SchoolID, "count", n)
	}
}

// generationResponse is the shared success shape for generation endpoints.
func generationResponse(gen sqlc.AiGeneration, cached bool) map[string]any {
	return map[string]any{
		"id":     gen.ID,
		"output": json.RawMessage(gen.OutputJson),
		"cached": cached,
	}
}

// runGeneration is the shared cache→quota→Claude→store flow. validate
// inspects the model's raw text and returns the canonical output JSON to
// store, or an error to trigger one retry (F-08's marks re-check).
func runGeneration(
	w http.ResponseWriter, r *http.Request,
	pool *pgxpool.Pool, client *ai.Client,
	claims *middleware.Claims, feature ai.Feature, language string,
	inputJSON []byte, systemPrompt string,
	validate func(text string) (json.RawMessage, error),
) {
	schoolID, err := parseUUID(claims.SchoolID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
		return
	}
	teacherID, err := parseUUID(claims.Subject)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's subject is not a valid UUID.")
		return
	}

	hash := inputHash(feature, ai.CurrentVersion, language, inputJSON)

	// Cache check — a hit costs no quota and no Claude call.
	var cached sqlc.AiGeneration
	cacheErr := db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
		var err error
		cached, err = sqlc.New(tx).GetCachedGeneration(r.Context(), sqlc.GetCachedGenerationParams{
			SchoolID: schoolID, InputHash: hash, Feature: string(feature),
		})
		return err
	})
	if cacheErr == nil {
		writeJSON(w, http.StatusOK, generationResponse(cached, true))
		return
	}
	if !errors.Is(cacheErr, pgx.ErrNoRows) {
		slog.Error("cache lookup failed", "error", cacheErr)
		writeErr(w, http.StatusInternalServerError, "CACHE_CHECK_FAILED", "Could not check generation cache.")
		return
	}

	if !reserveQuota(w, r, pool, claims, schoolID, 1) {
		return
	}

	output, res, err := generateValidated(r.Context(), client, systemPrompt, validate)
	if err != nil {
		releaseQuota(r.Context(), pool, claims, schoolID, 1)
		slog.Error("generation failed", "feature", feature, "error", err)
		writeErr(w, http.StatusBadGateway, "GENERATION_FAILED", "AI generation failed. Your quota was not consumed — try again.")
		return
	}

	var gen sqlc.AiGeneration
	err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
		var err error
		gen, err = sqlc.New(tx).InsertGeneration(r.Context(), sqlc.InsertGenerationParams{
			SchoolID: schoolID, TeacherID: teacherID,
			Feature: string(feature), PromptVersion: ai.CurrentVersion,
			InputHash: hash, InputJson: inputJSON, OutputJson: output,
			Language: language, Model: "claude-sonnet-4-6", CostInr: costINR(res),
		})
		return err
	})
	if err != nil {
		releaseQuota(r.Context(), pool, claims, schoolID, 1)
		slog.Error("generation store failed", "error", err)
		writeErr(w, http.StatusInternalServerError, "STORE_FAILED", "Generation succeeded but could not be saved. Try again.")
		return
	}

	writeJSON(w, http.StatusCreated, generationResponse(gen, false))
}

// generateValidated calls Claude and validates the output, retrying once
// on validation failure per F-08 ("retry once automatically on mismatch
// rather than surfacing a broken paper").
func generateValidated(ctx context.Context, client *ai.Client, systemPrompt string, validate func(string) (json.RawMessage, error)) (json.RawMessage, *ai.Result, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		res, err := client.Generate(ctx, systemPrompt, "Generate the JSON now.")
		if err != nil {
			return nil, nil, err
		}
		output, err := validate(res.Text)
		if err == nil {
			return output, res, nil
		}
		lastErr = err
		slog.Warn("generation output invalid, retrying", "attempt", attempt+1, "error", err)
	}
	return nil, nil, fmt.Errorf("output invalid after retry: %w", lastErr)
}

// validJSONObject is the baseline validator: the model must return a
// parseable JSON object (F-05: "don't let a handler pass through
// unvalidated model output").
func validJSONObject(text string) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	return json.RawMessage(text), nil
}

func validLanguage(l string) bool { return l == "hi" || l == "en" }

// --- Lesson plan (F-05) ---

type lessonPlanRequest struct {
	Board           string `json:"board"`
	Class           string `json:"class"`
	Subject         string `json:"subject"`
	Chapter         string `json:"chapter"`
	DurationMinutes int    `json:"duration_minutes"`
	Language        string `json:"language"`
}

func generateLessonPlan(pool *pgxpool.Pool, client *ai.Client, aiEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireTeacher(w, r)
		if claims == nil {
			return
		}
		if !aiEnabled {
			writeErr(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI features are not configured on this server.")
			return
		}

		var req lessonPlanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if req.Board == "" || req.Class == "" || req.Subject == "" || req.Chapter == "" ||
			req.DurationMinutes < 20 || req.DurationMinutes > 120 || !validLanguage(req.Language) {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "board, class, subject, chapter, duration_minutes (20-120), and language (hi|en) are required.")
			return
		}

		prompt, err := ai.LoadPrompt(ai.FeatureLessonPlan, ai.CurrentVersion)
		if err != nil {
			slog.Error("prompt load failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "PROMPT_LOAD_FAILED", "Could not load generation prompt.")
			return
		}
		prompt = strings.NewReplacer(
			"{board}", req.Board,
			"{class}", req.Class,
			"{subject}", req.Subject,
			"{chapter}", req.Chapter,
			"{duration_minutes}", fmt.Sprintf("%d", req.DurationMinutes),
			"{language}", req.Language,
		).Replace(prompt)

		inputJSON, _ := json.Marshal(req)
		runGeneration(w, r, pool, client, claims, ai.FeatureLessonPlan, req.Language, inputJSON, prompt, validJSONObject)
	}
}

// --- Question paper (F-08) ---

type questionPaperRequest struct {
	Board         string   `json:"board"`
	Class         string   `json:"class"`
	Subject       string   `json:"subject"`
	Chapters      []string `json:"chapters"`
	TotalMarks    int      `json:"total_marks"`
	QuestionTypes []string `json:"question_types"`
	Language      string   `json:"language"`
}

// questionPaperOutput is the subset of the prompt's output schema the
// server-side marks check needs.
type questionPaperOutput struct {
	Sections []struct {
		Questions []struct {
			Marks int `json:"marks"`
		} `json:"questions"`
	} `json:"sections"`
}

func generateQuestionPaper(pool *pgxpool.Pool, client *ai.Client, aiEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireTeacher(w, r)
		if claims == nil {
			return
		}
		if !aiEnabled {
			writeErr(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI features are not configured on this server.")
			return
		}

		var req questionPaperRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if req.Board == "" || req.Class == "" || req.Subject == "" || len(req.Chapters) == 0 ||
			req.TotalMarks < 10 || req.TotalMarks > 100 || len(req.QuestionTypes) == 0 || !validLanguage(req.Language) {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "board, class, subject, chapters, total_marks (10-100), question_types, and language (hi|en) are required.")
			return
		}

		prompt, err := ai.LoadPrompt(ai.FeatureQuestionPaper, ai.CurrentVersion)
		if err != nil {
			slog.Error("prompt load failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "PROMPT_LOAD_FAILED", "Could not load generation prompt.")
			return
		}
		prompt = strings.NewReplacer(
			"{board}", req.Board,
			"{class}", req.Class,
			"{subject}", req.Subject,
			"{chapters}", strings.Join(req.Chapters, ", "),
			"{total_marks}", fmt.Sprintf("%d", req.TotalMarks),
			"{question_types}", strings.Join(req.QuestionTypes, ", "),
			"{language}", req.Language,
		).Replace(prompt)

		// F-08: independently re-sum every question's marks — never trust
		// the model's own total_marks_check.
		validate := func(text string) (json.RawMessage, error) {
			raw, err := validJSONObject(text)
			if err != nil {
				return nil, err
			}
			var paper questionPaperOutput
			if err := json.Unmarshal(raw, &paper); err != nil {
				return nil, fmt.Errorf("output does not match question paper schema: %w", err)
			}
			sum := 0
			for _, s := range paper.Sections {
				for _, q := range s.Questions {
					sum += q.Marks
				}
			}
			if sum != req.TotalMarks {
				return nil, fmt.Errorf("marks sum %d != requested total %d", sum, req.TotalMarks)
			}
			return raw, nil
		}

		inputJSON, _ := json.Marshal(req)
		runGeneration(w, r, pool, client, claims, ai.FeatureQuestionPaper, req.Language, inputJSON, prompt, validate)
	}
}

// --- Report card remarks, batch (F-09) ---

type reportCardStudent struct {
	StudentName   string         `json:"student_name"`
	Class         string         `json:"class"`
	SubjectScores map[string]int `json:"subject_scores"`
	AttendancePct int            `json:"attendance_pct"`
	TeacherNote   string         `json:"teacher_note"`
}

type reportCardsBatchRequest struct {
	Students []reportCardStudent `json:"students"`
	Language string              `json:"language"`
}

func generateReportCardsBatch(pool *pgxpool.Pool, client *ai.Client, aiEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireTeacher(w, r)
		if claims == nil {
			return
		}
		if !aiEnabled {
			writeErr(w, http.StatusServiceUnavailable, "AI_DISABLED", "AI features are not configured on this server.")
			return
		}

		var req reportCardsBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if len(req.Students) == 0 || len(req.Students) > 60 || !validLanguage(req.Language) {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "students (1-60) and language (hi|en) are required.")
			return
		}
		for i, s := range req.Students {
			if s.StudentName == "" || s.Class == "" || len(s.SubjectScores) == 0 {
				writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", fmt.Sprintf("students[%d] needs student_name, class, and subject_scores.", i))
				return
			}
		}

		schoolID, err := parseUUID(claims.SchoolID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's school_id is not a valid UUID.")
			return
		}
		teacherID, err := parseUUID(claims.Subject)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's subject is not a valid UUID.")
			return
		}

		basePrompt, err := ai.LoadPrompt(ai.FeatureReportCardRemark, ai.CurrentVersion)
		if err != nil {
			slog.Error("prompt load failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "PROMPT_LOAD_FAILED", "Could not load generation prompt.")
			return
		}

		// Pass 1: resolve cache hits so the quota reservation covers only
		// the students who actually need a Claude call — F-09 requires the
		// whole batch to be checked up front, not to fail partway.
		type studentWork struct {
			student   reportCardStudent
			inputJSON []byte
			hash      string
			cachedGen *sqlc.AiGeneration
		}
		work := make([]studentWork, len(req.Students))
		misses := int32(0)
		cacheScanErr := db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			for i, s := range req.Students {
				inputJSON, _ := json.Marshal(struct {
					reportCardStudent
					Language string `json:"language"`
				}{s, req.Language})
				hash := inputHash(ai.FeatureReportCardRemark, ai.CurrentVersion, req.Language, inputJSON)
				work[i] = studentWork{student: s, inputJSON: inputJSON, hash: hash}
				gen, err := q.GetCachedGeneration(r.Context(), sqlc.GetCachedGenerationParams{
					SchoolID: schoolID, InputHash: hash, Feature: string(ai.FeatureReportCardRemark),
				})
				if err == nil {
					work[i].cachedGen = &gen
				} else if errors.Is(err, pgx.ErrNoRows) {
					misses++
				} else {
					return err
				}
			}
			return nil
		})
		if cacheScanErr != nil {
			slog.Error("batch cache scan failed", "error", cacheScanErr)
			writeErr(w, http.StatusInternalServerError, "CACHE_CHECK_FAILED", "Could not check generation cache.")
			return
		}

		if misses > 0 && !reserveQuota(w, r, pool, claims, schoolID, misses) {
			return
		}

		results := make([]map[string]any, 0, len(work))
		generated := int32(0)
		for _, item := range work {
			if item.cachedGen != nil {
				results = append(results, map[string]any{
					"student_name": item.student.StudentName,
					"id":           item.cachedGen.ID,
					"output":       json.RawMessage(item.cachedGen.OutputJson),
					"cached":       true,
				})
				continue
			}

			scores, _ := json.Marshal(item.student.SubjectScores)
			prompt := strings.NewReplacer(
				"{student_name}", item.student.StudentName,
				"{class}", item.student.Class,
				"{subject_scores}", string(scores),
				"{attendance_pct}", fmt.Sprintf("%d", item.student.AttendancePct),
				"{teacher_note}", item.student.TeacherNote,
				"{language}", req.Language,
			).Replace(basePrompt)

			output, res, err := generateValidated(r.Context(), client, prompt, validJSONObject)
			if err != nil {
				// Give back quota for this student and every one not yet
				// attempted, report partial results honestly.
				releaseQuota(r.Context(), pool, claims, schoolID, misses-generated)
				slog.Error("batch generation failed", "student", item.student.StudentName, "error", err)
				writeJSON(w, http.StatusOK, map[string]any{
					"results":   results,
					"partial":   true,
					"failed_at": item.student.StudentName,
				})
				return
			}
			generated++

			var gen sqlc.AiGeneration
			storeErr := db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
				var err error
				gen, err = sqlc.New(tx).InsertGeneration(r.Context(), sqlc.InsertGenerationParams{
					SchoolID: schoolID, TeacherID: teacherID,
					Feature: string(ai.FeatureReportCardRemark), PromptVersion: ai.CurrentVersion,
					InputHash: item.hash, InputJson: item.inputJSON, OutputJson: output,
					Language: req.Language, Model: "claude-sonnet-4-6", CostInr: costINR(res),
				})
				return err
			})
			if storeErr != nil {
				slog.Error("batch generation store failed", "error", storeErr)
				writeErr(w, http.StatusInternalServerError, "STORE_FAILED", "A generation could not be saved. Partial results were not returned — try again.")
				return
			}
			results = append(results, map[string]any{
				"student_name": item.student.StudentName,
				"id":           gen.ID,
				"output":       json.RawMessage(gen.OutputJson),
				"cached":       false,
			})
		}

		writeJSON(w, http.StatusCreated, map[string]any{"results": results, "partial": false})
	}
}

// --- Shared PATCH: save inline edits ---

type saveEditRequest struct {
	Output json.RawMessage `json:"output"`
}

func saveGenerationEdit(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := requireTeacher(w, r)
		if claims == nil {
			return
		}
		genID, err := parseUUID(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "id is not a valid UUID.")
			return
		}
		teacherID, err := parseUUID(claims.Subject)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "INVALID_CLAIMS", "Token's subject is not a valid UUID.")
			return
		}

		var req saveEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Output) == 0 {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "output (JSON) is required.")
			return
		}
		if _, err := validJSONObject(string(req.Output)); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_FIELD", "output must be a JSON object.")
			return
		}

		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			_, err := sqlc.New(tx).UpdateGenerationOutput(r.Context(), sqlc.UpdateGenerationOutputParams{
				ID: genID, TeacherID: teacherID, OutputJson: req.Output,
			})
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "NOT_FOUND", "Resource not found.")
			return
		}
		if err != nil {
			slog.Error("generation edit failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "UPDATE_FAILED", "Could not save edits.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	}
}

// --- GET /ai/usage ---

func getAIUsage(pool *pgxpool.Pool) http.HandlerFunc {
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

		var quota sqlc.GetQuotaRow
		err = db.WithRLSClaims(r.Context(), pool, rlsClaims(claims), func(tx pgx.Tx) error {
			var err error
			quota, err = sqlc.New(tx).GetQuota(r.Context(), sqlc.GetQuotaParams{
				SchoolID: schoolID, Month: currentQuotaMonth(),
			})
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// No generations yet this month — report the default limit.
			writeJSON(w, http.StatusOK, map[string]any{
				"generations_used": 0, "generations_limit": 500, "remaining": 500,
			})
			return
		}
		if err != nil {
			slog.Error("usage lookup failed", "error", err)
			writeErr(w, http.StatusInternalServerError, "USAGE_LOOKUP_FAILED", "Could not load AI usage.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"generations_used":  quota.GenerationsUsed,
			"generations_limit": quota.GenerationsLimit,
			"remaining":         quota.GenerationsLimit - quota.GenerationsUsed,
		})
	}
}
