package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dhawalhost/chalkedos/internal/supabase"
)

// phonePattern is a light E.164 sanity check (+ and 8-15 digits). Supabase
// Auth does the authoritative validation; this just rejects junk before the
// upstream call and keeps rate-limiter keys canonical.
var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// otpLimiter enforces the API Specification's rate limit for OTP requests:
// 5 per 15 minutes, applied per phone and per client IP independently.
type otpLimiter struct {
	byPhone *slidingLimiter
	byIP    *slidingLimiter
}

func newOTPLimiter() *otpLimiter {
	return &otpLimiter{
		byPhone: newSlidingLimiter(5, 15*time.Minute),
		byIP:    newSlidingLimiter(5, 15*time.Minute),
	}
}

func (l *otpLimiter) allow(phone, remoteAddr string) bool {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		ip = remoteAddr // RealIP middleware may have stripped the port
	}
	// Check both limits before recording either, so a phone-limited
	// request doesn't also burn the IP budget (and vice versa).
	phoneOK := l.byPhone.allow(phone)
	if !phoneOK {
		return false
	}
	return l.byIP.allow(ip)
}

type otpRequestBody struct {
	Phone string `json:"phone"`
}

// requestOTP implements POST /api/auth/otp/request — a thin proxy to
// Supabase Auth's phone-OTP endpoint. See API Specification, Section 02.
func requestOTP(auth *supabase.AuthClient, limiter *otpLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req otpRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if !phonePattern.MatchString(req.Phone) {
			writeErr(w, http.StatusBadRequest, "INVALID_PHONE", "phone must be in E.164 format, e.g. +919876543210.")
			return
		}
		if !limiter.allow(req.Phone, r.RemoteAddr) {
			writeErr(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many OTP requests. Try again in a few minutes.")
			return
		}

		if err := auth.RequestOTP(r.Context(), req.Phone); err != nil {
			var authErr *supabase.AuthError
			if errors.As(err, &authErr) && authErr.Status < 500 {
				// Upstream rejected the request (bad phone, Supabase-side
				// rate limit) — a client problem, not a server one.
				writeErr(w, http.StatusBadRequest, "OTP_REQUEST_FAILED", authErr.Message)
				return
			}
			slog.Error("otp request failed", "error", err)
			writeErr(w, http.StatusBadGateway, "AUTH_UPSTREAM_ERROR", "Could not send OTP. Try again.")
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{"otp_sent": true})
	}
}

type otpVerifyBody struct {
	Phone string `json:"phone"`
	Token string `json:"token"`
}

// verifyOTP implements POST /api/auth/otp/verify. On success it returns the
// Supabase session tokens plus the caller's profile (school + role), looked
// up via the get_login_profile SECURITY DEFINER function — see
// db/migrations/000015_add_login_profile_lookup.up.sql for why RLS can't
// apply at this point in the flow.
func verifyOTP(auth *supabase.AuthClient, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req otpVerifyBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_BODY", "Could not parse request body.")
			return
		}
		if !phonePattern.MatchString(req.Phone) || req.Token == "" {
			writeErr(w, http.StatusBadRequest, "MISSING_FIELDS", "phone (E.164) and token are required.")
			return
		}

		session, err := auth.VerifyOTP(r.Context(), req.Phone, req.Token)
		if err != nil {
			var authErr *supabase.AuthError
			if errors.As(err, &authErr) && authErr.Status < 500 {
				writeErr(w, http.StatusUnauthorized, "INVALID_OTP", "OTP is incorrect or expired.")
				return
			}
			slog.Error("otp verify failed", "error", err)
			writeErr(w, http.StatusBadGateway, "AUTH_UPSTREAM_ERROR", "Could not verify OTP. Try again.")
			return
		}

		var (
			schoolID, schoolSlug, schoolName string
			role, fullName, languagePref     string
		)
		err = pool.QueryRow(r.Context(),
			`SELECT school_id::text, school_slug, school_name, role, full_name, language_pref
			 FROM get_login_profile($1)`,
			session.User.ID,
		).Scan(&schoolID, &schoolSlug, &schoolName, &role, &fullName, &languagePref)
		if errors.Is(err, pgx.ErrNoRows) {
			// Authenticated with Supabase but no active profile — a user
			// who was removed or never onboarded. Not an auth failure.
			writeErr(w, http.StatusForbidden, "NO_PROFILE", "No active school profile for this account. Contact your school admin.")
			return
		}
		if err != nil {
			slog.Error("login profile lookup failed", "error", err, "user_id", session.User.ID)
			writeErr(w, http.StatusInternalServerError, "PROFILE_LOOKUP_FAILED", "Could not load your profile. Try again.")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  session.AccessToken,
			"refresh_token": session.RefreshToken,
			"expires_in":    session.ExpiresIn,
			"user": map[string]string{
				"id":            session.User.ID,
				"full_name":     fullName,
				"role":          role,
				"language_pref": languagePref,
			},
			"school": map[string]string{
				"id":   schoolID,
				"slug": schoolSlug,
				"name": schoolName,
			},
		})
	}
}
